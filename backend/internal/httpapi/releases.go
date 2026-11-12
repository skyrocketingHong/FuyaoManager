package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// Publisher executes publishes and rollbacks; publish.Manager implements it.
type Publisher interface {
	Publish(ctx context.Context, siteID, buildID, userID uuid.UUID, reasons []string) (model.Release, error)
	Rollback(ctx context.Context, siteID, releaseID, userID uuid.UUID) (model.Release, error)
}

// PublishedOrigins resolves per-site published origin URLs; publish.Server
// implements it.
type PublishedOrigins interface {
	OriginURL(ctx context.Context, siteID uuid.UUID) (string, error)
}

// releaseFailureDTO mirrors the persisted publish failure record.
type releaseFailureDTO struct {
	Stage                 string   `json:"stage"`
	Message               string   `json:"message"`
	MissingCapabilities   []string `json:"missingCapabilities,omitempty"`
	MissingURLConventions []string `json:"missingUrlConventions,omitempty"`
	MissingArtifacts      []string `json:"missingArtifacts,omitempty"`
}

// releaseDTO is the public representation of a release record. Filesystem
// locations are never exposed; the published origin URL is returned
// separately on the current-release read.
type releaseDTO struct {
	ID           uuid.UUID           `json:"id"`
	SiteID       uuid.UUID           `json:"siteId"`
	BuildID      uuid.UUID           `json:"buildId"`
	Theme        selectedThemeDTO    `json:"theme"`
	InputDigests json.RawMessage     `json:"inputDigests"`
	Routes       model.ReleaseRoutes `json:"routes"`
	Reasons      []string            `json:"reasons"`
	Status       string              `json:"status"`
	Failure      *releaseFailureDTO  `json:"failure"`
	CreatedBy    uuid.UUID           `json:"createdBy"`
	CreatedAt    string              `json:"createdAt"`
	ActivatedAt  *string             `json:"activatedAt"`
	SupersededAt *string             `json:"supersededAt"`
}

func toReleaseDTO(r model.Release) releaseDTO {
	dto := releaseDTO{
		ID:      r.ID,
		SiteID:  r.SiteID,
		BuildID: r.BuildID,
		Theme: selectedThemeDTO{
			ThemeID:        r.ThemeID,
			Version:        r.ThemeVersion,
			ManifestDigest: hex.EncodeToString(r.ManifestDigest),
		},
		InputDigests: r.InputDigests,
		Routes:       r.Routes,
		Reasons:      r.Reasons,
		Status:       r.Status,
		CreatedBy:    r.CreatedBy,
		CreatedAt:    r.CreatedAt.UTC().Format(timeFormat),
	}
	if dto.Reasons == nil {
		dto.Reasons = []string{}
	}
	if len(dto.InputDigests) == 0 {
		dto.InputDigests = json.RawMessage("null")
	}
	if r.Failure != nil {
		dto.Failure = &releaseFailureDTO{
			Stage:                 r.Failure.Stage,
			Message:               r.Failure.Message,
			MissingCapabilities:   r.Failure.MissingCapabilities,
			MissingURLConventions: r.Failure.MissingURLConventions,
			MissingArtifacts:      r.Failure.MissingArtifacts,
		}
	}
	if r.ActivatedAt != nil {
		s := r.ActivatedAt.UTC().Format(timeFormat)
		dto.ActivatedAt = &s
	}
	if r.SupersededAt != nil {
		s := r.SupersededAt.UTC().Format(timeFormat)
		dto.SupersededAt = &s
	}
	return dto
}

// handleCreateRelease publishes a succeeded build. Owners and editors only.
// A failed publish returns the release record with its failure and leaves the
// previous release live.
func (a *API) handleCreateRelease(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
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
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may publish releases.")
		return
	}
	if a.publisher == nil {
		writeProblem(w, http.StatusServiceUnavailable, "publish_unavailable", "The publish pipeline is not configured on this server.")
		return
	}

	var body struct {
		BuildID *uuid.UUID `json:"buildId"`
		Reasons []string   `json:"reasons"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if body.BuildID == nil {
		writeProblem(w, http.StatusBadRequest, "validation_error", "The buildId field is required.")
		return
	}

	release, err := a.publisher.Publish(r.Context(), siteID, *body.BuildID, auth.User.ID, body.Reasons)
	switch {
	case errors.Is(err, publish.ErrNoContracts):
		writeProblem(w, http.StatusServiceUnavailable, "publish_unavailable", "The theme contracts are not configured; publishing is unavailable.")
		return
	case errors.Is(err, publish.ErrBuildNotPublishable):
		writeProblem(w, http.StatusUnprocessableEntity, "build_not_publishable", "Only a succeeded build of this site can be published.")
		return
	case errors.Is(err, publish.ErrBuildAlreadyReleased):
		writeProblem(w, http.StatusConflict, "release_build_conflict", "This build already backs a release; roll back to it instead of republishing.")
		return
	case err != nil:
		// Publish failures carry a client-safe record persisted for
		// history; surface it as that release row. A failure that could not
		// be persisted returns no row and answers with a plain problem.
		if release.ID != uuid.Nil && release.Failure != nil {
			w.Header().Set("Location", "/api/v1/sites/"+siteID.String()+"/releases/"+release.ID.String())
			writeJSON(w, http.StatusUnprocessableEntity, toReleaseDTO(release))
			return
		}
		a.logger.Error("publish release", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to publish the release.")
		return
	}
	w.Header().Set("Location", "/api/v1/sites/"+siteID.String()+"/releases/"+release.ID.String())
	writeJSON(w, http.StatusCreated, toReleaseDTO(release))
}

// handleGetRelease returns one release record (live, superseded, or failed
// attempt) by id; any site member may read.
func (a *API) handleGetRelease(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	releaseID, ok := parseUUIDPath(w, r, "releaseId")
	if !ok {
		return
	}
	release, err := a.store.Releases.GetForUser(r.Context(), releaseID, siteID, auth.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "release_not_found", "The release does not exist on this site.")
		return
	}
	if err != nil {
		a.logger.Error("get release", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to read the release.")
		return
	}
	writeJSON(w, http.StatusOK, toReleaseDTO(release))
}

// handleListReleases returns the caller's site releases, newest first.
func (a *API) handleListReleases(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Themes.GetSelectionForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	releases, err := a.store.Releases.ListForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		a.logger.Error("list releases", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to list releases.")
		return
	}
	out := make([]releaseDTO, 0, len(releases))
	for _, rel := range releases {
		out = append(out, toReleaseDTO(rel))
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out})
}

// handleGetCurrentRelease returns the site's live release plus its published
// origin URL. Any site member may read.
func (a *API) handleGetCurrentRelease(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Themes.GetSelectionForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	release, err := a.store.Releases.GetCurrentForUser(r.Context(), siteID, auth.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "no_live_release", "The site has no live release yet.")
		return
	}
	if err != nil {
		a.logger.Error("get current release", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to read the current release.")
		return
	}
	originURL := ""
	if a.store.NetworkEntries != nil && a.edge != nil {
		// The managed edge entry (operator-approved IP:port) wins when the
		// site has one; the loopback published origin remains the local
		// development fallback.
		if site, siteErr := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); siteErr == nil {
			if entry, entryErr := a.store.NetworkEntries.GetSiteEntry(r.Context(), siteID); entryErr == nil {
				originURL = a.edge.EntryURL(site.Site.Visibility, entry.Port)
			}
		}
	}
	if originURL == "" && a.publishedOrigins != nil {
		if url, err := a.publishedOrigins.OriginURL(r.Context(), siteID); err == nil {
			originURL = url
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"release": toReleaseDTO(release), "originUrl": originURL})
}

// handleRollbackRelease re-points the site at an existing release. Owners and
// editors only; artifacts are reused, nothing is rebuilt.
func (a *API) handleRollbackRelease(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	releaseID, ok := parseUUIDPath(w, r, "releaseId")
	if !ok {
		return
	}
	rec, err := a.store.Themes.GetSelectionForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	if rec.Role != model.RoleOwner && rec.Role != model.RoleEditor {
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may roll back releases.")
		return
	}
	if a.publisher == nil {
		writeProblem(w, http.StatusServiceUnavailable, "publish_unavailable", "The publish pipeline is not configured on this server.")
		return
	}

	release, err := a.publisher.Rollback(r.Context(), siteID, releaseID, auth.User.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "release_not_found", "The release does not exist on this site.")
		return
	case errors.Is(err, store.ErrReleaseNotRollbackable):
		writeProblem(w, http.StatusConflict, "release_not_rollbackable", "The release cannot become live (already live, failed, or artifacts missing).")
		return
	case err != nil:
		a.logger.Error("rollback release", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to roll back the release.")
		return
	}
	writeJSON(w, http.StatusOK, toReleaseDTO(release))
}
