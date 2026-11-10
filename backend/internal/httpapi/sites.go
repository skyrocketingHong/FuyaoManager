package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// siteDTO is the public representation of a site with the caller's role.
type siteDTO struct {
	SiteID           string `json:"siteId"`
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	DefaultLocale    string `json:"defaultLocale"`
	Role             string `json:"role"`
	SettingsRevision int64  `json:"settingsRevision"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

// settingsDTO is the editable settings representation.
type settingsDTO struct {
	SiteID        string `json:"siteId"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	DefaultLocale string `json:"defaultLocale"`
	Revision      int64  `json:"revision"`
	UpdatedAt     string `json:"updatedAt"`
}

// settingsPatch is the strict PATCH body: every field is optional but at
// least one must be present, and unknown fields are rejected by decodeJSON.
type settingsPatch struct {
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	DefaultLocale *string `json:"defaultLocale"`
}

func toSiteDTO(rec store.SiteRecord) siteDTO {
	return siteDTO{
		SiteID:           rec.Site.ID.String(),
		Slug:             rec.Site.Slug,
		Name:             rec.Settings.Name,
		Description:      rec.Settings.Description,
		DefaultLocale:    rec.Settings.DefaultLocale,
		Role:             rec.Role,
		SettingsRevision: rec.Settings.Revision,
		CreatedAt:        rec.Site.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:        rec.Settings.UpdatedAt.UTC().Format(timeFormat),
	}
}

func toSettingsDTO(s model.SiteSettings) settingsDTO {
	return settingsDTO{
		SiteID:        s.SiteID.String(),
		Name:          s.Name,
		Description:   s.Description,
		DefaultLocale: s.DefaultLocale,
		Revision:      s.Revision,
		UpdatedAt:     s.UpdatedAt.UTC().Format(timeFormat),
	}
}

const timeFormat = "2006-01-02T15:04:05Z"

// settingsETag renders the strong entity tag for a settings revision.
func settingsETag(siteID uuid.UUID, revision int64) string {
	return fmt.Sprintf(`"%s-r%d"`, siteID, revision)
}

// parseIfMatch parses an If-Match header that must be the complete strong
// ETag of the target settings resource ("<siteId>-r<revision>"). The wildcard
// form "*" is rejected: this MVP requires an exact ETag so updates cannot
// bypass optimistic locking. It returns the site UUID embedded in the tag and
// the revision; a tag naming another site (or a forged prefix) fails the
// caller's site comparison and yields 412.
func parseIfMatch(header string) (siteID uuid.UUID, revision int64, err error) {
	value := strings.TrimSpace(header)
	if value == "*" {
		return uuid.Nil, 0, fmt.Errorf("If-Match must be an exact ETag, not *")
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return uuid.Nil, 0, fmt.Errorf("If-Match must be the quoted ETag returned by GET settings")
	}
	inner := value[1 : len(value)-1]
	idx := strings.LastIndex(inner, "-r")
	if idx < 0 {
		return uuid.Nil, 0, fmt.Errorf("If-Match does not match the ETag format")
	}
	id, idErr := uuid.Parse(inner[:idx])
	rev, revErr := strconv.ParseInt(inner[idx+2:], 10, 64)
	if idErr != nil || revErr != nil || rev <= 0 {
		return uuid.Nil, 0, fmt.Errorf("If-Match does not match the ETag format")
	}
	// Re-encode and compare so forged variants ("<uuid>-r1x", whitespace,
	// non-canonical UUIDs) cannot pass.
	if inner != fmt.Sprintf("%s-r%d", id, rev) {
		return uuid.Nil, 0, fmt.Errorf("If-Match does not match the ETag format")
	}
	return id, rev, nil
}

func writeSiteLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "not_found", "Site not found.")
		return
	}
	writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to read the site.")
}

func parseSiteID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("siteId"))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_site_id", "The siteId path parameter must be a UUID.")
		return uuid.Nil, false
	}
	return id, true
}

// handleListSites returns only the sites the caller belongs to.
func (a *API) handleListSites(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	records, err := a.store.Sites.ListForUser(r.Context(), auth.User.ID)
	if err != nil {
		a.logger.Error("list sites", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to list sites.")
		return
	}
	sites := make([]siteDTO, 0, len(records))
	for _, rec := range records {
		sites = append(sites, toSiteDTO(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites})
}

// handleGetSite returns one member site.
func (a *API) handleGetSite(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rec, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSiteDTO(rec))
}

// handleGetSettings returns the settings plus the strong ETag.
func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rec, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	w.Header().Set("ETag", settingsETag(siteID, rec.Settings.Revision))
	writeJSON(w, http.StatusOK, toSettingsDTO(rec.Settings))
}

// handlePatchSettings updates editable settings under optimistic concurrency.
// If-Match is mandatory: missing yields 428, a stale revision yields 412 with
// the current ETag in the response header.
func (a *API) handlePatchSettings(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rec, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	if rec.Role != model.RoleOwner && rec.Role != model.RoleEditor {
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may change site settings.")
		return
	}

	ifMatch := r.Header.Get("If-Match")
	if strings.TrimSpace(ifMatch) == "" {
		writeProblem(w, http.StatusPreconditionRequired, "precondition_required", "PATCH requires an If-Match header carrying the current ETag.")
		return
	}
	etagSiteID, expectedRev, err := parseIfMatch(ifMatch)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_if_match", "If-Match must be the exact ETag returned by GET settings; the wildcard form is not accepted.")
		return
	}
	if etagSiteID != siteID {
		w.Header().Set("ETag", settingsETag(siteID, rec.Settings.Revision))
		writeProblem(w, http.StatusPreconditionFailed, "precondition_failed", "The If-Match ETag belongs to a different resource.")
		return
	}

	var patch settingsPatch
	if !decodeJSON(w, r, &patch, false) {
		return
	}
	if patch.Name == nil && patch.Description == nil && patch.DefaultLocale == nil {
		writeProblem(w, http.StatusBadRequest, "validation_error", "At least one of name, description, or defaultLocale must be provided.")
		return
	}
	if err := validateSettingsPatch(&patch); err != nil {
		writeProblem(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	updated, err := a.store.Sites.UpdateSettings(r.Context(), siteID, expectedRev, patch.Name, patch.Description, patch.DefaultLocale)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeProblem(w, http.StatusNotFound, "not_found", "Site not found.")
		case errors.Is(err, store.ErrConflict):
			w.Header().Set("ETag", settingsETag(siteID, rec.Settings.Revision))
			writeProblem(w, http.StatusPreconditionFailed, "precondition_failed", "The settings changed after they were read; fetch again and retry with the new ETag.")
		default:
			a.logger.Error("update settings", "error", err)
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to update settings.")
		}
		return
	}
	w.Header().Set("ETag", settingsETag(siteID, updated.Revision))
	writeJSON(w, http.StatusOK, toSettingsDTO(updated))
}

// validateSettingsPatch enforces the field rules shared with the database
// CHECK constraints. Names are trimmed and must be 1-200 characters
// (runes); descriptions stay verbatim with a 1000-rune limit.
func validateSettingsPatch(patch *settingsPatch) error {
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > 200 {
			return errors.New("name must be 1 to 200 characters after trimming.")
		}
		patch.Name = &name
	}
	if patch.Description != nil {
		if n := utf8.RuneCountInString(*patch.Description); n > 1000 {
			return errors.New("description must be at most 1000 characters.")
		}
	}
	if patch.DefaultLocale != nil {
		locale := strings.TrimSpace(*patch.DefaultLocale)
		if locale != "zh-CN" && locale != "en-US" {
			return errors.New("defaultLocale must be one of: zh-CN, en-US.")
		}
		patch.DefaultLocale = &locale
	}
	return nil
}
