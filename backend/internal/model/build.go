package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Build status values persisted in site_builds.status.
const (
	BuildStatusQueued    = "queued"
	BuildStatusRunning   = "running"
	BuildStatusSucceeded = "succeeded"
	BuildStatusFailed    = "failed"
)

// BuildFailure is the persisted failure record of a build task: the stage it
// died in, a client-safe message, and the structured detail available for
// capability/URL-convention rejections.
type BuildFailure struct {
	Stage                 string   `json:"stage"`
	Message               string   `json:"message"`
	MissingCapabilities   []string `json:"missingCapabilities,omitempty"`
	MissingURLConventions []string `json:"missingUrlConventions,omitempty"`
	ExitCode              *int     `json:"exitCode,omitempty"`
}

// Build is one persisted build-preview task. The identity block
// (ThemeRowID..ThemeConfig, SiteSettings, and both revisions) is frozen at
// creation time and is the only input the executor may consult.
type Build struct {
	ID                     uuid.UUID
	SiteID                 uuid.UUID
	RequestedBy            uuid.UUID
	ThemeRowID             uuid.UUID
	ThemeID                string
	ThemeVersion           string
	ManifestDigest         []byte
	ThemeConfig            json.RawMessage
	SiteSettings           json.RawMessage
	SiteSettingsRevision   int64
	ThemeSelectionRevision int64
	Status                 string
	SourceDigest           string
	SourceFileCount        int64
	Failure                *BuildFailure
	ArtifactPath           string
	InputDigests           json.RawMessage
	CreatedAt              time.Time
	StartedAt              *time.Time
	FinishedAt             *time.Time
}

// SiteSettingsSnapshot is the subset of site settings injected into the build
// snapshot's site configuration. The JSON shape matches the platform overlay
// file the theme's preparation script consumes: {"site":{...}} wrapping the
// individual settings.
type SiteSettingsSnapshot struct {
	Site SiteSettingsOverlay `json:"site"`
}

// SiteSettingsOverlay lists the overridable public site settings.
type SiteSettingsOverlay struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	DefaultLocale string `json:"defaultLocale"`
}
