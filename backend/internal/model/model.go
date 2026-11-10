// Package model defines the persistent domain records shared by the store,
// WebAuthn, and HTTP layers.
package model

import (
	"time"

	"github.com/google/uuid"
)

// User statuses. A user starts as pending during bootstrap and becomes active
// once a passkey has been registered.
const (
	UserStatusPending = "pending"
	UserStatusActive  = "active"
)

// Site membership roles. Viewers have read-only access; editors and owners may
// change site settings.
const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// Supported site locales.
var SupportedLocales = []string{"zh-CN", "en-US"}

// User is an administrator account.
type User struct {
	ID                 uuid.UUID
	Username           string
	DisplayName        string
	WebAuthnUserHandle []byte
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Passkey is a stored WebAuthn credential.
type Passkey struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	CredentialID      []byte
	PublicKey         []byte
	AttestationType   string
	AttestationFormat string
	Transports        []string
	SignCount         uint32
	BackupEligible    bool
	BackupState       bool
	UserVerified      bool
	AAGUID            []byte
	LastUsedAt        *time.Time
	CreatedAt         time.Time
}

// Session is a server-side authenticated session. TokenHash holds the
// SHA-256 digest of the cookie value; CSRFToken is the stable per-session
// token shared by every reader (tabs, reloads) of the session.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash []byte
	CSRFToken string
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Site is a managed site entry.
type Site struct {
	ID        uuid.UUID
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SiteSettings holds the editable settings of a site. Revision drives
// optimistic concurrency via strong ETags.
type SiteSettings struct {
	SiteID        uuid.UUID
	Name          string
	Description   string
	DefaultLocale string
	Revision      int64
	UpdatedAt     time.Time
}

// Membership links a user to a site with a role.
type Membership struct {
	SiteID    uuid.UUID
	UserID    uuid.UUID
	Role      string
	CreatedAt time.Time
}
