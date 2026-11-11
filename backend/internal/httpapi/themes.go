package httpapi

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// ThemeConfigValidator validates a theme configuration against its theme's
// configuration rules. themecontract.Set implements it; keeping the interface
// here lets the handler run without a compiled contract set (feature then
// disabled with an explicit 503).
type ThemeConfigValidator interface {
	ValidateThemeConfig(schemaDoc, configData []byte) error
	ValidateEmptyThemeConfig(configData []byte) error
}

// themeDTO is the public representation of a catalog entry. It exposes the
// validated manifest content and the configuration schema the admin console
// renders its form from, but never the server-side source path or any other
// filesystem detail.
type themeDTO struct {
	ID             string          `json:"id"`
	ThemeID        string          `json:"themeId"`
	Version        string          `json:"version"`
	Name           string          `json:"name,omitempty"`
	Description    string          `json:"description,omitempty"`
	Adapter        string          `json:"adapter"`
	InputProtocol  string          `json:"inputProtocol"`
	Capabilities   []string        `json:"capabilities"`
	Outputs        themeOutputsDTO `json:"outputs"`
	ConfigSchema   json.RawMessage `json:"configSchema"`
	ManifestDigest string          `json:"manifestDigest"`
	RegisteredAt   string          `json:"registeredAt"`
	UpdatedAt      string          `json:"updatedAt"`
}

type themeOutputsDTO struct {
	SpaFallback       *string  `json:"spaFallback"`
	URLConventions    []string `json:"urlConventions"`
	RequiredArtifacts []string `json:"requiredArtifacts"`
}

// selectedThemeDTO identifies the selected catalog entry inside a selection
// response: theme id plus version, distinguishable by the manifest digest.
type selectedThemeDTO struct {
	ThemeID        string `json:"themeId"`
	Version        string `json:"version"`
	ManifestDigest string `json:"manifestDigest"`
}

// siteThemeDTO is a site's theme selection. revision 0 and theme null mean no
// selection yet; the ETag carries the same revision for If-Match.
type siteThemeDTO struct {
	SiteID      string            `json:"siteId"`
	Selected    bool              `json:"selected"`
	Theme       *selectedThemeDTO `json:"theme"`
	ThemeConfig json.RawMessage   `json:"themeConfig"`
	Revision    int64             `json:"revision"`
	UpdatedAt   *string           `json:"updatedAt"`
}

// themeSelectionPut is the strict PUT body: themeId and version name a
// catalog entry; themeConfig is optional and defaults to an empty object. An
// explicit JSON null is not the same as omitting the field and is rejected.
type themeSelectionPut struct {
	ThemeID     string       `json:"themeId"`
	Version     string       `json:"version"`
	ThemeConfig optionalJSON `json:"themeConfig"`
}

// optionalJSON preserves the omitted/present distinction that plain pointer
// or RawMessage decoding loses: the standard library decodes a JSON null into
// a nil pointer, which is indistinguishable from a missing field. Its
// UnmarshalJSON is invoked even for null (with the literal bytes), so
// "present but null" stays observable.
type optionalJSON struct {
	Present bool
	Value   json.RawMessage
}

func (o *optionalJSON) UnmarshalJSON(data []byte) error {
	o.Present = true
	o.Value = append(json.RawMessage(nil), data...)
	return nil
}

// themeConfigDetail is the client-facing rejection detail; validator output
// can embed local schema resource paths, so the exact error is logged
// server-side instead of being echoed to the client.
const themeConfigDetail = "The theme configuration does not satisfy the theme's configuration schema."

var (
	themeIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)
	themeVersionReg = regexp.MustCompile(`^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*)?(?:\+[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*)?$`)
)

func toThemeDTO(t model.Theme) (themeDTO, error) {
	view, err := model.ParseThemeManifest(t.Manifest)
	if err != nil {
		return themeDTO{}, err
	}
	return themeDTO{
		ID:             t.ID.String(),
		ThemeID:        t.ThemeID,
		Version:        t.Version,
		Name:           view.Name,
		Description:    view.Description,
		Adapter:        t.Adapter,
		InputProtocol:  view.InputProtocol,
		Capabilities:   view.Capabilities,
		Outputs:        themeOutputsDTO(view.Outputs),
		ConfigSchema:   t.ConfigSchema,
		ManifestDigest: hex.EncodeToString(t.ManifestDigest),
		RegisteredAt:   t.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:      t.UpdatedAt.UTC().Format(timeFormat),
	}, nil
}

func toSiteThemeDTO(siteID string, rec store.SelectionRecord) (siteThemeDTO, error) {
	dto := siteThemeDTO{SiteID: siteID, ThemeConfig: json.RawMessage("null")}
	if rec.Selection == nil {
		return dto, nil
	}
	dto.Selected = true
	dto.ThemeConfig = rec.Selection.ThemeConfig
	dto.Revision = rec.Selection.Revision
	updated := rec.Selection.UpdatedAt.UTC().Format(timeFormat)
	dto.UpdatedAt = &updated
	dto.Theme = &selectedThemeDTO{
		ThemeID:        rec.Theme.ThemeID,
		Version:        rec.Theme.Version,
		ManifestDigest: hex.EncodeToString(rec.Theme.ManifestDigest),
	}
	return dto, nil
}

// themeETag renders the strong entity tag for a theme-selection revision;
// t0 is the unselected state.
func themeETag(siteID string, revision int64) string {
	return fmt.Sprintf(`"%s-t%d"`, siteID, revision)
}

// parseThemeIfMatch parses an If-Match header naming a theme-selection ETag
// ("<siteId>-t<revision>", revision 0 allowed for the unselected state). The
// wildcard form is rejected, mirroring the settings contract.
func parseThemeIfMatch(header string) (siteID string, revision int64, err error) {
	value := strings.TrimSpace(header)
	if value == "*" {
		return "", 0, fmt.Errorf("If-Match must be an exact ETag, not *")
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", 0, fmt.Errorf("If-Match must be the quoted ETag returned by GET the site theme")
	}
	inner := value[1 : len(value)-1]
	idx := strings.LastIndex(inner, "-t")
	if idx < 0 {
		return "", 0, fmt.Errorf("If-Match does not match the theme ETag format")
	}
	id, revStr := inner[:idx], inner[idx+2:]
	rev, revErr := strconv.ParseInt(revStr, 10, 64)
	if _, uuidErr := uuid.Parse(id); uuidErr != nil || revErr != nil || rev < 0 {
		return "", 0, fmt.Errorf("If-Match does not match the theme ETag format")
	}
	if inner != fmt.Sprintf("%s-t%d", id, rev) {
		return "", 0, fmt.Errorf("If-Match does not match the theme ETag format")
	}
	return id, rev, nil
}

// handleListThemes returns the registered theme catalog. Any authenticated
// administrator may read it, viewers included.
func (a *API) handleListThemes(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	themes, err := a.store.Themes.List(r.Context())
	if err != nil {
		a.logger.Error("list themes", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to list themes.")
		return
	}
	out := make([]themeDTO, 0, len(themes))
	for _, t := range themes {
		dto, err := toThemeDTO(t)
		if err != nil {
			a.logger.Error("decode stored theme manifest", "themeId", t.ThemeID, "error", err)
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to list themes.")
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"themes": out})
}

// handleGetSiteTheme returns the site's theme selection plus the strong ETag.
func (a *API) handleGetSiteTheme(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rec, err := a.store.Themes.GetSelectionForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	var revision int64
	if rec.Selection != nil {
		revision = rec.Selection.Revision
	}
	dto, err := toSiteThemeDTO(siteID.String(), rec)
	if err != nil {
		a.logger.Error("decode site theme selection", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to read the site theme.")
		return
	}
	w.Header().Set("ETag", themeETag(siteID.String(), revision))
	writeJSON(w, http.StatusOK, dto)
}

// handlePutSiteTheme saves the site's theme selection and configuration under
// optimistic concurrency. If-Match is mandatory (428 when missing); the theme
// and version must exist in the catalog (404) and the configuration must
// satisfy the theme's configuration schema (422). Never silently downgraded.
func (a *API) handlePutSiteTheme(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
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
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may change the site theme.")
		return
	}

	ifMatch := r.Header.Get("If-Match")
	if strings.TrimSpace(ifMatch) == "" {
		writeProblem(w, http.StatusPreconditionRequired, "precondition_required", "PUT requires an If-Match header carrying the current theme ETag.")
		return
	}
	etagSiteID, expectedRev, err := parseThemeIfMatch(ifMatch)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_if_match", "If-Match must be the exact ETag returned by GET the site theme; the wildcard form is not accepted.")
		return
	}
	if etagSiteID != siteID.String() {
		w.Header().Set("ETag", themeETag(siteID.String(), selectionRevision(rec)))
		writeProblem(w, http.StatusPreconditionFailed, "precondition_failed", "The If-Match ETag belongs to a different resource.")
		return
	}

	var body themeSelectionPut
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if !themeIDPattern.MatchString(body.ThemeID) {
		writeProblem(w, http.StatusBadRequest, "validation_error", "themeId must be a lowercase kebab-case theme identifier.")
		return
	}
	if !themeVersionReg.MatchString(body.Version) {
		writeProblem(w, http.StatusBadRequest, "validation_error", "version must be a SemVer 2.0.0 string.")
		return
	}
	themeConfig := []byte("{}")
	if body.ThemeConfig.Present {
		if bytes.Equal(bytes.TrimSpace(body.ThemeConfig.Value), []byte("null")) {
			writeProblem(w, http.StatusBadRequest, "validation_error", "themeConfig must be a JSON object; explicitly null is not accepted.")
			return
		}
		themeConfig = body.ThemeConfig.Value
	}

	theme, err := a.store.Themes.Get(r.Context(), body.ThemeID, body.Version)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "theme_not_found", "The theme and version are not registered in the catalog.")
		return
	}
	if err != nil {
		a.logger.Error("lookup theme", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to save the site theme.")
		return
	}

	if a.themeContracts == nil {
		writeProblem(w, http.StatusServiceUnavailable, "theme_contracts_unavailable", "Theme contract validation is not configured on this server; set FUYAO_ADMIN_THEME_CONTRACTS.")
		return
	}
	if theme.ConfigSchema == nil {
		if err := a.themeContracts.ValidateEmptyThemeConfig(themeConfig); err != nil {
			a.logger.Warn("theme config rejected", "siteId", siteID, "themeId", theme.ThemeID, "error", err)
			writeProblem(w, http.StatusUnprocessableEntity, "invalid_theme_config", themeConfigDetail)
			return
		}
	} else if err := a.themeContracts.ValidateThemeConfig(theme.ConfigSchema, themeConfig); err != nil {
		a.logger.Warn("theme config rejected", "siteId", siteID, "themeId", theme.ThemeID, "error", err)
		writeProblem(w, http.StatusUnprocessableEntity, "invalid_theme_config", themeConfigDetail)
		return
	}

	updated, err := a.store.Themes.SaveSelection(r.Context(), siteID, theme.ID, auth.User.ID, themeConfig, expectedRev)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeProblem(w, http.StatusNotFound, "theme_not_found", "The theme and version are not registered in the catalog.")
		case errors.Is(err, store.ErrConflict):
			// Recompute the current ETag from a fresh read: the snapshot read
			// at request start may itself be stale after a concurrent writer
			// won the race, and it must not be advertised as current.
			a.writeCurrentThemeETag(w, r, siteID, auth.User.ID)
			writeProblem(w, http.StatusPreconditionFailed, "precondition_failed", "The theme selection changed after it was read; fetch again and retry with the new ETag.")
		default:
			a.logger.Error("save theme selection", "error", err)
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to save the site theme.")
		}
		return
	}
	dto, err := toSiteThemeDTO(siteID.String(), store.SelectionRecord{Selection: &updated, Theme: theme})
	if err != nil {
		a.logger.Error("decode saved selection", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to save the site theme.")
		return
	}
	w.Header().Set("ETag", themeETag(siteID.String(), updated.Revision))
	writeJSON(w, http.StatusOK, dto)
}

func selectionRevision(rec store.SelectionRecord) int64 {
	if rec.Selection == nil {
		return 0
	}
	return rec.Selection.Revision
}

// writeCurrentThemeETag stamps the freshest theme-selection ETag it can read
// right now. When the fresh read itself fails the header is omitted rather
// than risking an advertised stale value.
func (a *API) writeCurrentThemeETag(w http.ResponseWriter, r *http.Request, siteID, userID uuid.UUID) {
	fresh, err := a.store.Themes.GetSelectionForUser(r.Context(), siteID, userID)
	if err != nil {
		a.logger.Error("refresh theme ETag after conflict", "siteId", siteID, "error", err)
		return
	}
	w.Header().Set("ETag", themeETag(siteID.String(), selectionRevision(fresh)))
}
