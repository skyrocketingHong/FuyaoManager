// Package caddymanage applies only root-registered, finite settings to an
// immutable single-site Caddyfile template. It never accepts Caddy source,
// filesystem paths, upstreams, commands or arbitrary headers from HTTP.
package caddymanage

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/google/uuid"
)

var (
	ErrNotRegistered  = errors.New("Caddy site has no operator registration")
	ErrConflict       = errors.New("Caddy configuration or revision changed")
	ErrValidation     = errors.New("Caddy configuration validation failed")
	ErrPermission     = errors.New("Caddy capability trust boundary failed")
	ErrUnsupported    = errors.New("Caddy setting or site is unsupported")
	ErrUnavailable    = errors.New("Caddy management capability unavailable")
	ErrInvalidRequest = errors.New("invalid Caddy management request")
)

const RegistryRoot = "/etc/fuyao-caddy-control/sites"
const StateRoot = "/var/lib/fuyao-caddy-control"
const SourceMarker = "__FUYAO_SITE_SOURCE__"
const maxRequestBytes = 65536
const maxFileBytes = 2 << 20

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Settings struct {
	Compression     string `json:"compression,omitempty"`
	CacheControl    string `json:"cacheControl,omitempty"`
	SecurityHeaders string `json:"securityHeaders,omitempty"`
}
type Capabilities struct {
	Compression     bool `json:"compression"`
	CacheControl    bool `json:"cacheControl"`
	SecurityHeaders bool `json:"securityHeaders"`
}
type Request struct {
	Operation           string   `json:"operation"`
	SiteKey             string   `json:"siteKey"`
	ExpectedDigest      string   `json:"expectedDigest,omitempty"`
	Settings            Settings `json:"settings,omitempty"`
	ValidationID        string   `json:"validationId,omitempty"`
	RevisionID          string   `json:"revisionId,omitempty"`
	SiteID              string   `json:"siteId,omitempty"`
	ReleaseID           string   `json:"releaseId,omitempty"`
	ManifestDigest      string   `json:"manifestDigest,omitempty"`
	MediaManifestDigest string   `json:"mediaManifestDigest,omitempty"`
}
type Result struct {
	Registered         bool         `json:"registered"`
	Adopted            bool         `json:"adopted"`
	SourceDigest       string       `json:"sourceDigest"`
	CandidateDigest    string       `json:"candidateDigest,omitempty"`
	ValidationID       string       `json:"validationId,omitempty"`
	RootRevisionID     string       `json:"rootRevisionId,omitempty"`
	Settings           Settings     `json:"settings"`
	Capabilities       Capabilities `json:"capabilities"`
	Drifted            bool         `json:"drifted,omitempty"`
	RuntimeUncertain   bool         `json:"runtimeUncertain,omitempty"`
	PublicationEnabled bool         `json:"publicationEnabled,omitempty"`
	PublishedReleaseID string       `json:"publishedReleaseId,omitempty"`
}
type Preserve struct {
	Compression     string `json:"compression"`
	CacheControl    string `json:"cacheControl"`
	SecurityHeaders string `json:"securityHeaders"`
}
type Registration struct {
	SiteKey        string       `json:"siteKey"`
	Kind           string       `json:"kind"`
	StaticRoot     string       `json:"staticRoot"`
	MasterFile     string       `json:"masterFile"`
	SourceFile     string       `json:"sourceFile"`
	BaseTemplate   string       `json:"baseTemplate"`
	MasterTemplate string       `json:"masterTemplate"`
	InitialDigest  string       `json:"initialDigest"`
	Capabilities   Capabilities `json:"capabilities"`
	Preserve       Preserve     `json:"preserve"`
	Publication    *Publication `json:"publication,omitempty"`
}

// Publication is installed only by the root operator. All source paths and
// template slots remain outside the unprivileged request contract.
type Publication struct {
	SiteID        string        `json:"siteId"`
	ReleasesRoot  string        `json:"releasesRoot"`
	BuildsRoot    string        `json:"buildsRoot"`
	SourceUID     uint32        `json:"sourceUid"`
	AllowedThemes []string      `json:"allowedThemes"`
	Paths         []PublishPath `json:"paths"`
}

type PublishPath struct {
	Marker        string `json:"marker"`
	Preserve      string `json:"preserve"`
	MediaRelative string `json:"mediaRelative,omitempty"`
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func NormalizeSettings(value Settings) (Settings, error) {
	if value.Compression == "" {
		value.Compression = "preserve"
	}
	if value.CacheControl == "" {
		value.CacheControl = "preserve"
	}
	if value.SecurityHeaders == "" {
		value.SecurityHeaders = "preserve"
	}
	if !oneOf(value.Compression, "preserve", "none", "gzip", "zstd", "gzip_zstd") || !oneOf(value.CacheControl, "preserve", "no_store") || !oneOf(value.SecurityHeaders, "preserve", "baseline") {
		return Settings{}, ErrUnsupported
	}
	return value, nil
}
func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func DecodeRequest(reader io.Reader) (Request, error) {
	var value Request
	data, err := io.ReadAll(io.LimitReader(reader, maxRequestBytes+1))
	if err != nil || len(data) > maxRequestBytes {
		return value, ErrInvalidRequest
	}
	if err := strictJSON(data, &value); err != nil {
		return value, ErrInvalidRequest
	}
	if !keyPattern.MatchString(value.SiteKey) || !oneOf(value.Operation, "inspect", "adopt", "validate", "deploy", "rollback", "restore", "release") {
		return value, ErrInvalidRequest
	}
	if value.Operation != "inspect" && !digestPattern.MatchString(value.ExpectedDigest) {
		return value, ErrInvalidRequest
	}
	if value.Operation == "deploy" && !revisionPattern.MatchString(value.ValidationID) {
		return value, ErrInvalidRequest
	}
	if oneOf(value.Operation, "rollback", "restore") && !revisionPattern.MatchString(value.RevisionID) {
		return value, ErrInvalidRequest
	}
	if value.Operation == "release" {
		if !canonicalUUID(value.SiteID) || !canonicalUUID(value.ReleaseID) || !digestPattern.MatchString(value.ManifestDigest) || !digestPattern.MatchString(value.MediaManifestDigest) || value.ValidationID != "" || value.RevisionID != "" {
			return value, ErrInvalidRequest
		}
	} else if value.SiteID != "" || value.ReleaseID != "" || value.ManifestDigest != "" || value.MediaManifestDigest != "" {
		return value, ErrInvalidRequest
	}
	if _, err := NormalizeSettings(value.Settings); err != nil {
		return value, err
	}
	return value, nil
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytesReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ErrInvalidRequest
	}
	return nil
}
