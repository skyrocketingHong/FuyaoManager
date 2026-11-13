// Administrative account lifecycle: the store-side write policies for the
// account-management surface. Every write re-checks the operator (active +
// admin) inside its transaction; the per-file advisory lock serializes all
// lifecycle writes so last-admin decisions are race-free.
//
// Lifecycle writes take the advisory lock, then credential-adjacent rows,
// then the user row, aligning with the existing code and binding ceremonies.
// Issuers lock and re-check account status before inserting. Re-enabling
// sweeps artifacts again to revoke late inserts from the deactivation window.
// Site ownership writes share the advisory lock so the last-owner check sees
// committed memberships and account states.

package store

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// Account lifecycle errors surfaced to the API layer.
var (
	// ErrOperatorForbidden means the transaction's operator is not an active
	// administrator; every account-management write requires one.
	ErrOperatorForbidden = errors.New("operator must be an active administrator")
	// ErrSelfManagement means an administrator may not disable or delete
	// their own account (re-enable, role, and profile edits stay allowed).
	ErrSelfManagement = errors.New("administrators cannot disable or delete their own account")
	// ErrLastAdmin means the operation would remove the last valid
	// administrator (active + admin). Another administrator must be promoted
	// first.
	ErrLastAdmin = errors.New("cannot remove the last valid administrator")
	// ErrLastOwner means the account is the sole owner of at least one site;
	// ownership must be transferred before the account can be disabled,
	// deleted, or otherwise removed from its sites.
	ErrLastOwner = errors.New("cannot remove the last valid site owner")
)

// AccountLifecycleLockKey serializes account-management writes. Distinct from
// EnrollmentLockKey: lifecycle operations require an initialized platform,
// first-initialization ceremonies require an uninitialized one, so the two
// lock domains never contend.
const AccountLifecycleLockKey = 0x61636374 // "acct"

const maxDisplayNameLength = 200 // users_display_name_length CHECK

// beginAccountLifecycle opens the serialized lifecycle transaction and
// re-checks the operator under the advisory lock.
func (s *Store) beginAccountLifecycle(ctx context.Context, operatorID uuid.UUID) (pgx.Tx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AccountLifecycleLockKey); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	var status, role string
	err = tx.QueryRow(ctx,
		`SELECT status, platform_role FROM users WHERE id = $1`, operatorID,
	).Scan(&status, &role)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != model.UserStatusActive || role != model.PlatformRoleAdmin {
		_ = tx.Rollback(ctx)
		return nil, ErrOperatorForbidden
	}
	return tx, nil
}

// revokeAccountArtifacts expires every live credential-adjacent artifact of
// the account: sessions, unused one-time codes, unused enrollment permits,
// and unconsumed ceremony states. Rows are updated in place (never deleted)
// so audit history survives; unused codes and permits expire immediately,
// which is how this schema revokes them.
func revokeAccountArtifacts(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	stmts := []string{
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		`UPDATE auth_codes SET expires_at = now() WHERE user_id = $1 AND used_at IS NULL`,
		`UPDATE enrollment_permits SET expires_at = now() WHERE user_id = $1 AND used_at IS NULL`,
		`UPDATE auth_states SET expires_at = now() WHERE user_id = $1 AND consumed_at IS NULL`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt, userID); err != nil {
			return err
		}
	}
	return nil
}

// insertAccountCode mints a one-time login code inside a transaction,
// mirroring AuthCodeStore.Create. The raw code is returned exactly once and
// only its digest is stored.
func insertAccountCode(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ttl time.Duration) (string, time.Time, error) {
	raw := randomBytes(10)
	code := formatCode(base32.StdEncoding.EncodeToString(raw))
	var expiresAt time.Time
	err := tx.QueryRow(ctx,
		`INSERT INTO auth_codes (id, user_id, purpose, code_hash, expires_at)
		 VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))
		 RETURNING expires_at`,
		newUUID(), userID, AuthCodePurposeLogin, sha256Sum([]byte(code)), ttl.Seconds(),
	).Scan(&expiresAt)
	return code, expiresAt, err
}

// countValidAdmins reports how many active administrators exist. Reliable
// only under the lifecycle lock: no other writer can create or remove a
// valid admin while it is held (first-initialization completions refuse to
// run once an administrator exists).
func countValidAdmins(ctx context.Context, tx pgx.Tx) (int, error) {
	var n int
	err := tx.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE platform_role = 'admin' AND status = 'active'`).Scan(&n)
	return n, err
}

// isSoleSiteOwner reports whether removing the account from active service
// would leave any of its sites without an active owner: the account holds an
// owner membership and no OTHER active owner exists for that site. Inactive
// owners do not count as available ones — a co-owner that is disabled or
// deleted cannot take over, so the last active owner stays protected. Site
// creation participates in the same lifecycle lock, so committed sites are
// always visible here. Platform roles and site memberships are separate:
// this protects site ownership, not administrator counts.
func isSoleSiteOwner(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM site_memberships m
		   WHERE m.user_id = $1 AND m.role = 'owner'
		     AND NOT EXISTS (
		       SELECT 1 FROM site_memberships o
		       JOIN users ou ON ou.id = o.user_id
		       WHERE o.site_id = m.site_id AND o.role = 'owner'
		         AND ou.status = 'active' AND ou.id <> $1
		     )
		 )`, userID,
	).Scan(&exists)
	return exists, err
}

// validateAccountFields applies the existing username format validation and
// the display-name length bound before any statement runs.
func validateAccountFields(username, displayName string) error {
	if !ValidUsername(username) {
		return fmt.Errorf("username must match %s", usernamePattern.String())
	}
	if utf8.RuneCountInString(displayName) > maxDisplayNameLength {
		return fmt.Errorf("display name must be at most %d characters", maxDisplayNameLength)
	}
	return nil
}

// CreateAccountWithCode inserts an active member account and issues its
// first one-time login code in one transaction. New accounts carry no
// password: the code redeems exactly like a CLI login code, and because the
// account owns no credential for the current relying party the exchange
// mints a restricted binding session. The raw code is returned exactly once.
func (s *Store) CreateAccountWithCode(ctx context.Context, operatorID uuid.UUID, username, displayName string, codeTTL time.Duration) (model.User, string, time.Time, error) {
	if err := validateAccountFields(username, displayName); err != nil {
		return model.User{}, "", time.Time{}, err
	}
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return model.User{}, "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var u model.User
	err = tx.QueryRow(ctx,
		`INSERT INTO users (id, username, display_name, webauthn_user_handle, status, platform_role)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+userColumns,
		newUUID(), username, displayName, randomBytes(32), model.UserStatusActive, model.PlatformRoleMember,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, "", time.Time{}, ErrUsernameTaken
		}
		return model.User{}, "", time.Time{}, err
	}

	code, expiresAt, err := insertAccountCode(ctx, tx, u.ID, codeTTL)
	if err != nil {
		return model.User{}, "", time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, "", time.Time{}, err
	}
	return u, code, expiresAt, nil
}

// IssueAccountCode re-issues a one-time login code for an active account,
// revoking its unused codes first so exactly one live code exists per
// issuance. Disabled, deleted, and pending accounts are refused; the raw
// code is returned exactly once and never logged.
func (s *Store) IssueAccountCode(ctx context.Context, operatorID, targetID uuid.UUID, codeTTL time.Duration) (string, time.Time, error) {
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM users WHERE id = $1`, targetID,
	).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, ErrNotFound
		}
		return "", time.Time{}, err
	}
	if status != model.UserStatusActive {
		return "", time.Time{}, ErrConflict
	}

	// Child rows before any user-row write, per the lifecycle lock order.
	if _, err := tx.Exec(ctx,
		`UPDATE auth_codes SET expires_at = now() WHERE user_id = $1 AND used_at IS NULL`, targetID,
	); err != nil {
		return "", time.Time{}, err
	}
	code, expiresAt, err := insertAccountCode(ctx, tx, targetID, codeTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, err
	}
	return code, expiresAt, nil
}

// AdminUpdateAccount sets the username and display name of one account in a
// single guarded statement; the UUID, the WebAuthn user handle, every
// passkey, and all memberships are untouched, and the username's UNIQUE
// constraint rejects collisions. The operator re-check runs in the same
// transaction.
func (s *Store) AdminUpdateAccount(ctx context.Context, operatorID, targetID uuid.UUID, username, displayName string) (model.User, error) {
	if err := validateAccountFields(username, displayName); err != nil {
		return model.User{}, err
	}
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var u model.User
	err = tx.QueryRow(ctx,
		`UPDATE users SET username = $2, display_name = $3, updated_at = now()
		 WHERE id = $1
		 RETURNING `+userColumns,
		targetID, username, displayName,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, ErrUsernameTaken
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return u, nil
}

// AdminSetPlatformRole sets the platform role of an active account. Demoting
// the last valid administrator is refused; self-demotion is allowed while
// another valid administrator exists. Roles and site memberships are
// separate — this never touches site ownership. The change is visible to
// every reader immediately after commit (sessions resolve the user row on
// every request).
func (s *Store) AdminSetPlatformRole(ctx context.Context, operatorID, targetID uuid.UUID, role string) (model.User, error) {
	if role != model.PlatformRoleAdmin && role != model.PlatformRoleMember {
		return model.User{}, fmt.Errorf("platform role must be %s or %s", model.PlatformRoleAdmin, model.PlatformRoleMember)
	}
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var target model.User
	err = tx.QueryRow(ctx,
		`SELECT id, username, display_name, webauthn_user_handle, status, platform_role, deleted_at, created_at, updated_at
		 FROM users WHERE id = $1 FOR UPDATE`, targetID,
	).Scan(&target.ID, &target.Username, &target.DisplayName, &target.WebAuthnUserHandle, &target.Status,
		&target.PlatformRole, &target.DeletedAt, &target.CreatedAt, &target.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	if target.Status != model.UserStatusActive {
		return model.User{}, ErrConflict
	}
	if target.PlatformRole == model.PlatformRoleAdmin && role == model.PlatformRoleMember {
		n, err := countValidAdmins(ctx, tx)
		if err != nil {
			return model.User{}, err
		}
		if n <= 1 {
			return model.User{}, ErrLastAdmin
		}
	}

	var u model.User
	err = tx.QueryRow(ctx,
		`UPDATE users SET platform_role = $2, updated_at = now()
		 WHERE id = $1
		 RETURNING `+userColumns,
		targetID, role,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return model.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return u, nil
}

// DisableAccount deactivates an active account and revokes its live
// artifacts: sessions, unused one-time codes, unused enrollment permits, and
// unconsumed ceremony states. Self-disable, removing the last valid
// administrator, and removing the sole owner of any site are refused. The
// account, its UUID, credentials, and history all stay.
func (s *Store) DisableAccount(ctx context.Context, operatorID, targetID uuid.UUID) (model.User, error) {
	return s.deactivateAccount(ctx, operatorID, targetID, model.UserStatusDisabled)
}

// DeleteAccount soft-deletes an account (from active or disabled state) with
// the same operator, self, last-admin, last-owner, and revocation rules as
// disabling. A deleted account is terminal: it can never be restored.
func (s *Store) DeleteAccount(ctx context.Context, operatorID, targetID uuid.UUID) (model.User, error) {
	return s.deactivateAccount(ctx, operatorID, targetID, model.UserStatusDeleted)
}

// deactivateAccount carries the shared disable/delete policy. targetStatus is
// the lifecycle destination.
func (s *Store) deactivateAccount(ctx context.Context, operatorID, targetID uuid.UUID, targetStatus string) (model.User, error) {
	if operatorID == targetID {
		return model.User{}, ErrSelfManagement
	}
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var target model.User
	err = tx.QueryRow(ctx,
		`SELECT id, username, display_name, webauthn_user_handle, status, platform_role, deleted_at, created_at, updated_at
		 FROM users WHERE id = $1`, targetID,
	).Scan(&target.ID, &target.Username, &target.DisplayName, &target.WebAuthnUserHandle, &target.Status,
		&target.PlatformRole, &target.DeletedAt, &target.CreatedAt, &target.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	// Disable only takes an active account; deletion additionally accepts a
	// disabled one. Pending rows are bootstrap artifacts, deleted is
	// terminal.
	switch target.Status {
	case model.UserStatusActive:
	case model.UserStatusDisabled:
		if targetStatus != model.UserStatusDeleted {
			return model.User{}, ErrConflict
		}
	default:
		return model.User{}, ErrConflict
	}
	if target.PlatformRole == model.PlatformRoleAdmin {
		n, err := countValidAdmins(ctx, tx)
		if err != nil {
			return model.User{}, err
		}
		// Only an active administrator is a valid one; deactivating the
		// last active admin would leave the platform unmanageable.
		if target.Status == model.UserStatusActive && n <= 1 {
			return model.User{}, ErrLastAdmin
		}
	}
	if sole, err := isSoleSiteOwner(ctx, tx, targetID); err != nil {
		return model.User{}, err
	} else if sole {
		return model.User{}, ErrLastOwner
	}

	// Child rows first, user row last — the ceremony lock order.
	if err := revokeAccountArtifacts(ctx, tx, targetID); err != nil {
		return model.User{}, err
	}
	var u model.User
	err = tx.QueryRow(ctx,
		`UPDATE users SET status = $2, deleted_at = CASE WHEN $2 = 'deleted' THEN now() ELSE deleted_at END,
		    updated_at = now()
		 WHERE id = $1 AND status IN ('active', 'disabled')
		 RETURNING `+userColumns,
		targetID, targetStatus,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrConflict
		}
		return model.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return u, nil
}

// EnableAccount re-enables a disabled account. Deleted accounts are terminal
// and can never be restored through any path. Re-enabling re-runs the
// artifact sweep first: a session, code, or ceremony state that raced the
// disable (inserted after its sweep ran, while the account was already on its
// way down) is inert while the account is disabled and must not revive with
// it — nothing issued against the deactivated account survives the cycle.
func (s *Store) EnableAccount(ctx context.Context, operatorID, targetID uuid.UUID) (model.User, error) {
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM users WHERE id = $1`, targetID,
	).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	if status != model.UserStatusDisabled {
		return model.User{}, ErrConflict
	}

	// Child rows first, user row last — the lifecycle lock order.
	if err := revokeAccountArtifacts(ctx, tx, targetID); err != nil {
		return model.User{}, err
	}
	var u model.User
	err = tx.QueryRow(ctx,
		`UPDATE users SET status = 'active', updated_at = now()
		 WHERE id = $1 AND status = 'disabled'
		 RETURNING `+userColumns,
		targetID,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return model.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return u, nil
}
