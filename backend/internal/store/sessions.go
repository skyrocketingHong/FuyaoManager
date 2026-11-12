package store

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// SessionStore persists server-side sessions. The cookie token is stored as
// a SHA-256 digest only. The CSRF token is stored verbatim so every reader of
// the session (multiple tabs, reloads) receives the same stable value; see
// migration 0002 for the rationale.
type SessionStore struct {
	pool *pgxpool.Pool
}

// Create issues a new session for the user and returns the raw cookie token
// and CSRF token exactly once.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID, ttl time.Duration) (cookieToken string, csrf string, expiresAt time.Time, err error) {
	// The stored digest is computed over the exact text presented in the
	// cookie, so lookup agrees byte for byte. Both values are freshly
	// generated random material, never literals.
	cookieToken = base64.RawURLEncoding.EncodeToString(randomBytes(32))
	csrf = base64.RawURLEncoding.EncodeToString(randomBytes(32))
	id := newUUID()
	err = s.pool.QueryRow(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, csrf_token, expires_at)
		 VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))
		 RETURNING expires_at`,
		id, userID, sha256Sum([]byte(cookieToken)), csrf, ttl.Seconds(),
	).Scan(&expiresAt)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return cookieToken, csrf, expiresAt, nil
}

// AuthSession is a live session with its owning user.
type AuthSession struct {
	Session model.Session
	User    model.User
}

// GetAndTouch resolves a session cookie token to its session and user,
// requiring an active user and an unexpired, unrevoked session, and refreshes
// last_seen_at.
func (s *SessionStore) GetAndTouch(ctx context.Context, tokenHash []byte) (AuthSession, error) {
	var out AuthSession
	err := s.pool.QueryRow(ctx,
		`UPDATE sessions s
		 SET last_seen_at = now()
		 FROM users u
		 WHERE s.token_hash = $1
		   AND s.user_id = u.id
		   AND u.status = 'active'
		   AND s.revoked_at IS NULL
		   AND s.expires_at > clock_timestamp()
		   AND s.csrf_token <> ''
		 RETURNING s.id, s.user_id, s.csrf_token, s.created_at, s.last_seen_at, s.expires_at,
		           u.id, u.username, u.display_name, u.webauthn_user_handle, u.status, u.created_at, u.updated_at`,
		tokenHash,
	).Scan(&out.Session.ID, &out.Session.UserID, &out.Session.CSRFToken, &out.Session.CreatedAt,
		&out.Session.LastSeen, &out.Session.ExpiresAt,
		&out.User.ID, &out.User.Username, &out.User.DisplayName, &out.User.WebAuthnUserHandle,
		&out.User.Status, &out.User.CreatedAt, &out.User.UpdatedAt)
	if err != nil {
		return AuthSession{}, ErrNotFound
	}
	return out, nil
}

// Revoke marks a session revoked.
func (s *SessionStore) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}
