package store

import (
	"context"
	"errors"
	"time"
)

// InitAdmin registers the first administrator account and issues a one-time
// initialization code (purpose "init"). It refuses once an active
// administrator exists, keeping first-run binding a one-shot CLI-authorized
// operation: the first anonymous browser visitor can never claim it.
func (s *Store) InitAdmin(ctx context.Context, username, displayName string, codeTTL time.Duration) (code string, expiresAt time.Time, err error) {
	if !usernamePattern.MatchString(username) {
		return "", time.Time{}, errors.New("username must match " + usernamePattern.String())
	}
	hasActive, err := s.Users.HasActiveUser(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	if hasActive {
		return "", time.Time{}, errors.New("an administrator is already registered; use `fuyao-admin login-code` instead")
	}
	user, err := s.Users.GetByUsername(ctx, username)
	if err != nil {
		user, err = s.Users.Create(ctx, username, displayName)
		if err != nil {
			return "", time.Time{}, err
		}
	}
	code, _, expiresAt, err = s.AuthCodes.Create(ctx, user.ID, AuthCodePurposeInit, codeTTL)
	return code, expiresAt, err
}

// IssueLoginCode issues a per-login one-time code (purpose "login") for an
// already-active user. Pending users cannot receive login codes: their only
// path in is a fresh initialization code from the CLI while bootstrap is open.
func (s *Store) IssueLoginCode(ctx context.Context, username string, codeTTL time.Duration) (code string, expiresAt time.Time, err error) {
	user, err := s.Users.GetByUsername(ctx, username)
	if err != nil {
		return "", time.Time{}, err
	}
	if user.Status != "active" {
		return "", time.Time{}, errors.New("user " + username + " has not completed initialization")
	}
	code, _, expiresAt, err = s.AuthCodes.Create(ctx, user.ID, AuthCodePurposeLogin, codeTTL)
	return code, expiresAt, err
}

// RevokeUserSessions revokes every live session of the user and returns how
// many rows were revoked.
func (s *Store) RevokeUserSessions(ctx context.Context, username string) (int64, error) {
	user, err := s.Users.GetByUsername(ctx, username)
	if err != nil {
		return 0, err
	}
	tag, err := s.Pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now()
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > clock_timestamp()`,
		user.ID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
