package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// PasskeyStore persists WebAuthn credentials.
type PasskeyStore struct {
	pool *pgxpool.Pool
}

const passkeyColumns = `id, user_id, rp_id, credential_id, public_key, attestation_type, attestation_format,
	transports, sign_count, backup_eligible, backup_state, user_verified, aaguid, last_used_at, created_at`

// Create inserts a newly registered credential.
func (s *PasskeyStore) Create(ctx context.Context, pk model.Passkey) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO passkeys (`+passkeyColumns+`)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		newUUID(), pk.UserID, pk.RPID, pk.CredentialID, pk.PublicKey, pk.AttestationType, pk.AttestationFormat,
		pk.Transports, pk.SignCount, pk.BackupEligible, pk.BackupState, pk.UserVerified, pk.AAGUID,
		pk.LastUsedAt, time.Now().UTC(),
	)
	return err
}

// CountByUserRP reports how many credentials the user owns under the given
// relying-party ID. Zero means the account is not bound for that RP.
func (s *PasskeyStore) CountByUserRP(ctx context.Context, userID uuid.UUID, rpID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM passkeys WHERE user_id = $1 AND rp_id = $2`, userID, rpID,
	).Scan(&n)
	return n, err
}

// ListByUser returns all credentials owned by the user. The WebAuthn layer
// needs every credential to resolve discoverable logins.
func (s *PasskeyStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.Passkey, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+passkeyColumns+` FROM passkeys WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Passkey
	for rows.Next() {
		var pk model.Passkey
		if err := rows.Scan(&pk.ID, &pk.UserID, &pk.RPID, &pk.CredentialID, &pk.PublicKey, &pk.AttestationType,
			&pk.AttestationFormat, &pk.Transports, &pk.SignCount, &pk.BackupEligible, &pk.BackupState,
			&pk.UserVerified, &pk.AAGUID, &pk.LastUsedAt, &pk.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, pk)
	}
	return out, rows.Err()
}

// UpdateAfterLogin records the post-assertion signature counter and flags.
func (s *PasskeyStore) UpdateAfterLogin(ctx context.Context, credentialID []byte, signCount uint32, backupState, userVerified bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE passkeys
		 SET sign_count = $2, backup_state = $3, user_verified = user_verified OR $4, last_used_at = now()
		 WHERE credential_id = $1`,
		credentialID, signCount, backupState, userVerified,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
