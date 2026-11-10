package httpapi

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/testauth"
)

// setupAtVersion provisions the test world at an older schema version by
// applying migrations up to the given version only.
func setupAtVersion(t *testing.T, version int) *testEnv {
	t.Helper()
	return setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrationsUpTo(ctx, env.pool, version); err != nil {
			t.Fatalf("apply migrations up to %d: %v", version, err)
		}
	})
}

// insertV1Session writes a schema-0001 session row (csrf_hash digest, no
// csrf_token) for a user, exactly as the pre-upgrade backend would have.
func insertV1Session(t *testing.T, env *testEnv, userID uuid.UUID, token string) {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	if _, err := env.pool.Exec(context.Background(),
		`INSERT INTO sessions (id, user_id, token_hash, csrf_hash, expires_at)
		 VALUES ($1, $2, $3, $4, now() + interval '1 hour')`,
		uuid.New(), userID, digest[:], digest[:]); err != nil {
		t.Fatalf("insert v1 session: %v", err)
	}
}

// seedPasskey persists a synthetic credential directly (upgrade scenarios
// cannot reuse the closed bootstrap flow for an already-active user).
func seedPasskey(t *testing.T, env *testEnv, userID uuid.UUID, auth *testauth.Authenticator) {
	t.Helper()
	credID, publicKey := auth.ExportCredential()
	if _, err := env.pool.Exec(context.Background(),
		`INSERT INTO passkeys (id, user_id, credential_id, public_key, attestation_type, attestation_format,
		 transports, sign_count, backup_eligible, backup_state, user_verified, aaguid, last_used_at, created_at)
		 VALUES ($1, $2, $3, $4, 'none', 'none', '{}', 0, false, false, true, NULL, now(), now())`,
		uuid.New(), userID, credID, publicKey); err != nil {
		t.Fatalf("seed passkey: %v", err)
	}
}

// TestSessionUpgradeFromV1 proves the 0002 migration gives pre-existing v1
// sessions a clean recovery path: after the upgrade they resolve as
// anonymous (GET /api/v1/session -> authenticated:false), never as
// authenticated-but-unwritable, and a fresh passkey login works immediately.
func TestSessionUpgradeFromV1(t *testing.T) {
	env := setupAtVersion(t, 1)
	ctx := context.Background()

	user, err := env.store.Users.Create(ctx, "legacy", "legacy")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := env.store.Users.Activate(ctx, user.ID); err != nil {
		t.Fatalf("activate user: %v", err)
	}
	legacyToken := "legacy-session-token-value"
	insertV1Session(t, env, user.ID, legacyToken)
	auth := testauth.New()
	seedPasskey(t, env, user.ID, auth)

	// Upgrade to the current schema (0002 removes v1 sessions).
	if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	// The stale cookie now resolves as anonymous, not half-authenticated.
	c := env.newClient()
	c.token = legacyToken
	status, _, body := c.do("GET", "/api/v1/session", nil, nil)
	if status != 200 {
		t.Fatalf("session probe after upgrade: %d %s", status, body)
	}
	var session sessionResp
	decodeJSONBody(t, body, &session)
	if session.Authenticated || session.CSRFToken != "" {
		t.Fatalf("v1 session must resolve anonymous after upgrade: %s", body)
	}

	// Mutating with the stale cookie is a plain 401.
	status, _, body = c.do("GET", "/api/v1/sites", nil, nil)
	if status != 401 {
		t.Fatalf("sites with stale cookie: %d %s, want 401", status, body)
	}

	// Recovery: the legacy administrator signs in with the passkey right away.
	status, _, body = c.do("POST", "/api/v1/auth/login/options", map[string]string{}, nil)
	if status != 200 {
		t.Fatalf("login options: %d %s", status, body)
	}
	var options requestOptionsResp
	decodeJSONBody(t, body, &options)
	c.token = ""
	assertion := auth.Assert("localhost", env.base, options.PublicKey.Challenge, user.WebAuthnUserHandle)
	status, _, body = c.do("POST", "/api/v1/auth/login/verify", assertion, nil)
	if status != 200 {
		t.Fatalf("login after upgrade: %d %s", status, body)
	}
	var done ceremonyDoneResp
	decodeJSONBody(t, body, &done)
	if done.User.Username != "legacy" {
		t.Fatalf("unexpected user after re-login: %s", body)
	}
}

// TestPermitExpiryWhileWaitingForLock proves the enrollment transaction
// judges permit expiry with the wall clock: a permit that expires while the
// request waits for the advisory lock is rejected, never granted.
func TestPermitExpiryWhileWaitingForLock(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	c := env.newClient()

	code, permitID, _, err := env.store.BootstrapAdminWithPermit(ctx, "locked", "locked", 10*time.Minute)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	status, _, body := c.do("POST", "/api/v1/auth/register/options", map[string]string{"permit": code}, nil)
	if status != 200 {
		t.Fatalf("options: %d %s", status, body)
	}
	var options creationOptionsResp
	decodeJSONBody(t, body, &options)
	auth := testauth.New()
	response := auth.CreateCredential("localhost", env.base, options.PublicKey.Challenge)

	// Shrink the permit to a 1.5s window, then hold the enrollment lock
	// long enough that the verify transaction resumes after expiry.
	if _, err := env.pool.Exec(ctx,
		`UPDATE enrollment_permits SET expires_at = clock_timestamp() + interval '1500 milliseconds' WHERE id = $1`,
		permitID); err != nil {
		t.Fatalf("shrink permit: %v", err)
	}
	holder, err := env.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire holder conn: %v", err)
	}
	defer holder.Release()
	lockTx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, store.EnrollmentLockKey); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	resultStatus := make(chan int, 1)
	go func() {
		s, _, _ := c.do("POST", "/api/v1/auth/register/verify", response, nil)
		resultStatus <- s
	}()

	time.Sleep(2200 * time.Millisecond)        // the permit expires while verify waits
	if err := lockTx.Commit(ctx); err != nil { // release the lock; verify resumes
		t.Fatalf("holder commit: %v", err)
	}

	got := <-resultStatus
	if got != 409 {
		t.Fatalf("verify after lock wait: got %d, want 409", got)
	}
	var passkeys, active int
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM passkeys`).Scan(&passkeys)
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE status='active'`).Scan(&active)
	if passkeys != 0 || active != 0 {
		t.Fatalf("expired-under-lock permit must not write state: passkeys=%d active=%d", passkeys, active)
	}
}
