package store

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
)

// One-time code purposes. An "init" code binds the first administrator
// identity created by the CLI and may only activate a still-pending user
// while no active administrator exists; a "login" code opens a session for an
// already-active user. The two purposes are not interchangeable.
const (
	AuthCodePurposeInit  = "init"
	AuthCodePurposeLogin = "login"
)

// MaxCodeAttempts bounds how many times a known code may be presented (each
// presentation that reaches the row burns one attempt) before it is locked.
// Exported for tests that drive the lock behavior.
const MaxCodeAttempts = 6

// Code exchange errors surfaced to the API layer.
var (
	// ErrCodeInvalid means the presented code matches no row.
	ErrCodeInvalid = errors.New("authentication code is unknown")
	// ErrCodeUsed means the code was already consumed exactly once.
	ErrCodeUsed = errors.New("authentication code already used")
	// ErrCodeExpired means the code's validity window passed.
	ErrCodeExpired = errors.New("authentication code expired")
	// ErrCodeLocked means the code exhausted its presentation attempts.
	ErrCodeLocked = errors.New("authentication code locked")
	// ErrCodePurpose means the code is valid but its purpose does not match
	// the target user's state (e.g. a login code for a pending user).
	ErrCodePurpose = errors.New("authentication code purpose mismatch")
)

// AuthCodeStore persists CLI-issued one-time authentication codes. Only the
// SHA-256 digest of the code is stored; the raw code is returned exactly once
// to the CLI that created it.
type AuthCodeStore struct {
	pool *pgxpool.Pool
}

// Create issues a one-time code for the user and purpose, returning the raw
// code exactly once. The code is formatted XXXX-XXXX-XXXX-XXXX (80 bits of
// entropy), the same format as enrollment permits.
func (s *AuthCodeStore) Create(ctx context.Context, userID uuid.UUID, purpose string, ttl time.Duration) (code string, id uuid.UUID, expiresAt time.Time, err error) {
	raw := randomBytes(10)
	code = formatCode(base32.StdEncoding.EncodeToString(raw))
	id = newUUID()
	err = s.pool.QueryRow(ctx,
		`INSERT INTO auth_codes (id, user_id, purpose, code_hash, expires_at)
		 VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))
		 RETURNING expires_at`,
		id, userID, purpose, sha256Sum([]byte(code)), ttl.Seconds(),
	).Scan(&expiresAt)
	if err != nil {
		return "", uuid.Nil, time.Time{}, err
	}
	return code, id, expiresAt, nil
}

// ExchangeResult is the outcome of a successful code redemption: the signed-in
// user plus the freshly minted session material.
type ExchangeResult struct {
	User        model.User
	SessionID   uuid.UUID
	CookieToken string
	CSRFToken   string
	ExpiresAt   time.Time
}

// ExchangeCode atomically redeems a one-time code for a new session. Code
// consumption, purpose enforcement (first-admin activation under the
// enrollment advisory lock when required), and session creation commit in one
// transaction, so concurrent redemptions of the same code can never both
// succeed and a failed session never burns the code.
func (s *Store) ExchangeCode(ctx context.Context, code string, sessionTTL time.Duration) (ExchangeResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ExchangeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Step 1 — burn one attempt for every presentation of a still-unused
	// code, which bounds replay probing of an expired or locked code. A row
	// that no longer matches is classified precisely (unknown / used /
	// expired / locked).
	tag, err := tx.Exec(ctx,
		`UPDATE auth_codes
		 SET attempts = attempts + 1
		 WHERE code_hash = $1
		   AND used_at IS NULL
		   AND attempts < $2`,
		sha256Sum([]byte(code)), MaxCodeAttempts,
	)
	if err != nil {
		return ExchangeResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return ExchangeResult{}, classifyCodeFailure(ctx, tx, code)
	}

	// Step 2 — claim: exactly one concurrent presentation converts the
	// attempt into the single use.
	var codeID, userID uuid.UUID
	var purpose string
	err = tx.QueryRow(ctx,
		`UPDATE auth_codes
		 SET used_at = now()
		 WHERE code_hash = $1
		   AND used_at IS NULL
		   AND expires_at > clock_timestamp()
		 RETURNING id, user_id, purpose`,
		sha256Sum([]byte(code)),
	).Scan(&codeID, &userID, &purpose)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return ExchangeResult{}, err
		}
		return ExchangeResult{}, classifyCodeFailure(ctx, tx, code)
	}

	var user model.User
	err = tx.QueryRow(ctx,
		`SELECT id, username, display_name, webauthn_user_handle, status, created_at, updated_at
		 FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&user.ID, &user.Username, &user.DisplayName, &user.WebAuthnUserHandle,
		&user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return ExchangeResult{}, err
	}

	switch purpose {
	case AuthCodePurposeInit:
		// First-administrator closure mirrors CompleteEnrollment: the
		// advisory lock makes "no active administrator" and the activation
		// one atomic step.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, EnrollmentLockKey); err != nil {
			return ExchangeResult{}, err
		}
		if user.Status != model.UserStatusPending {
			return ExchangeResult{}, ErrCodePurpose
		}
		var hasActive bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM users WHERE status = 'active' AND id <> $1)`, userID,
		).Scan(&hasActive); err != nil {
			return ExchangeResult{}, err
		}
		if hasActive {
			return ExchangeResult{}, ErrBootstrapClosed
		}
		tag, err := tx.Exec(ctx,
			`UPDATE users SET status = 'active', updated_at = now() WHERE id = $1 AND status = 'pending'`, userID)
		if err != nil {
			return ExchangeResult{}, err
		}
		if tag.RowsAffected() == 0 {
			return ExchangeResult{}, ErrConflict
		}
		user.Status = model.UserStatusActive
	case AuthCodePurposeLogin:
		if user.Status != model.UserStatusActive {
			return ExchangeResult{}, ErrCodePurpose
		}
	default:
		return ExchangeResult{}, ErrCodePurpose
	}

	// Session material is minted inside the same transaction so a failed
	// insert rolls the code consumption back with it.
	cookieToken := base32Code()
	csrf := base32Code()
	sessionID := newUUID()
	var expiresAt time.Time
	if err := tx.QueryRow(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, csrf_token, expires_at)
		 VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))
		 RETURNING expires_at`,
		sessionID, userID, sha256Sum([]byte(cookieToken)), csrf, sessionTTL.Seconds(),
	).Scan(&expiresAt); err != nil {
		return ExchangeResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ExchangeResult{}, err
	}
	return ExchangeResult{
		User:        user,
		SessionID:   sessionID,
		CookieToken: cookieToken,
		CSRFToken:   csrf,
		ExpiresAt:   expiresAt,
	}, nil
}

// PurgeExpired removes code rows that are expired and either used or older
// than a day, keeping the table small without losing recent audit state.
func (s *AuthCodeStore) PurgeExpired(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM auth_codes
		 WHERE expires_at < now() - interval '24 hours'
		    OR (used_at IS NOT NULL AND expires_at < clock_timestamp())`)
	return err
}

// classifyCodeFailure inspects the row (if any) behind a failed claim and maps
// it to the precise sentinel error.
func classifyCodeFailure(ctx context.Context, tx pgx.Tx, code string) error {
	var usedAt *time.Time
	var expiresAt time.Time
	var attempts int
	err := tx.QueryRow(ctx,
		`SELECT used_at, expires_at, attempts FROM auth_codes WHERE code_hash = $1`,
		sha256Sum([]byte(code)),
	).Scan(&usedAt, &expiresAt, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCodeInvalid
	}
	if err != nil {
		return err
	}
	switch {
	case usedAt != nil:
		return ErrCodeUsed
	case !expiresAt.After(time.Now()):
		return ErrCodeExpired
	case attempts >= MaxCodeAttempts:
		return ErrCodeLocked
	default:
		return ErrCodeInvalid
	}
}

func formatCode(encoded string) string {
	encoded = strings.TrimRight(encoded, "=")
	var parts []string
	for i := 0; i < len(encoded); i += 4 {
		parts = append(parts, encoded[i:i+4])
	}
	return strings.Join(parts, "-")
}

// base32Code mints fresh random material for session tokens, mirroring
// SessionStore.Create.
func base32Code() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(32))
}
