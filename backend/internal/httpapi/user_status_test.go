package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// setUserStatus flips an account's lifecycle status directly. The status
// management API does not exist yet; the regressions below pin the store-level
// refusals that must hold once it does, so raw SQL keeps the surface minimal.
func setUserStatus(t *testing.T, env *testEnv, userID uuid.UUID, status string) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE users SET status = $2, deleted_at = CASE WHEN $2 = 'deleted' THEN now() ELSE NULL END WHERE id = $1`,
		userID, status); err != nil {
		t.Fatalf("set user status %s: %v", status, err)
	}
}

// countPasskeys returns the number of stored credentials (whole schema).
func countPasskeys(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM passkeys`).Scan(&n); err != nil {
		t.Fatalf("count passkeys: %v", err)
	}
	return n
}

// TestDisabledAccountLoginCodeRefused proves deactivated and soft-deleted
// accounts can never receive one-time login codes.
func TestDisabledAccountLoginCodeRefused(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	user, _, _ := env.createUserSession("refused-" + model.UserStatusDisabled)
	setUserStatus(t, env, user.ID, model.UserStatusDisabled)
	if _, _, err := env.store.IssueLoginCode(ctx, user.Username, time.Minute); err == nil {
		t.Fatalf("disabled account received a login code")
	}

	user, _, _ = env.createUserSession("refused-" + model.UserStatusDeleted)
	setUserStatus(t, env, user.ID, model.UserStatusDeleted)
	if _, _, err := env.store.IssueLoginCode(ctx, user.Username, time.Minute); err == nil {
		t.Fatalf("deleted account received a login code")
	}
}

// TestDisabledAccountSessionRefused proves a live session stops resolving the
// moment its account is deactivated or soft-deleted — the session row stays,
// but GetAndTouch (the middleware read) refuses every non-active account.
func TestDisabledAccountSessionRefused(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	for _, status := range []string{model.UserStatusDisabled, model.UserStatusDeleted} {
		user, token, _ := env.createUserSession("session-" + status)
		digest := sha256Sum64(token)
		if _, err := env.store.Sessions.GetAndTouch(ctx, digest); err != nil {
			t.Fatalf("session resolves before %s: %v", status, err)
		}
		setUserStatus(t, env, user.ID, status)
		if _, err := env.store.Sessions.GetAndTouch(ctx, digest); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session after %s: got %v, want ErrNotFound", status, err)
		}
	}
}

// TestDisabledAccountInitCodeCannotActivate proves an init code issued for a
// pending account cannot activate it after the account was deactivated: the
// pending check fires before any write, so the code burn rolls back with the
// transaction.
func TestDisabledAccountInitCodeCannotActivate(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	code, _, err := env.store.InitAdmin(ctx, "victim", "victim", time.Minute)
	if err != nil {
		t.Fatalf("init admin: %v", err)
	}
	user, err := env.store.Users.GetByUsername(ctx, "victim")
	if err != nil {
		t.Fatalf("load victim: %v", err)
	}
	setUserStatus(t, env, user.ID, model.UserStatusDisabled)

	if _, err := env.store.ExchangeCode(ctx, code, time.Hour, "localhost", time.Minute); !errors.Is(err, store.ErrCodePurpose) {
		t.Fatalf("exchange init code for disabled account: got %v, want ErrCodePurpose", err)
	}
	after, err := env.store.Users.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload victim: %v", err)
	}
	if after.Status != model.UserStatusDisabled {
		t.Fatalf("disabled account changed status to %q", after.Status)
	}
}

// TestStalePermitCannotActivateInactiveAccount proves an enrollment permit
// issued before an account was deactivated or soft-deleted can never activate
// it: the bootstrap-closure check inside CompleteEnrollment counts every
// non-pending account, and the guarded activation update never fires.
func TestStalePermitCannotActivateInactiveAccount(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	cases := []struct {
		username string
		status   string
	}{
		{"stale-disabled", model.UserStatusDisabled},
		{"stale-deleted", model.UserStatusDeleted},
	}
	// Both permits are issued while the platform is still uninitialized;
	// the statuses flip afterwards, exactly like a permit that ages while an
	// operator deactivates the account.
	type staleCase struct {
		permitID uuid.UUID
		user     model.User
		status   string
	}
	var stales []staleCase
	for _, tc := range cases {
		_, permitID, _, err := env.store.BootstrapAdminWithPermit(ctx, tc.username, tc.username, 10*time.Minute)
		if err != nil {
			t.Fatalf("bootstrap %s: %v", tc.username, err)
		}
		user, err := env.store.Users.GetByUsername(ctx, tc.username)
		if err != nil {
			t.Fatalf("load %s: %v", tc.username, err)
		}
		stales = append(stales, staleCase{permitID: permitID, user: user, status: tc.status})
	}

	for _, sc := range stales {
		setUserStatus(t, env, sc.user.ID, sc.status)
		credential := model.Passkey{
			UserID:       sc.user.ID,
			RPID:         "localhost",
			CredentialID: []byte("cred-" + sc.user.Username),
			PublicKey:    []byte("pk"),
			Transports:   []string{},
		}
		if _, err := env.store.CompleteEnrollment(ctx, sc.permitID, sc.user, credential); !errors.Is(err, store.ErrBootstrapClosed) {
			t.Fatalf("complete enrollment for %s account: got %v, want ErrBootstrapClosed", sc.status, err)
		}
		after, err := env.store.Users.GetByID(ctx, sc.user.ID)
		if err != nil {
			t.Fatalf("reload %s: %v", sc.user.Username, err)
		}
		if after.Status != sc.status {
			t.Fatalf("%s account changed status to %q", sc.status, after.Status)
		}
	}
	if got := countPasskeys(t, env); got != 0 {
		t.Fatalf("refused enrollments wrote %d passkeys", got)
	}
}

// TestDisableDoesNotReopenBootstrap proves first-run bootstrap stays closed
// for good once the platform initialized: deactivating or soft-deleting the
// administrators never lets init-admin or bootstrap-admin run again.
func TestDisableDoesNotReopenBootstrap(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	disabled, _, _ := env.createUserSession("admin-disabled")
	setUserStatus(t, env, disabled.ID, model.UserStatusDisabled)
	deleted, _, _ := env.createUserSession("admin-deleted")
	setUserStatus(t, env, deleted.ID, model.UserStatusDeleted)

	if _, _, err := env.store.InitAdmin(ctx, "newcomer", "newcomer", time.Minute); err == nil {
		t.Fatalf("init admin reopened after administrators were removed")
	}
	if _, _, err := env.store.BootstrapAdmin(ctx, "newcomer", "newcomer", time.Minute); err == nil {
		t.Fatalf("bootstrap reopened after administrators were removed")
	}
}

// TestCompletePasskeyBindRefusesDisabledAccount proves a binding ceremony on a
// session that was valid when it started cannot complete once the account was
// deactivated in between: the locked user re-check refuses before any write.
func TestCompletePasskeyBindRefusesDisabledAccount(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	user, token, _ := env.createUserSession("bind-victim")
	var sessionID uuid.UUID
	if err := env.pool.QueryRow(ctx,
		`SELECT id FROM sessions WHERE token_hash = $1`, sha256Sum64(token)).Scan(&sessionID); err != nil {
		t.Fatalf("load bind session: %v", err)
	}
	setUserStatus(t, env, user.ID, model.UserStatusDisabled)

	credential := model.Passkey{
		UserID:       user.ID,
		RPID:         "localhost",
		CredentialID: []byte("cred-bind-victim"),
		PublicKey:    []byte("pk"),
		Transports:   []string{},
	}
	if _, err := env.store.CompletePasskeyBind(ctx, sessionID, user.ID, credential, time.Hour); !errors.Is(err, store.ErrSessionNotEligible) {
		t.Fatalf("bind for disabled account: got %v, want ErrSessionNotEligible", err)
	}
	if got := countPasskeys(t, env); got != 1 {
		t.Fatalf("refused bind wrote credentials: %d passkeys, want 1 (the seeded one)", got)
	}
}

// TestInitCodeActivatesReusedPendingMemberAsAdmin proves the init exchange
// assigns the admin role in the same statement that performs the first
// activation: a pending member row created by a non-bootstrap path is
// promoted when it is activated as the first account, and the returned User
// matches the persisted state while keeping its identity.
func TestInitCodeActivatesReusedPendingMemberAsAdmin(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	member, err := env.store.Users.Create(ctx, "reused-member", "reused-member")
	if err != nil {
		t.Fatalf("create pending member: %v", err)
	}
	if member.PlatformRole != model.PlatformRoleMember {
		t.Fatalf("precondition: role %q, want member", member.PlatformRole)
	}

	code, _, err := env.store.InitAdmin(ctx, "reused-member", "reused-member", time.Minute)
	if err != nil {
		t.Fatalf("init admin reusing pending row: %v", err)
	}
	result, err := env.store.ExchangeCode(ctx, code, time.Hour, "localhost", time.Minute)
	if err != nil {
		t.Fatalf("exchange init code: %v", err)
	}
	if result.User.ID != member.ID || result.User.Status != model.UserStatusActive ||
		result.User.PlatformRole != model.PlatformRoleAdmin {
		t.Fatalf("exchange result: id=%v status=%q role=%q, want same id, active, admin",
			result.User.ID, result.User.Status, result.User.PlatformRole)
	}

	persisted, err := env.store.Users.GetByID(ctx, member.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if persisted.Status != model.UserStatusActive || persisted.PlatformRole != model.PlatformRoleAdmin {
		t.Fatalf("persisted state: status=%q role=%q, want active, admin", persisted.Status, persisted.PlatformRole)
	}
	if !bytes.Equal(persisted.WebAuthnUserHandle, member.WebAuthnUserHandle) {
		t.Fatalf("webauthn user handle changed during reuse")
	}
}

// TestPermitEnrollmentActivatesReusedPendingMemberAsAdmin proves the permit
// enrollment path carries the same guarantee: completing first-run
// enrollment for a reused pending member row activates it as an admin and
// returns the User consistent with the persisted row.
func TestPermitEnrollmentActivatesReusedPendingMemberAsAdmin(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	member, err := env.store.Users.Create(ctx, "permit-member", "permit-member")
	if err != nil {
		t.Fatalf("create pending member: %v", err)
	}
	_, permitID, _, err := env.store.BootstrapAdminWithPermit(ctx, "permit-member", "permit-member", 10*time.Minute)
	if err != nil {
		t.Fatalf("bootstrap reusing pending row: %v", err)
	}

	credential := model.Passkey{
		UserID:       member.ID,
		RPID:         "localhost",
		CredentialID: []byte("cred-permit-member"),
		PublicKey:    []byte("pk"),
		Transports:   []string{},
	}
	activated, err := env.store.CompleteEnrollment(ctx, permitID, member, credential)
	if err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}
	if activated.ID != member.ID || activated.Status != model.UserStatusActive ||
		activated.PlatformRole != model.PlatformRoleAdmin {
		t.Fatalf("enrollment result: id=%v status=%q role=%q, want same id, active, admin",
			activated.ID, activated.Status, activated.PlatformRole)
	}
	if !bytes.Equal(activated.WebAuthnUserHandle, member.WebAuthnUserHandle) {
		t.Fatalf("webauthn user handle changed during enrollment")
	}

	persisted, err := env.store.Users.GetByID(ctx, member.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if persisted.Status != model.UserStatusActive || persisted.PlatformRole != model.PlatformRoleAdmin {
		t.Fatalf("persisted state: status=%q role=%q, want active, admin", persisted.Status, persisted.PlatformRole)
	}
}

// TestActivateOnlyTransitionsPending proves the generic Activate is strictly
// pending-to-active: it succeeds for pending rows (leaving the role alone —
// admin assignment belongs to the first-initialization completions) and
// refuses disabled or deleted rows without changing them.
func TestActivateOnlyTransitionsPending(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	pending, err := env.store.Users.Create(ctx, "activate-pending", "activate-pending")
	if err != nil {
		t.Fatalf("create pending member: %v", err)
	}
	if err := env.store.Users.Activate(ctx, pending.ID); err != nil {
		t.Fatalf("activate pending: %v", err)
	}
	activated, err := env.store.Users.GetByID(ctx, pending.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if activated.Status != model.UserStatusActive || activated.PlatformRole != model.PlatformRoleMember {
		t.Fatalf("generic activate must not grant a role: status=%q role=%q", activated.Status, activated.PlatformRole)
	}

	for _, status := range []string{model.UserStatusDisabled, model.UserStatusDeleted} {
		user, _, _ := env.createUserSession("blocked-" + status)
		setUserStatus(t, env, user.ID, status)
		if err := env.store.Users.Activate(ctx, user.ID); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("activate %s account: got %v, want ErrConflict", status, err)
		}
		after, err := env.store.Users.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if after.Status != status {
			t.Fatalf("%s account changed status to %q", status, after.Status)
		}
	}
}

// TestUpgradeFrom0012BackfillsPlatformRole proves the 0013 upgrade treats
// every pre-existing account as a bootstrap administrator: the backfill sets
// platform_role='admin' on all 0012 rows, deleted_at starts empty, the legacy
// pending row keeps activating through the ordinary init flow while keeping
// its admin role, and accounts created afterwards default to member.
func TestUpgradeFrom0012BackfillsPlatformRole(t *testing.T) {
	env := setupAtVersion(t, 12)
	ctx := context.Background()

	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		t.Fatalf("read randomness: %v", err)
	}
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO users (id, username, display_name, webauthn_user_handle, status)
		 VALUES ($1, 'legacy', 'legacy', $2, 'pending')`,
		uuid.New(), handle); err != nil {
		t.Fatalf("insert 0012 user: %v", err)
	}

	if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	var role string
	var deletedAt *time.Time
	if err := env.pool.QueryRow(ctx,
		`SELECT platform_role, deleted_at FROM users WHERE username = 'legacy'`).Scan(&role, &deletedAt); err != nil {
		t.Fatalf("read backfilled role: %v", err)
	}
	if role != model.PlatformRoleAdmin || deletedAt != nil {
		t.Fatalf("0012 backfill: role=%q deletedAt=%v, want admin and empty", role, deletedAt)
	}

	// The legacy pending row still activates through the ordinary init flow
	// (pending rows do not close the window) and keeps the admin role.
	if _, _, err := env.store.InitAdmin(ctx, "legacy", "legacy", time.Minute); err != nil {
		t.Fatalf("init admin for legacy row: %v", err)
	}
	legacy, err := env.store.Users.GetByUsername(ctx, "legacy")
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if legacy.PlatformRole != model.PlatformRoleAdmin {
		t.Fatalf("legacy role after reuse: %q, want admin", legacy.PlatformRole)
	}

	fresh, err := env.store.Users.Create(ctx, "newcomer", "newcomer")
	if err != nil {
		t.Fatalf("create fresh account: %v", err)
	}
	if fresh.PlatformRole != model.PlatformRoleMember {
		t.Fatalf("fresh account role: %q, want member", fresh.PlatformRole)
	}
}
