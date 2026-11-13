package store

import (
	"context"
	"errors"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// InitAdmin registers the first administrator account and issues a one-time
// initialization code (purpose "init"). It refuses once the platform is
// initialized, and the refusal is permanent — deactivating or deleting the
// administrators never reopens first-run binding, so the first anonymous
// browser visitor can never claim it.
func (s *Store) InitAdmin(ctx context.Context, username, displayName string, codeTTL time.Duration) (code string, expiresAt time.Time, err error) {
	if !usernamePattern.MatchString(username) {
		return "", time.Time{}, errors.New("username must match " + usernamePattern.String())
	}
	initialized, err := s.Users.HasInitializedUser(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	if initialized {
		return "", time.Time{}, errors.New("an administrator is already registered; use `fuyao-admin login-code` instead")
	}
	// A reused pending row keeps its identity (UUID, WebAuthn user handle,
	// credentials) whatever path created it; the first-initialization
	// completion transactions (the init exchange and CompleteEnrollment)
	// assign the admin role in the same statement that activates it, so a
	// reused pending member row can never become the sole account as a
	// member.
	user, err := s.Users.GetByUsername(ctx, username)
	if err != nil {
		user, err = s.Users.CreateAdmin(ctx, username, displayName)
		if err != nil {
			return "", time.Time{}, err
		}
	}
	code, _, expiresAt, err = s.AuthCodes.Create(ctx, user.ID, AuthCodePurposeInit, codeTTL)
	return code, expiresAt, err
}

// IssueLoginCode issues a per-login one-time code (purpose "login") for an
// already-active user. Pending users cannot receive login codes: their only
// path in is a fresh initialization code from the CLI while bootstrap is
// open. Disabled and deleted accounts are refused as well — they can never
// authenticate again.
func (s *Store) IssueLoginCode(ctx context.Context, username string, codeTTL time.Duration) (code string, expiresAt time.Time, err error) {
	user, err := s.Users.GetByUsername(ctx, username)
	if err != nil {
		return "", time.Time{}, err
	}
	if user.Status != model.UserStatusActive {
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
