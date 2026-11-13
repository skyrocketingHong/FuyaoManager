// Package model defines the persistent domain records shared by the store,
// WebAuthn, and HTTP layers.
package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// User statuses. A user starts as pending during bootstrap and becomes active
// once a passkey has been registered. disabled deactivates an account
// (sessions and one-time codes are revoked; login is refused); deleted is the
// soft delete — the row and its history stay queryable, the account can never
// log in again.
const (
	UserStatusPending  = "pending"
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
	UserStatusDeleted  = "deleted"
)

// Platform roles. Every pre-migration account is an admin (bootstrap
// administrator); accounts created afterwards default to member, the minimal
// privilege level.
const (
	PlatformRoleAdmin  = "admin"
	PlatformRoleMember = "member"
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

// User is an account of the management platform. PlatformRole distinguishes
// the bootstrap administrators (admin) from accounts created afterwards
// (member, the minimal privilege); DeletedAt records a soft delete.
type User struct {
	ID                 uuid.UUID
	Username           string
	DisplayName        string
	WebAuthnUserHandle []byte
	Status             string
	PlatformRole       string
	DeletedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Session kinds. A full session may use the business APIs; a restricted
// session only proves the account identity long enough to bind a passkey.
const (
	SessionKindFull       = "full"
	SessionKindRestricted = "restricted"
)

// Passkey is a stored WebAuthn credential. RPID records the relying-party ID
// the credential was registered under; a credential only counts as bound for
// the current deployment when it matches the configured RP ID.
type Passkey struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	RPID              string
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
	Kind      string
	TokenHash []byte
	CSRFToken string
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Site visibility values. A private site is only reachable from the server
// host and the private network; a public site gets a public entry.
const (
	SiteVisibilityPublic  = "public"
	SiteVisibilityPrivate = "private"
)

// Site provisioning states. A site is only "ready" once the platform
// provisioned its on-disk content/config tree; "provisioning"/"failed" keep
// the row queryable while directory setup is pending or broken.
const (
	SiteProvisioning    = "provisioning"
	SiteReady           = "ready"
	SiteProvisionFailed = "failed"
)

// Site is a managed site entry.
type Site struct {
	ID               uuid.UUID
	Slug             string
	Visibility       string
	ProvisionState   string
	ProvisionFailure json.RawMessage
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// MediaTaskFailure records why an asynchronous media task broke.
type MediaTaskFailure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// ProvisionFailure records why a site's directory provisioning broke, with the
// exact stage that failed so the UI can offer an informed retry.
type ProvisionFailure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// Value and Scan let ProvisionFailure round-trip through the jsonb column.
func (f ProvisionFailure) Value() (driver.Value, error) {
	if f.Stage == "" {
		return nil, nil
	}
	return json.Marshal(f)
}

func (f *ProvisionFailure) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*f = ProvisionFailure{}
		return nil
	case []byte:
		return json.Unmarshal(v, f)
	case string:
		return json.Unmarshal([]byte(v), f)
	default:
		return fmt.Errorf("ProvisionFailure: unsupported scan source %T", src)
	}
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
