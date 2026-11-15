package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// Enrollment closure errors surfaced to the API layer.
var (
	// ErrBootstrapClosed means the platform is already initialized (any
	// account that is no longer pending exists), so the first-run enrollment
	// window is closed — permanently, also after the administrators were
	// deactivated or deleted.
	ErrBootstrapClosed = errors.New("an administrator is already registered")
	// ErrPermitExpired means the permit expired while its ceremony was in
	// flight; a live challenge cannot extend the enrollment authorization.
	ErrPermitExpired = errors.New("the enrollment permit has expired")
)

// EnrollmentLockKey serializes first-administrator enrollments. The advisory
// lock makes "the platform is not yet initialized" and the subsequent writes
// one atomic step, so two concurrent completions can never both succeed. It
// is exported so integration tests can hold the lock and probe the waiting
// behavior.
const EnrollmentLockKey = 0x66757961 // "fuya"

// CompleteEnrollment finishes first-run passkey enrollment atomically. Under
// one transaction and advisory lock it re-verifies every bootstrap invariant:
// the permit is unconsumed and unexpired, the platform is still
// uninitialized (no account is active, disabled, or deleted), and the user is
// still pending — a permit issued before the account was disabled or deleted
// can never activate it. Only then does it consume the permit, insert the
// credential, and activate the user as an admin in the same statement, so a
// reused pending member row can never become the sole account while staying
// a member. The returned User matches the persisted state (the UUID, the
// WebAuthn user handle, and every credential are untouched). Any failure
// rolls everything back.
func (s *Store) CompleteEnrollment(ctx context.Context, permitID uuid.UUID, user model.User, credential model.Passkey) (model.User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, EnrollmentLockKey); err != nil {
		return model.User{}, err
	}

	// clock_timestamp() (statement wall clock) rather than now() (transaction
	// start): time spent waiting for the advisory lock must not extend the
	// permit's validity window.
	tag, err := tx.Exec(ctx,
		`UPDATE enrollment_permits
		 SET used_at = now()
		 WHERE id = $1 AND used_at IS NULL AND expires_at > clock_timestamp()`,
		permitID)
	if err != nil {
		return model.User{}, err
	}
	if tag.RowsAffected() == 0 {
		var usedAt, expiresAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT used_at, expires_at FROM enrollment_permits WHERE id = $1`, permitID).Scan(&usedAt, &expiresAt); err != nil {
			return model.User{}, err
		}
		if usedAt != nil {
			return model.User{}, ErrPermitUsed
		}
		return model.User{}, ErrPermitExpired
	}

	var initialized bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE status <> 'pending')`).Scan(&initialized); err != nil {
		return model.User{}, err
	}
	if initialized {
		return model.User{}, ErrBootstrapClosed
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO passkeys (`+passkeyColumns+`)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		newUUID(), user.ID, credential.RPID, credential.CredentialID, credential.PublicKey, credential.AttestationType,
		credential.AttestationFormat, credential.Transports, credential.SignCount, credential.BackupEligible,
		credential.BackupState, credential.UserVerified, credential.AAGUID, credential.LastUsedAt, time.Now().UTC(),
	); err != nil {
		return model.User{}, err
	}

	// The activation is also the role assignment (same statement, same
	// transaction): first initialization always yields an admin, whichever
	// path created the pending row.
	var platformRole string
	err = tx.QueryRow(ctx,
		`UPDATE users SET status = 'active', platform_role = 'admin', updated_at = now()
		 WHERE id = $1 AND status = 'pending'
		 RETURNING platform_role`, user.ID,
	).Scan(&platformRole)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrConflict
		}
		return model.User{}, err
	}
	user.Status = model.UserStatusActive
	user.PlatformRole = platformRole

	return user, tx.Commit(ctx)
}
