package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// TestDisableRacesIssuanceAndRevivesNothing proves the disable/enable cycle
// closes the concurrent-issuance race: a code and a cookie committed while a
// disable is in flight (parked mid-sweep on a held code row) stay unusable
// for the disabled account and are swept by the re-enable, so nothing issued
// against the deactivated account revives with it. The interleave completes
// without deadlock — a lock conflict would surface as an error from either
// side and fail the test.
func TestDisableRacesIssuanceAndRevivesNothing(t *testing.T) {
	env := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	op := env.newAdmin("race-op")
	victim, _, _ := env.createUserSession("race-victim")

	// An unused code row gives the disable sweep something deterministic to
	// park on when its row is held.
	if _, _, err := env.store.IssueAccountCode(ctx, op.ID, victim.ID, 10*time.Minute); err != nil {
		t.Fatalf("issue park code: %v", err)
	}
	var parkedCodeID uuid.UUID
	if err := env.pool.QueryRow(ctx,
		`SELECT id FROM auth_codes WHERE user_id = $1 AND used_at IS NULL LIMIT 1`, victim.ID,
	).Scan(&parkedCodeID); err != nil {
		t.Fatalf("load park code: %v", err)
	}

	holder, err := env.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	defer holder.Release()
	hTx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer func() { _ = hTx.Rollback(context.Background()) }()
	if _, err := hTx.Exec(ctx,
		`SELECT id FROM auth_codes WHERE id = $1 FOR NO KEY UPDATE`, parkedCodeID); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	disableErr := make(chan error, 1)
	go func() {
		_, err := env.store.DisableAccount(ctx, op.ID, victim.ID)
		disableErr <- err
	}()
	for {
		var waiting bool
		if err := env.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid)))`,
			int32(holder.Conn().PgConn().PID()),
		).Scan(&waiting); err != nil {
			t.Fatalf("wait for blocked disable sweep: %v", err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("disable never reached the held code row: %v", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The raced issuance: a fresh code and cookie committed while the
	// disable transaction is in flight.
	racedCode, _, _, err := env.store.AuthCodes.Create(ctx, victim.ID, store.AuthCodePurposeLogin, time.Hour)
	if err != nil {
		t.Fatalf("issue raced code: %v", err)
	}
	racedToken, _, _, err := env.store.Sessions.Create(ctx, victim.ID, time.Hour)
	if err != nil {
		t.Fatalf("issue raced cookie: %v", err)
	}
	if err := hTx.Commit(ctx); err != nil {
		t.Fatalf("release park: %v", err)
	}
	if err := <-disableErr; err != nil {
		t.Fatalf("disable failed (deadlock or guard refused the interleave): %v", err)
	}

	// While disabled: the raced artifacts are inert.
	if _, err := env.store.ExchangeCode(ctx, racedCode, time.Hour, "localhost", time.Minute); !errors.Is(err, store.ErrCodePurpose) {
		t.Fatalf("raced code while disabled: got %v, want ErrCodePurpose", err)
	}
	if _, err := env.store.Sessions.GetAndTouch(ctx, sha256Sum64(racedToken)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("raced cookie while disabled: got %v, want ErrNotFound", err)
	}

	// Re-enabling must not revive anything issued against the deactivated
	// account: the sweep re-runs before the status flips.
	if _, err := env.store.EnableAccount(ctx, op.ID, victim.ID); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if _, err := env.store.ExchangeCode(ctx, racedCode, time.Hour, "localhost", time.Minute); !errors.Is(err, store.ErrCodeExpired) {
		t.Fatalf("raced code after re-enable: got %v, want ErrCodeExpired", err)
	}
	if _, err := env.store.Sessions.GetAndTouch(ctx, sha256Sum64(racedToken)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("raced cookie after re-enable: got %v, want ErrNotFound", err)
	}
}

func TestInactiveAccountRejectsCredentialIssuance(t *testing.T) {
	for _, status := range []string{model.UserStatusDisabled, model.UserStatusDeleted} {
		t.Run(status, func(t *testing.T) {
			env := setup(t)
			ctx := t.Context()
			op := env.newAdmin("issue-op")
			user, _, _ := env.createUserSession("inactive-user")
			keys, err := env.store.Passkeys.ListByUser(ctx, user.ID)
			if err != nil || len(keys) == 0 {
				t.Fatal("fixture passkey missing")
			}
			if status == model.UserStatusDisabled {
				_, err = env.store.DisableAccount(ctx, op.ID, user.ID)
			} else {
				_, err = env.store.DeleteAccount(ctx, op.ID, user.ID)
			}
			if err != nil {
				t.Fatalf("deactivate account: %v", err)
			}
			if _, _, _, err := env.store.Sessions.Create(ctx, user.ID, time.Hour); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("issue session: got %v, want ErrConflict", err)
			}
			for _, purpose := range []string{store.AuthCodePurposeInit, store.AuthCodePurposeLogin} {
				if _, _, _, err := env.store.AuthCodes.Create(ctx, user.ID, purpose, time.Minute); !errors.Is(err, store.ErrCodePurpose) {
					t.Fatalf("issue %s code: got %v, want ErrCodePurpose", purpose, err)
				}
			}
			api := &API{store: env.store, cfg: Config{SessionTTL: time.Hour, RPID: "localhost"}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			response := httptest.NewRecorder()
			api.finishLogin(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/verify", nil), user, keys[0].CredentialID)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("stale verified login status = %d, want 401", response.Code)
			}
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == sessionCookieName {
					t.Fatal("inactive account received a session cookie")
				}
			}
		})
	}
}

// TestSoleActiveOwnerProtection proves the last-owner check counts active
// owners only: with two owners on one site, disabling one leaves the other
// as the last ACTIVE owner, whose own deactivation must then be refused
// until ownership moves back.
func TestSoleActiveOwnerProtection(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	op := env.newAdmin("own-op")
	a, _, _ := env.createUserSession("owner-a")
	b, _, _ := env.createUserSession("owner-b")
	createSite(ctx, t, env, "dual-owned", "Dual Owned", "", "zh-CN", a.ID)

	var siteID uuid.UUID
	if err := env.pool.QueryRow(ctx,
		`SELECT site_id FROM site_memberships WHERE user_id = $1 AND role = 'owner' LIMIT 1`, a.ID,
	).Scan(&siteID); err != nil {
		t.Fatalf("load site: %v", err)
	}
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'owner')`,
		siteID, b.ID); err != nil {
		t.Fatalf("add co-owner: %v", err)
	}

	if _, err := env.store.DisableAccount(ctx, op.ID, b.ID); err != nil {
		t.Fatalf("disable co-owner: %v", err)
	}
	// b is now an inactive owner: it cannot take the site over, so a stays
	// the last active owner and must be refused.
	if _, err := env.store.DisableAccount(ctx, op.ID, a.ID); !errors.Is(err, store.ErrLastOwner) {
		t.Fatalf("disable last active owner: got %v, want ErrLastOwner", err)
	}
	if _, err := env.store.DeleteAccount(ctx, op.ID, a.ID); !errors.Is(err, store.ErrLastOwner) {
		t.Fatalf("delete last active owner: got %v, want ErrLastOwner", err)
	}

	if _, err := env.store.EnableAccount(ctx, op.ID, b.ID); err != nil {
		t.Fatalf("re-enable co-owner: %v", err)
	}
	if _, err := env.store.DisableAccount(ctx, op.ID, a.ID); err != nil {
		t.Fatalf("disable once another active owner exists: %v", err)
	}
}

// TestSiteCreateRequiresActiveOwner proves site creation shares the
// lifecycle lock domain: the owner must be an active account at insert time,
// so a site can never come into existence under an owner that is being
// deactivated concurrently.
func TestSiteCreateRequiresActiveOwner(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	op := env.newAdmin("ghost-op")
	ghost, _, _ := env.createUserSession("ghost")
	if _, err := env.store.DisableAccount(ctx, op.ID, ghost.ID); err != nil {
		t.Fatalf("disable ghost: %v", err)
	}

	if _, err := env.store.Sites.Create(ctx, store.NewSiteParams{Slug: "ghost-site", Name: "Ghost"}, ghost.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("create site under disabled owner: got %v, want ErrConflict", err)
	}
	if _, err := env.store.Sites.Create(ctx, store.NewSiteParams{Slug: "ghost-site", Name: "Ghost"}, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("create site under unknown owner: got %v, want ErrNotFound", err)
	}
}

// setUserRole grants or revokes a platform role directly. Role management
// via the API belongs to the HTTP layer; the store policy tests only need a
// way to arrange operators.
func setUserRole(t *testing.T, env *testEnv, userID uuid.UUID, role string) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE users SET platform_role = $2 WHERE id = $1`, userID, role); err != nil {
		t.Fatalf("set user role %s: %v", role, err)
	}
}

// newAdmin creates an active account with a passkey, a session, and the
// admin platform role.
func (e *testEnv) newAdmin(username string) model.User {
	e.t.Helper()
	user, _, _ := e.createUserSession(username)
	setUserRole(e.t, e, user.ID, model.PlatformRoleAdmin)
	return user
}

// TestAccountCreateAndBindingLoop proves the admin-created account closed
// loop: an active member account plus its first one-time code are written
// atomically, the code redeems into a restricted binding session (the
// account owns no credential for the current relying party), and the binding
// ceremony upgrades it to a full session — no password anywhere.
func TestAccountCreateAndBindingLoop(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	op := env.newAdmin("op-create")

	u, code, expiresAt, err := env.store.CreateAccountWithCode(ctx, op.ID, "newbie", "Newbie", time.Minute)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if u.Status != model.UserStatusActive || u.PlatformRole != model.PlatformRoleMember || u.DeletedAt != nil {
		t.Fatalf("created account: status=%q role=%q deleted=%v", u.Status, u.PlatformRole, u.DeletedAt)
	}
	if code == "" || !expiresAt.After(time.Now()) {
		t.Fatalf("first code must be live: code=%q expires=%v", code, expiresAt)
	}

	if _, _, _, err := env.store.CreateAccountWithCode(ctx, op.ID, "newbie", "Newbie", time.Minute); !errors.Is(err, store.ErrUsernameTaken) {
		t.Fatalf("duplicate username: got %v, want ErrUsernameTaken", err)
	}
	if _, _, _, err := env.store.CreateAccountWithCode(ctx, op.ID, "Bad Name", "x", time.Minute); err == nil {
		t.Fatalf("invalid username accepted")
	}

	result, err := env.store.ExchangeCode(ctx, code, time.Hour, "localhost", time.Minute)
	if err != nil {
		t.Fatalf("exchange first code: %v", err)
	}
	if result.Kind != model.SessionKindRestricted || result.User.ID != u.ID {
		t.Fatalf("first exchange: kind=%q user=%v, want restricted session for the new account", result.Kind, result.User.ID)
	}

	credential := model.Passkey{
		UserID:       u.ID,
		RPID:         "localhost",
		CredentialID: []byte("cred-newbie"),
		PublicKey:    []byte("pk"),
		Transports:   []string{},
	}
	bound, err := env.store.CompletePasskeyBind(ctx, result.SessionID, u.ID, credential, time.Hour)
	if err != nil {
		t.Fatalf("complete binding: %v", err)
	}
	if bound.Kind != model.SessionKindFull {
		t.Fatalf("bound session kind: %q, want full", bound.Kind)
	}
}

// TestAccountRoleChangesAreImmediate proves role writes take effect on the
// next read, that non-admin operators are refused in-transaction, and that
// the last valid administrator cannot be demoted.
func TestAccountRoleChangesAreImmediate(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	op := env.newAdmin("role-op")
	target, _, _ := env.createUserSession("role-target")

	if _, err := env.store.AdminSetPlatformRole(ctx, target.ID, op.ID, model.PlatformRoleAdmin); !errors.Is(err, store.ErrOperatorForbidden) {
		t.Fatalf("member operator: got %v, want ErrOperatorForbidden", err)
	}

	updated, err := env.store.AdminSetPlatformRole(ctx, op.ID, target.ID, model.PlatformRoleAdmin)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if updated.PlatformRole != model.PlatformRoleAdmin {
		t.Fatalf("promoted role: %q", updated.PlatformRole)
	}
	reloaded, err := env.store.Users.GetByID(ctx, target.ID)
	if err != nil || reloaded.PlatformRole != model.PlatformRoleAdmin {
		t.Fatalf("role not immediate after commit: %v %q", err, reloaded.PlatformRole)
	}
	if _, err := env.store.AdminSetPlatformRole(ctx, op.ID, target.ID, model.PlatformRoleMember); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if _, err := env.store.AdminSetPlatformRole(ctx, op.ID, target.ID, "owner"); err == nil {
		t.Fatalf("invalid role accepted")
	}
	if _, err := env.store.AdminSetPlatformRole(ctx, op.ID, uuid.New(), model.PlatformRoleAdmin); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown target: got %v, want ErrNotFound", err)
	}

	// The operator is the only valid admin: self-demotion is refused.
	if _, err := env.store.AdminSetPlatformRole(ctx, op.ID, op.ID, model.PlatformRoleMember); !errors.Is(err, store.ErrLastAdmin) {
		t.Fatalf("demote last admin: got %v, want ErrLastAdmin", err)
	}
}

// TestLastAdminConcurrentDisable proves the lifecycle advisory lock makes
// the last-admin invariant race-free: two concurrent disables that each
// target the other of the only two administrators leave exactly one valid
// administrator — one write wins, the other is refused.
func TestLastAdminConcurrentDisable(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	a := env.newAdmin("admin-a")
	b := env.newAdmin("admin-b")

	type outcome struct {
		target string
		err    error
	}
	results := make(chan outcome, 2)
	go func() {
		_, err := env.store.DisableAccount(ctx, a.ID, b.ID)
		results <- outcome{target: "b", err: err}
	}()
	go func() {
		_, err := env.store.DisableAccount(ctx, b.ID, a.ID)
		results <- outcome{target: "a", err: err}
	}()

	wins := 0
	for i := 0; i < 2; i++ {
		r := <-results
		switch {
		case r.err == nil:
			wins++
		case errors.Is(r.err, store.ErrLastAdmin), errors.Is(r.err, store.ErrOperatorForbidden):
			// Refused under the lock: either the target was already the
			// last valid admin, or the operator was deactivated by the
			// concurrent winner first. Both leave an administrator.
		default:
			t.Fatalf("disable %s: unexpected error %v", r.target, r.err)
		}
	}
	if wins != 1 {
		t.Fatalf("exactly one concurrent disable must win, got %d", wins)
	}
	var validAdmins int
	if err := env.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE platform_role = 'admin' AND status = 'active'`).Scan(&validAdmins); err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if validAdmins != 1 {
		t.Fatalf("valid administrators after race: %d, want 1", validAdmins)
	}
}

// TestDisableRevokesSessionsCodesAndStates proves disabling an account kills
// every live credential-adjacent artifact at once — the session stops
// resolving, an unused code expires instead of redeeming, a ceremony state
// becomes unconsumable, and no further codes can be issued — while
// re-enabling restores only the account, never the revoked artifacts.
func TestDisableRevokesSessionsCodesAndStates(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	op := env.newAdmin("revoke-op")
	victim, token, _ := env.createUserSession("revoke-victim")

	code, _, err := env.store.IssueAccountCode(ctx, op.ID, victim.ID, time.Minute)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}
	stateID, err := env.store.AuthStates.Create(ctx, "bind", &victim.ID, nil, nil, []byte("{}"), time.Minute)
	if err != nil {
		t.Fatalf("create auth state: %v", err)
	}
	if _, err := env.store.Sessions.GetAndTouch(ctx, sha256Sum64(token)); err != nil {
		t.Fatalf("session must resolve before disable: %v", err)
	}

	disabled, err := env.store.DisableAccount(ctx, op.ID, victim.ID)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.Status != model.UserStatusDisabled {
		t.Fatalf("disable result status: %q", disabled.Status)
	}

	if _, err := env.store.Sessions.GetAndTouch(ctx, sha256Sum64(token)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session after disable: got %v, want ErrNotFound", err)
	}
	if _, err := env.store.ExchangeCode(ctx, code, time.Hour, "localhost", time.Minute); !errors.Is(err, store.ErrCodeExpired) {
		t.Fatalf("revoked code after disable: got %v, want ErrCodeExpired", err)
	}
	if _, _, err := env.store.IssueAccountCode(ctx, op.ID, victim.ID, time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("issue code for disabled account: got %v, want ErrConflict", err)
	}
	if _, err := env.store.AuthStates.Consume(ctx, stateID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ceremony state after disable: got %v, want ErrNotFound", err)
	}

	if _, err := env.store.EnableAccount(ctx, op.ID, victim.ID); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if _, err := env.store.Sessions.GetAndTouch(ctx, sha256Sum64(token)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked session must stay revoked after re-enable: %v", err)
	}
}

// TestLastSiteOwnerProtected proves an account that is the sole owner of any
// site cannot be disabled or deleted until ownership is transferred, while a
// second owner unlocks the operation; self disable/delete and deleted
// accounts (terminal: no re-enable, no re-delete, disable of deleted) are
// refused as well.
func TestLastSiteOwnerProtected(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	op := env.newAdmin("owner-op")
	owner, _, _ := env.createUserSession("site-owner")
	createSite(ctx, t, env, "sole-owned", "Sole Owned", "", "zh-CN", owner.ID)

	if _, err := env.store.DisableAccount(ctx, op.ID, owner.ID); !errors.Is(err, store.ErrLastOwner) {
		t.Fatalf("disable sole owner: got %v, want ErrLastOwner", err)
	}
	if _, err := env.store.DeleteAccount(ctx, op.ID, owner.ID); !errors.Is(err, store.ErrLastOwner) {
		t.Fatalf("delete sole owner: got %v, want ErrLastOwner", err)
	}

	// Transferring (or sharing) ownership is the caller's duty; once another
	// owner exists the account is no longer irreplaceable.
	var siteID uuid.UUID
	if err := env.pool.QueryRow(ctx,
		`SELECT site_id FROM site_memberships WHERE user_id = $1 AND role = 'owner'`, owner.ID,
	).Scan(&siteID); err != nil {
		t.Fatalf("load owned site: %v", err)
	}
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'owner')`,
		siteID, op.ID); err != nil {
		t.Fatalf("add second owner: %v", err)
	}
	if _, err := env.store.DisableAccount(ctx, op.ID, owner.ID); err != nil {
		t.Fatalf("disable after ownership transfer: %v", err)
	}

	if _, err := env.store.DisableAccount(ctx, op.ID, op.ID); !errors.Is(err, store.ErrSelfManagement) {
		t.Fatalf("self disable: got %v, want ErrSelfManagement", err)
	}
	if _, err := env.store.DeleteAccount(ctx, op.ID, op.ID); !errors.Is(err, store.ErrSelfManagement) {
		t.Fatalf("self delete: got %v, want ErrSelfManagement", err)
	}

	mortal, _, _ := env.createUserSession("mortal")
	if _, err := env.store.DeleteAccount(ctx, op.ID, mortal.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := env.store.EnableAccount(ctx, op.ID, mortal.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("re-enable deleted: got %v, want ErrConflict", err)
	}
	if _, err := env.store.DeleteAccount(ctx, op.ID, mortal.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("re-delete deleted: got %v, want ErrConflict", err)
	}
}
