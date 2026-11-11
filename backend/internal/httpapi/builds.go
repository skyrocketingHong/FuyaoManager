package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// BuildEnqueuer hands a freshly created build to the executor. buildrunner.
// Runner implements it; the interface keeps httpapi independent of the
// runner's wiring.
type BuildEnqueuer interface {
	Enqueue(buildID uuid.UUID)
}

// PreviewIssuer mints private preview links. buildrunner.PreviewServer
// implements it; Issue returns the ready-to-open URL on the build's private
// preview origin.
type PreviewIssuer interface {
	Issue(siteID, buildID uuid.UUID) (url string, expiresAt time.Time, err error)
}

var errRunnerNotConfigured = errors.New("build runner not configured")

var _ = errRunnerNotConfigured

// buildFailureDTO mirrors the persisted failure record.
type buildFailureDTO struct {
	Stage                 string   `json:"stage"`
	Message               string   `json:"message"`
	MissingCapabilities   []string `json:"missingCapabilities,omitempty"`
	MissingURLConventions []string `json:"missingUrlConventions,omitempty"`
	ExitCode              *int     `json:"exitCode,omitempty"`
}

// buildDTO is the public representation of a build task. It never exposes
// filesystem paths: the artifact location stays server-side.
type buildDTO struct {
	ID                     string           `json:"id"`
	SiteID                 string           `json:"siteId"`
	Status                 string           `json:"status"`
	RequestedBy            string           `json:"requestedBy"`
	Theme                  selectedThemeDTO `json:"theme"`
	ThemeConfig            json.RawMessage  `json:"themeConfig"`
	SiteSettings           json.RawMessage  `json:"siteSettings"`
	SiteSettingsRevision   int64            `json:"siteSettingsRevision"`
	ThemeSelectionRevision int64            `json:"themeSelectionRevision"`
	Failure                *buildFailureDTO `json:"failure"`
	InputDigests           json.RawMessage  `json:"inputDigests"`
	CreatedAt              string           `json:"createdAt"`
	StartedAt              *string          `json:"startedAt"`
	FinishedAt             *string          `json:"finishedAt"`
}

func toBuildDTO(b model.Build) buildDTO {
	dto := buildDTO{
		ID:          b.ID.String(),
		SiteID:      b.SiteID.String(),
		Status:      b.Status,
		RequestedBy: b.RequestedBy.String(),
		Theme: selectedThemeDTO{
			ThemeID:        b.ThemeID,
			Version:        b.ThemeVersion,
			ManifestDigest: hex.EncodeToString(b.ManifestDigest),
		},
		ThemeConfig:            b.ThemeConfig,
		SiteSettings:           b.SiteSettings,
		SiteSettingsRevision:   b.SiteSettingsRevision,
		ThemeSelectionRevision: b.ThemeSelectionRevision,
		CreatedAt:              b.CreatedAt.UTC().Format(timeFormat),
	}
	if b.Failure != nil {
		dto.Failure = &buildFailureDTO{
			Stage:                 b.Failure.Stage,
			Message:               b.Failure.Message,
			MissingCapabilities:   b.Failure.MissingCapabilities,
			MissingURLConventions: b.Failure.MissingURLConventions,
			ExitCode:              b.Failure.ExitCode,
		}
	}
	if len(b.InputDigests) > 0 {
		dto.InputDigests = b.InputDigests
	} else {
		dto.InputDigests = json.RawMessage("null")
	}
	if b.StartedAt != nil {
		s := b.StartedAt.UTC().Format(timeFormat)
		dto.StartedAt = &s
	}
	if b.FinishedAt != nil {
		s := b.FinishedAt.UTC().Format(timeFormat)
		dto.FinishedAt = &s
	}
	return dto
}

// handleCreateBuild enqueues a build-preview task. The creation transaction
// freezes the current settings and theme-selection version view into the task
// row; later edits never leak into a running build. Owners and editors only.
func (a *API) handleCreateBuild(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rec, err := a.store.Themes.GetSelectionForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	if rec.Role != model.RoleOwner && rec.Role != model.RoleEditor {
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may trigger builds.")
		return
	}
	if a.buildEnqueuer == nil {
		writeProblem(w, http.StatusServiceUnavailable, "build_runner_unavailable", "The build executor is not configured on this server.")
		return
	}

	build, err := a.store.Builds.CreateFromSiteSnapshot(r.Context(), siteID, auth.User.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "site_not_found", "The site does not exist.")
		return
	case errors.Is(err, store.ErrNoThemeSelection):
		writeProblem(w, http.StatusUnprocessableEntity, "no_theme_selected", "The site has no theme selection yet; select a theme first.")
		return
	case err != nil:
		a.logger.Error("create build", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to create the build task.")
		return
	}
	a.buildEnqueuer.Enqueue(build.ID)
	w.Header().Set("Location", "/api/v1/sites/"+siteID.String()+"/builds/"+build.ID.String())
	writeJSON(w, http.StatusAccepted, toBuildDTO(build))
}

// handleListBuilds returns the caller's site builds, newest first.
func (a *API) handleListBuilds(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Themes.GetSelectionForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	builds, err := a.store.Builds.ListForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		a.logger.Error("list builds", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to list builds.")
		return
	}
	out := make([]buildDTO, 0, len(builds))
	for _, b := range builds {
		out = append(out, toBuildDTO(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"builds": out})
}

// handleGetBuild returns one task's persisted state for re-query.
func (a *API) handleGetBuild(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	buildID, ok := parseUUIDPath(w, r, "buildId")
	if !ok {
		return
	}
	build, err := a.store.Builds.GetForUser(r.Context(), buildID, siteID, auth.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "build_not_found", "The build task does not exist on this site.")
		return
	}
	if err != nil {
		a.logger.Error("get build", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to read the build task.")
		return
	}
	writeJSON(w, http.StatusOK, toBuildDTO(build))
}

// handleCreatePreviewLink mints a short-lived preview URL for a succeeded
// build. Any site member may preview; the token is unguessable, expires, and
// lives on the preview origin, separate from the management session.
func (a *API) handleCreatePreviewLink(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	buildID, ok := parseUUIDPath(w, r, "buildId")
	if !ok {
		return
	}
	if a.previews == nil {
		writeProblem(w, http.StatusServiceUnavailable, "preview_unavailable", "The preview server is not configured on this server.")
		return
	}
	build, err := a.store.Builds.GetForUser(r.Context(), buildID, siteID, auth.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "build_not_found", "The build task does not exist on this site.")
		return
	}
	if err != nil {
		a.logger.Error("get build for preview", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to issue the preview link.")
		return
	}
	if build.Status != model.BuildStatusSucceeded {
		writeProblem(w, http.StatusConflict, "build_not_succeeded", "Preview links exist only for succeeded builds.")
		return
	}
	url, expiresAt, err := a.previews.Issue(siteID, buildID)
	if err != nil {
		writeProblem(w, http.StatusConflict, "build_not_succeeded", "Preview artifacts are not available for this build.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":       url,
		"expiresAt": expiresAt.UTC().Format(time.RFC3339),
	})
}

// parseUUIDPath extracts a request-path UUID value or writes a 400.
func parseUUIDPath(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	value := r.PathValue(name)
	id, err := uuid.Parse(value)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_id", "The "+name+" path segment is not a valid identifier.")
		return uuid.Nil, false
	}
	return id, true
}

var _ = time.Now
