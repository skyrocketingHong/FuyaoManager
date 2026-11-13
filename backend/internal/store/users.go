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

// UserStore persists platform accounts.
type UserStore struct {
	pool *pgxpool.Pool
}

const userColumns = `id, username, display_name, webauthn_user_handle, status, platform_role, deleted_at, created_at, updated_at`

func scanUser(row pgx.Row) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Create inserts a pending account with a fresh random WebAuthn user handle
// and the minimal platform privilege: every account created after the
// bootstrap administrators is a member.
func (s *UserStore) Create(ctx context.Context, username, displayName string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, username, display_name, webauthn_user_handle, platform_role)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+userColumns,
		newUUID(), username, displayName, randomBytes(32), model.PlatformRoleMember,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// CreateAdmin inserts a pending bootstrap administrator account. It is the
// only creation path of init-admin/bootstrap-admin, so every row it returns
// (or that a retry reuses from an earlier call) carries the admin role.
func (s *UserStore) CreateAdmin(ctx context.Context, username, displayName string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, username, display_name, webauthn_user_handle, platform_role)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+userColumns,
		newUUID(), username, displayName, randomBytes(32), model.PlatformRoleAdmin,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.WebAuthnUserHandle, &u.Status,
		&u.PlatformRole, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// GetByID loads an account by primary key.
func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

// GetByUsername loads an account by username.
func (s *UserStore) GetByUsername(ctx context.Context, username string) (model.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

// GetByHandle loads an account by WebAuthn user handle.
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
	return u, nil
}

// HasInitializedUser reports whether the platform has been initialized: any
// account that is no longer pending exists (active, disabled, or deleted).
// First-run bootstrap is only allowed while this is false, and the window
// stays closed once initialization happened — deactivating or deleting the
// last administrator never reopens anonymous bootstrap. Pending rows from
// failed first-run attempts do not close the window, so a retry with another
// username stays possible.
func (s *UserStore) HasInitializedUser(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE status <> 'pending')`).Scan(&exists)
	return exists, err
}

// ListActive returns every active platform administrator.
func (s *UserStore) ListActive(ctx context.Context) ([]model.User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users
		 WHERE platform_role = 'admin' AND status = 'active'
		 ORDER BY username`)
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

// CountActive returns the number of active platform administrators.
func (s *UserStore) CountActive(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE platform_role = 'admin' AND status = 'active'`).Scan(&n)
	return n, err
}

// Activate marks a pending account active and refuses anything else: a
// disabled or deleted account is never reactivated through this generic
// path — an authorized re-enable belongs to a dedicated, audited store
// method of the account-management surface.
func (s *UserStore) Activate(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET status = 'active', updated_at = now() WHERE id = $1 AND status = 'pending'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}
