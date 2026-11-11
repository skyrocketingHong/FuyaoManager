// Package model defines the persistent domain records shared by the store,
// WebAuthn, and HTTP layers.
package model

import (
	"encoding/json"
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

// Theme is one registered (theme id, version) pair from the theme catalog.
// Manifest holds the validated theme.json document; ManifestDigest is the
// SHA-256 digest of the exact bytes read at registration, which distinguishes
// identical re-registration from a changed manifest under the same version.
// ConfigSchema is nil when the manifest declares no configSchema (only an
// empty object is a valid theme configuration then). SourcePath is the
// server-side trusted theme root for the build executor; it must never be
// exposed through the management API.
type Theme struct {
	ID             uuid.UUID
	ThemeID        string
	Version        string
	Adapter        string
	Manifest       json.RawMessage
	ManifestDigest []byte
	ConfigSchema   json.RawMessage
	SourcePath     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SiteThemeSelection is a site's selected theme registration and its theme
// configuration. Revision drives optimistic concurrency via strong ETags,
// independently of SiteSettings.Revision.
type SiteThemeSelection struct {
	SiteID      uuid.UUID
	ThemeID     uuid.UUID
	ThemeConfig json.RawMessage
	Revision    int64
	SelectedBy  uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ThemeManifestView is the typed projection of a validated theme.json
// document. It mirrors the fields of the manifest contract
// (contracts/theme/manifest.schema.json) that the management API exposes.
type ThemeManifestView struct {
	Contract      string   `json:"contract"`
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Adapter       string   `json:"adapter"`
	InputProtocol string   `json:"inputProtocol"`
	Capabilities  []string `json:"capabilities"`
	ConfigSchema  *struct {
		File   string `json:"file"`
		Format string `json:"format"`
	} `json:"configSchema"`
	Outputs struct {
		SpaFallback       *string  `json:"spaFallback"`
		URLConventions    []string `json:"urlConventions"`
		RequiredArtifacts []string `json:"requiredArtifacts"`
	} `json:"outputs"`
}

// ParseThemeManifest decodes a stored manifest document into its typed view.
// The manifest was validated against the contract at registration time, so
// decoding failures indicate storage corruption and are surfaced as errors.
func ParseThemeManifest(data []byte) (ThemeManifestView, error) {
	var view ThemeManifestView
	if err := json.Unmarshal(data, &view); err != nil {
		return ThemeManifestView{}, err
	}
	return view, nil
}
