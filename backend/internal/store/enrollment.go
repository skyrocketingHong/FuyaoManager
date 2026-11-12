package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// Enrollment closure errors surfaced to the API layer.
var (
	// ErrBootstrapClosed means an active administrator already exists, so the
	// first-run enrollment window is closed.
	ErrBootstrapClosed = errors.New("an administrator is already registered")
	// ErrPermitExpired means the permit expired while its ceremony was in
	// flight; a live challenge cannot extend the enrollment authorization.
	ErrPermitExpired = errors.New("the enrollment permit has expired")
)

// EnrollmentLockKey serializes first-administrator enrollments. The advisory
// lock makes "no active administrator exists" and the subsequent writes one
// atomic step, so two concurrent completions can never both succeed. It is
// exported so integration tests can hold the lock and probe the waiting
// behavior.
const EnrollmentLockKey = 0x66757961 // "fuya"

// CompleteEnrollment finishes first-run passkey enrollment atomically. Under
// one transaction and advisory lock it re-verifies every bootstrap invariant:
// the permit is unconsumed and unexpired, no active administrator exists, and
// the user is still pending. Only then does it consume the permit, insert the
// credential, and activate the user. Any failure rolls everything back.
func (s *Store) CompleteEnrollment(ctx context.Context, permitID uuid.UUID, user model.User, credential model.Passkey) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, EnrollmentLockKey); err != nil {
		return err
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
		return err
	}
	if tag.RowsAffected() == 0 {
		var usedAt, expiresAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT used_at, expires_at FROM enrollment_permits WHERE id = $1`, permitID).Scan(&usedAt, &expiresAt); err != nil {
			return err
		}
		if usedAt != nil {
			return ErrPermitUsed
		}
		return ErrPermitExpired
	}

	var hasActive bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE status = 'active')`).Scan(&hasActive); err != nil {
		return err
	}
	if hasActive {
		return ErrBootstrapClosed
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO passkeys (`+passkeyColumns+`)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		newUUID(), user.ID, credential.RPID, credential.CredentialID, credential.PublicKey, credential.AttestationType,
		credential.AttestationFormat, credential.Transports, credential.SignCount, credential.BackupEligible,
		credential.BackupState, credential.UserVerified, credential.AAGUID, credential.LastUsedAt, time.Now(),
	); err != nil {
		return err
	}

	tag, err = tx.Exec(ctx, `UPDATE users SET status = 'active', updated_at = now() WHERE id = $1 AND status = 'pending'`, user.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
