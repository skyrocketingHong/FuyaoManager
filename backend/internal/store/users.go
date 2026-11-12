package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// ErrUsernameTaken marks an account rename whose username collides with
// another account's (the users.username UNIQUE constraint).
var ErrUsernameTaken = errors.New("username already taken")

// UserStore persists administrator accounts.
type UserStore struct {
	pool *pgxpool.Pool
}

const userColumns = `id, username, display_name, webauthn_user_handle, status, created_at, updated_at`

func scanUser(row pgx.Row) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Create inserts a pending user with a fresh random WebAuthn user handle.
func (s *UserStore) Create(ctx context.Context, username, displayName string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, username, display_name, webauthn_user_handle)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+userColumns,
		newUUID(), username, displayName, randomBytes(32),
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// GetByID loads a user by primary key.
func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

// GetByUsername loads a user by username.
func (s *UserStore) GetByUsername(ctx context.Context, username string) (model.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

// GetByHandle loads a user by WebAuthn user handle.
func (s *UserStore) GetByHandle(ctx context.Context, handle []byte) (model.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE webauthn_user_handle = $1`, handle)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

// UpdateUsername renames one account in a single atomic statement. The
// immutable columns — the UUID, the WebAuthn user handle, every passkey
// credential, and the role memberships — are untouched; the username's
// UNIQUE constraint rejects a collision with another account.
func (s *UserStore) UpdateUsername(ctx context.Context, id uuid.UUID, username string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`UPDATE users SET username = $2, updated_at = now()
		 WHERE id = $1
		 RETURNING `+userColumns,
		id, username,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status, &u.CreatedAt, &u.UpdatedAt)
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
	return u, nil
}

// HasActiveUser reports whether any administrator has completed passkey
// registration. First-run bootstrap is only allowed while this is false.
func (s *UserStore) HasActiveUser(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE status = 'active')`).Scan(&exists)
	return exists, err
}

// ListActive returns every active administrator.
func (s *UserStore) ListActive(ctx context.Context) ([]model.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users WHERE status = 'active' ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountActive returns the number of active administrators.
func (s *UserStore) CountActive(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE status = 'active'`).Scan(&n)
	return n, err
}

// Activate marks a pending user active.
func (s *UserStore) Activate(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET status = 'active', updated_at = now() WHERE id = $1`, id)
	return err
}
