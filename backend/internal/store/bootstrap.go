package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// usernamePattern mirrors the users_username_format CHECK constraint.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

// ValidUsername reports whether username satisfies the format the database
// CHECK constraint enforces, so callers can reject invalid input with a
// structured error instead of relying on the constraint.
func ValidUsername(username string) bool {
	return usernamePattern.MatchString(username)
}

// BootstrapAdmin registers the first administrator account (reusing an
// existing pending row for the same username) and issues a single-use
// enrollment permit. It refuses once the platform is initialized, and the
// refusal is permanent: deactivating or deleting the administrators never
// reopens first-run bootstrap.
func (s *Store) BootstrapAdmin(ctx context.Context, username, displayName string, permitTTL time.Duration) (code string, expiresAt time.Time, err error) {
	code, _, expiresAt, err = s.BootstrapAdminWithPermit(ctx, username, displayName, permitTTL)
	return code, expiresAt, err
}

// BootstrapAdminWithPermit also returns the permit ID for callers (tests)
// that need to manipulate the permit row directly.
func (s *Store) BootstrapAdminWithPermit(ctx context.Context, username, displayName string, permitTTL time.Duration) (code string, permitID uuid.UUID, expiresAt time.Time, err error) {
	if !usernamePattern.MatchString(username) {
		return "", uuid.Nil, time.Time{}, fmt.Errorf("username must match %s", usernamePattern.String())
	}
	initialized, err := s.Users.HasInitializedUser(ctx)
	if err != nil {
		return "", uuid.Nil, time.Time{}, err
	}
	if initialized {
		return "", uuid.Nil, time.Time{}, errors.New("an administrator is already registered; bootstrap is only allowed for the first administrator")
	}
	// A reused pending row keeps its identity (UUID, WebAuthn user handle,
	// credentials) whatever path created it; the first-initialization
	// completion transactions (CompleteEnrollment and the init exchange)
	// assign the admin role in the same statement that activates it, so a
	// reused pending member row can never become the sole account as a
	// member.
	user, err := s.Users.GetByUsername(ctx, username)
	if err != nil {
		user, err = s.Users.CreateAdmin(ctx, username, displayName)
		if err != nil {
			return "", uuid.Nil, time.Time{}, err
		}
	}
	code, permitID, expiresAt, err = s.Permits.Create(ctx, user.ID, permitTTL)
	return code, permitID, expiresAt, err
}
