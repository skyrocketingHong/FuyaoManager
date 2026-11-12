package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// ErrDuplicateCredential means the verified credential is already stored
// (for this or another account); the client registered the same authenticator
// credential twice.
var ErrDuplicateCredential = errors.New("credential already registered")

// ErrSessionNotEligible means the session that requested the binding is gone,
// revoked, expired, or no longer belongs to the account (for example after a
// concurrent completion already rotated it away).
var ErrSessionNotEligible = errors.New("session is not eligible for passkey binding")

// CompletePasskeyBind finishes the session-bound passkey registration in one
// transaction: the new credential (tagged with the relying party it was
// registered under) is written, a fresh full session is minted, and the
// session that carried the ceremony is revoked. Other restricted sessions of
// the same account are deliberately NOT upgraded — each of them must
// complete (or restart) its own ceremony.
func (s *Store) CompletePasskeyBind(ctx context.Context, sessionID, userID uuid.UUID, pk model.Passkey, sessionTTL time.Duration) (ExchangeResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ExchangeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the carrying session and re-check it is still a live session of
	// this exact account. Concurrent completions of two ceremonies on the
	// same session: the first commit revokes the row, so the second one
	// fails here instead of issuing a second full session.
	var kind string
	err = tx.QueryRow(ctx,
		`SELECT kind FROM sessions
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > clock_timestamp()
		 FOR UPDATE`,
		sessionID, userID,
	).Scan(&kind)
	if err != nil {
		return ExchangeResult{}, ErrSessionNotEligible
	}

	pk.UserID = userID
	if _, err := tx.Exec(ctx,
		`INSERT INTO passkeys (`+passkeyColumns+`)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		newUUID(), pk.UserID, pk.RPID, pk.CredentialID, pk.PublicKey, pk.AttestationType, pk.AttestationFormat,
		pk.Transports, pk.SignCount, pk.BackupEligible, pk.BackupState, pk.UserVerified, pk.AAGUID,
		pk.LastUsedAt, time.Now(),
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ExchangeResult{}, ErrDuplicateCredential
		}
		return ExchangeResult{}, err
	}

	cookieToken := base32Code()
	csrf := base32Code()
	newSessionID := newUUID()
	var expiresAt time.Time
	if err := tx.QueryRow(ctx,
		`INSERT INTO sessions (id, user_id, kind, token_hash, csrf_token, expires_at)
		 VALUES ($1, $2, $3, $4, $5, now() + make_interval(secs => $6))
		 RETURNING expires_at`,
		newSessionID, userID, model.SessionKindFull, sha256Sum([]byte(cookieToken)), csrf, sessionTTL.Seconds(),
	).Scan(&expiresAt); err != nil {
		return ExchangeResult{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID,
	); err != nil {
		return ExchangeResult{}, err
	}

	var user model.User
	if err := tx.QueryRow(ctx,
		`SELECT id, username, display_name, webauthn_user_handle, status, created_at, updated_at
		 FROM users WHERE id = $1`, userID,
	).Scan(&user.ID, &user.Username, &user.DisplayName, &user.WebAuthnUserHandle,
		&user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return ExchangeResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ExchangeResult{}, err
	}
	return ExchangeResult{
		User:        user,
		SessionID:   newSessionID,
		Kind:        model.SessionKindFull,
		CookieToken: cookieToken,
		CSRFToken:   csrf,
		ExpiresAt:   expiresAt,
	}, nil
}
