// Package store implements PostgreSQL persistence for the management MVP.
// Each store type owns the SQL for one aggregate; transactions that span
// aggregates live in the store that initiates them.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors mapped to HTTP statuses by the API layer.
var (
	// ErrNotFound means the requested row does not exist (or the caller has no
	// membership in it, which is reported the same way to avoid leaking site
	// existence).
	ErrNotFound = errors.New("record not found")
	// ErrConflict means an optimistic-concurrency check failed.
	ErrConflict = errors.New("record conflict")
	// ErrPermitInvalid means an enrollment permit is unknown, expired, or
	// exhausted.
	ErrPermitInvalid = errors.New("enrollment permit is invalid")
	// ErrPermitUsed means an enrollment permit was already consumed.
	ErrPermitUsed = errors.New("enrollment permit is already used")
)

// Store aggregates the individual record stores.
type Store struct {
	Pool       *pgxpool.Pool
	Users      *UserStore
	Passkeys   *PasskeyStore
	Sessions   *SessionStore
	AuthStates *AuthStateStore
	Permits    *PermitStore
	Sites      *SiteStore
	Themes     *ThemeStore
}

// New wires a Store around the given pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{
		Pool:       pool,
		Users:      &UserStore{pool: pool},
		Passkeys:   &PasskeyStore{pool: pool},
		Sessions:   &SessionStore{pool: pool},
		AuthStates: &AuthStateStore{pool: pool},
		Permits:    &PermitStore{pool: pool},
		Sites:      &SiteStore{pool: pool},
		Themes:     &ThemeStore{pool: pool},
	}
}

// newUUID returns a random UUIDv4.
func newUUID() uuid.UUID {
	id, err := uuid.NewRandom()
	if err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return id
}

// randomBytes returns n cryptographically random bytes.
func randomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return buf
}

// sha256Sum hashes a secret for storage.
func sha256Sum(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}
