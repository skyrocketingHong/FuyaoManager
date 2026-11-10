package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuthStateStore persists short-lived WebAuthn ceremony state (the server
// side of a challenge). States are single-use: Consume atomically claims a
// live row exactly once, which is what makes challenges non-replayable.
type AuthStateStore struct {
	pool *pgxpool.Pool
}

// AuthState is a consumed ceremony state.
type AuthState struct {
	ID       uuid.UUID
	Kind     string
	UserID   *uuid.UUID
	PermitID *uuid.UUID
	Data     []byte
}

// Create stores ceremony state keyed by a fresh random ID that is echoed to
// the client in a short-lived cookie.
func (s *AuthStateStore) Create(ctx context.Context, kind string, userID, permitID *uuid.UUID, data []byte, ttl time.Duration) (uuid.UUID, error) {
	id := newUUID()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO auth_states (id, kind, user_id, permit_id, data, expires_at)
		 VALUES ($1, $2, $3, $4, $5, now() + make_interval(secs => $6))`,
		id, kind, userID, permitID, data, ttl.Seconds(),
	)
	return id, err
}

// Consume claims a live state. It returns ErrNotFound when the ID is unknown,
// already consumed, or expired; either way the challenge can no longer be
// used.
func (s *AuthStateStore) Consume(ctx context.Context, id uuid.UUID) (AuthState, error) {
	var state AuthState
	err := s.pool.QueryRow(ctx,
		`UPDATE auth_states
		 SET consumed_at = now()
		 WHERE id = $1 AND consumed_at IS NULL AND expires_at > clock_timestamp()
		 RETURNING kind, user_id, permit_id, data`,
		id,
	).Scan(&state.Kind, &state.UserID, &state.PermitID, &state.Data)
	if err != nil {
		return AuthState{}, ErrNotFound
	}
	state.ID = id
	return state, nil
}

// PurgeExpired removes expired ceremony states and finished sessions. It is
// called opportunistically when new ceremonies begin.
func (s *AuthStateStore) PurgeExpired(ctx context.Context) {
	_, _ = s.pool.Exec(ctx, `DELETE FROM auth_states WHERE expires_at < now()`)
	_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
}
