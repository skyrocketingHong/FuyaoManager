package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Release status values persisted in site_releases.status. A publish attempt
// starts implicit and lands on live (switched the pointer) or failed (the
// previous release stays live); live turns superseded when a newer publish or
// a rollback replaces it.
const (
	ReleaseStatusLive       = "live"
	ReleaseStatusSuperseded = "superseded"
	ReleaseStatusFailed     = "failed"
)

// PublishFailure is the persisted failure record of a publish attempt:
// client-safe stage and message plus the structured detail for
// capability/URL-convention/artifact rejections.
type PublishFailure struct {
	Stage                 string   `json:"stage"`
	Message               string   `json:"message"`
	MissingCapabilities   []string `json:"missingCapabilities,omitempty"`
	MissingURLConventions []string `json:"missingUrlConventions,omitempty"`
	MissingArtifacts      []string `json:"missingArtifacts,omitempty"`
}

// ReleaseRoutes is the serving metadata frozen into the release record and
// mirrored into release.json; it comes from the theme manifest at publish
// time.
type ReleaseRoutes struct {
	SpaFallback    *string  `json:"spaFallback"`
	URLConventions []string `json:"urlConventions"`
}

// Release is one persisted publish record. The theme identity and input
// digests identify the exact triple (content, site config, theme config) the
// release was built from; rollback restores that triple without rebuilding.
type Release struct {
	ID             uuid.UUID
	SiteID         uuid.UUID
	BuildID        uuid.UUID
	ReleaseDir     string
	ThemeID        string
	ThemeVersion   string
	ManifestDigest []byte
	InputDigests   json.RawMessage
	Routes         ReleaseRoutes
	Reasons        []string
	Status         string
	Failure        *PublishFailure
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	ActivatedAt    *time.Time
	SupersededAt   *time.Time
}

// ReleaseInputDigests is the typed view of the three input digests carried on
// build rows; release.json requires exactly these keys.
type ReleaseInputDigests struct {
	ContentDigest     string `json:"contentDigest"`
	SiteConfigDigest  string `json:"siteConfigDigest"`
	ThemeConfigDigest string `json:"themeConfigDigest"`
}
