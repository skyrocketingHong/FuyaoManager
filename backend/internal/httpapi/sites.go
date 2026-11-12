package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// siteDTO is the public representation of a site with the caller's role.
type siteDTO struct {
	SiteID           string          `json:"siteId"`
	Slug             string          `json:"slug"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	DefaultLocale    string          `json:"defaultLocale"`
	Visibility       string          `json:"visibility"`
	ProvisionState   string          `json:"provisionState"`
	ProvisionFailure json.RawMessage `json:"provisionFailure,omitempty"`
	Role             string          `json:"role"`
	SettingsRevision int64           `json:"settingsRevision"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
}

// siteCreate is the strict POST /api/v1/sites body. themeId/themeVersion/
// themeConfig optionally record an initial theme selection (validated like a
// theme PUT); visibility defaults to private.
type siteCreate struct {
	Slug          string          `json:"slug"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	DefaultLocale string          `json:"defaultLocale"`
	Visibility    string          `json:"visibility"`
	ThemeID       string          `json:"themeId"`
	ThemeVersion  string          `json:"themeVersion"`
	ThemeConfig   json.RawMessage `json:"themeConfig"`
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
		Visibility:       rec.Site.Visibility,
		ProvisionState:   rec.Site.ProvisionState,
		ProvisionFailure: rec.Site.ProvisionFailure,
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

var siteSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// handleCreateSite creates a site from the admin UI: database rows (site,
// settings, owner membership) in one transaction, an optional initial theme
// selection validated exactly like a theme PUT, and then the on-disk
// provisioning. A database success alone never reports the site as ready —
// the provision state tells the UI whether directory setup succeeded, failed
// (retryable), or is still pending. Slug uniqueness is enforced by the
// database, so a duplicate or raced request can never create a second site.
func (a *API) handleCreateSite(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if a.provisioner == nil {
		writeProblem(w, http.StatusServiceUnavailable, "provisioning_unavailable", "Site provisioning is not configured on this server.")
		return
	}
	var body siteCreate
	if !decodeJSON(w, r, &body, false) {
		return
	}
	body.Slug = strings.TrimSpace(body.Slug)
	body.Name = strings.TrimSpace(body.Name)
	body.DefaultLocale = strings.TrimSpace(body.DefaultLocale)
	if body.DefaultLocale == "" {
		body.DefaultLocale = "zh-CN"
	}
	if body.Visibility == "" {
		body.Visibility = model.SiteVisibilityPrivate
	}
	if !siteSlugPattern.MatchString(body.Slug) {
		writeProblem(w, http.StatusBadRequest, "validation_error", "slug must match ^[a-z0-9][a-z0-9-]{0,62}$.")
		return
	}
	if n := utf8.RuneCountInString(body.Name); n < 1 || n > 200 {
		writeProblem(w, http.StatusBadRequest, "validation_error", "name must be 1 to 200 characters.")
		return
	}
	if n := utf8.RuneCountInString(body.Description); n > 1000 {
		writeProblem(w, http.StatusBadRequest, "validation_error", "description must be at most 1000 characters.")
		return
	}
	if body.DefaultLocale != "zh-CN" && body.DefaultLocale != "en-US" {
		writeProblem(w, http.StatusBadRequest, "validation_error", "defaultLocale must be one of: zh-CN, en-US.")
		return
	}
	if body.Visibility != model.SiteVisibilityPrivate && body.Visibility != model.SiteVisibilityPublic {
		writeProblem(w, http.StatusBadRequest, "validation_error", "visibility must be one of: private, public.")
		return
	}

	// Optional initial theme selection: validated up front (catalog lookup +
	// contract check) so an invalid theme fails the creation atomically
	// instead of leaving a half-configured site.
	var theme *model.Theme
	themeConfig := []byte("{}")
	if body.ThemeID != "" {
		if !themeIDPattern.MatchString(body.ThemeID) {
			writeProblem(w, http.StatusBadRequest, "validation_error", "themeId must be a lowercase kebab-case theme identifier.")
			return
		}
		if !themeVersionReg.MatchString(body.ThemeVersion) {
			writeProblem(w, http.StatusBadRequest, "validation_error", "themeVersion must be a SemVer 2.0.0 string.")
			return
		}
		if len(body.ThemeConfig) > 0 && !bytes.Equal(bytes.TrimSpace(body.ThemeConfig), []byte("null")) {
			themeConfig = body.ThemeConfig
		}
		t, err := a.store.Themes.Get(r.Context(), body.ThemeID, body.ThemeVersion)
		if errors.Is(err, store.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "theme_not_found", "The theme and version are not registered in the catalog.")
			return
		}
		if err != nil {
			a.logger.Error("lookup theme for site creation", "error", err)
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to create the site.")
			return
		}
		if a.themeContracts == nil {
			writeProblem(w, http.StatusServiceUnavailable, "theme_contracts_unavailable", "Theme contract validation is not configured on this server; set FUYAO_ADMIN_THEME_CONTRACTS.")
			return
		}
		if t.ConfigSchema == nil {
			if err := a.themeContracts.ValidateEmptyThemeConfig(themeConfig); err != nil {
				writeProblem(w, http.StatusUnprocessableEntity, "invalid_theme_config", themeConfigDetail)
				return
			}
		} else if err := a.themeContracts.ValidateThemeConfig(t.ConfigSchema, themeConfig); err != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "invalid_theme_config", themeConfigDetail)
			return
		}
		theme = &t
	}

	rec, err := a.store.Sites.Create(r.Context(), store.NewSiteParams{
		Slug:          body.Slug,
		Name:          body.Name,
		Description:   body.Description,
		DefaultLocale: body.DefaultLocale,
		Visibility:    body.Visibility,
	}, auth.User.ID)
	if err != nil {
		if isUniqueViolation(err) {
			writeProblem(w, http.StatusConflict, "slug_taken", "A site with this slug already exists.")
			return
		}
		a.logger.Error("create site", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to create the site.")
		return
	}

	if theme != nil {
		if _, err := a.store.Themes.SaveSelection(r.Context(), rec.Site.ID, theme.ID, auth.User.ID, themeConfig, 0); err != nil {
			a.logger.Error("save initial theme selection", "siteId", rec.Site.ID, "error", err)
			// The site itself exists; the theme can still be selected via
			// the normal PUT, so surface the state instead of failing the
			// whole creation.
			writeProblem(w, http.StatusInternalServerError, "internal_error", "The site was created but the initial theme selection failed; select the theme from the site page.")
			return
		}
	}

	rec, _ = a.provisionSite(r, rec, auth.User.ID)
	// 201 also when provisioning failed: the site exists, the response
	// carries provisionState=failed, and the retry endpoint can fix it.
	writeJSON(w, http.StatusCreated, toSiteDTO(rec))
}

// handleProvisionSite retries directory provisioning for a site whose
// provision state is not ready. Idempotent: provisioning never overwrites
// existing files, and a site already ready stays ready.
func (a *API) handleProvisionSite(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if a.provisioner == nil {
		writeProblem(w, http.StatusServiceUnavailable, "provisioning_unavailable", "Site provisioning is not configured on this server.")
		return
	}
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
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may provision the site.")
		return
	}
	if rec.Site.ProvisionState == model.SiteReady {
		writeJSON(w, http.StatusOK, toSiteDTO(rec))
		return
	}
	rec, _ = a.provisionSite(r, rec, auth.User.ID)
	writeJSON(w, http.StatusOK, toSiteDTO(rec))
}

// provisionSite runs the directory provisioning and persists the resulting
// state, returning the refreshed record and the failure that was recorded
// (nil on success). The site's network entry is allocated first so the
// scaffold site.yaml seeds the real entry URL (http://<host>:<port>) — never
// an invented domain.
func (a *API) provisionSite(r *http.Request, rec store.SiteRecord, userID uuid.UUID) (store.SiteRecord, model.ProvisionFailure) {
	failure := model.ProvisionFailure{}
	baseURL := ""
	if a.store.NetworkEntries != nil && a.edge != nil {
		if entry, _, err := a.store.NetworkEntries.EnsureSiteEntry(r.Context(), rec.Site.ID, 20000, 20100); err == nil {
			baseURL = a.edge.EntryURL(rec.Site.Visibility, entry.Port)
		} else {
			a.logger.Error("allocate site entry at provisioning", "siteId", rec.Site.ID, "error", err)
		}
	}
	if err := a.provisioner.Provision(rec.Site, rec.Settings, baseURL); err != nil {
		failure = model.ProvisionFailure{Stage: "directories", Message: err.Error()}
		a.logger.Error("provision site", "siteId", rec.Site.ID, "error", err)
	}
	if err := a.store.Sites.MarkProvisioned(r.Context(), rec.Site.ID, failure); err != nil {
		a.logger.Error("mark provision state", "siteId", rec.Site.ID, "error", err)
	}
	refreshed, err := a.store.Sites.GetForUser(r.Context(), rec.Site.ID, userID)
	if err == nil {
		return refreshed, failure
	}
	return rec, failure
}

// handlePutVisibility switches a site between public and private. Changing
// visibility re-binds the site's edge entry (private entries answer only on
// the private interface via an explicit bind, public entries on all
// interfaces) by re-applying the managed reverse-proxy configuration; the
// database switch commits first, so a failing re-apply stays repairable and
// is reported.
//
// A request carrying the database's current value is NOT a no-op: the
// database saying "private" does not imply the running proxy actually binds
// the private interface (a previous apply may have failed), so the handler
// always re-applies and reports the outcome. That makes retry the recovery
// path for a failed visibility switch.
func (a *API) handlePutVisibility(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	var body struct {
		Visibility string `json:"visibility"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if body.Visibility != model.SiteVisibilityPrivate && body.Visibility != model.SiteVisibilityPublic {
		writeProblem(w, http.StatusBadRequest, "validation_error", "visibility must be one of: private, public.")
		return
	}
	rec, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	if rec.Role != model.RoleOwner && rec.Role != model.RoleEditor {
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may change site visibility.")
		return
	}
	if rec.Site.Visibility != body.Visibility {
		if err := a.store.Sites.UpdateVisibility(r.Context(), siteID, body.Visibility); err != nil {
			a.logger.Error("update visibility", "siteId", siteID, "error", err)
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to change visibility.")
			return
		}
	}
	if a.edge != nil {
		if err := a.edge.Reconcile(r.Context()); err != nil {
			a.logger.Error("edge apply after visibility change", "siteId", siteID, "error", err)
			writeProblem(w, http.StatusConflict, "edge_apply_failed", "The visibility is recorded in the database but the site entry update failed; retry to re-apply.")
			return
		}
		// The entry host changed with the visibility: keep site.yaml's
		// canonical URL aligned so RSS/Sitemap/canonical outputs of the next
		// build use the real entry (a rebuild is required to re-emit them).
		if entry, err := a.store.NetworkEntries.GetSiteEntry(r.Context(), siteID); err == nil {
			newURL := a.edge.EntryURL(body.Visibility, entry.Port)
			if err := a.content.UpdateSiteBaseURL(siteID, newURL); err != nil {
				a.logger.Error("rewrite site base url after visibility change", "siteId", siteID, "error", err)
			}
		}
	}
	refreshed, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSiteDTO(refreshed))
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
