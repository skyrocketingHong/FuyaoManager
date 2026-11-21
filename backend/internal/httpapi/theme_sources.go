package httpapi

import (
	"errors"
	"net/http"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themeimport"
)

func (a *API) handleListThemeSources(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	sources := []themeimport.SourceStatus{}
	if a.themeImporter != nil {
		sources = a.themeImporter.List()
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (a *API) handleCheckThemeSource(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if a.themeImporter == nil {
		a.writeThemeSourceError(w, store.ErrNotFound)
		return
	}
	candidate, err := a.themeImporter.Check(r.Context(), r.PathValue("sourceId"))
	if err != nil {
		a.writeThemeSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, candidate)
}

func (a *API) handleInstallThemeSource(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if a.themeImporter == nil {
		a.writeThemeSourceError(w, store.ErrNotFound)
		return
	}
	var body struct {
		Commit string `json:"commit"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	task, err := a.themeImporter.Install(r.Context(), r.PathValue("sourceId"), body.Commit, auth.User.ID)
	if err != nil {
		a.writeThemeSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func (a *API) writeThemeSourceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, 404, "theme_source_not_found", "This GitHub theme source is not configured.")
	case errors.Is(err, themeimport.ErrIncompatible):
		writeProblem(w, 422, "theme_source_incompatible", "The repository does not provide a compatible theme.")
	case errors.Is(err, themeimport.ErrStale):
		writeProblem(w, 409, "theme_source_conflict", "The source changed or the same version is already registered. Check again.")
	case errors.Is(err, themeimport.ErrBusy):
		writeProblem(w, 409, "theme_install_busy", "A theme installation is already running.")
	case errors.Is(err, themeimport.ErrSource):
		a.logger.Warn("check GitHub theme", "error", err)
		writeProblem(w, 502, "theme_source_failed", "GitHub theme check failed. Retry after checking the repository and branch.")
	default:
		a.logger.Error("read GitHub theme", "error", err)
		writeProblem(w, 500, "internal_error", "Cannot read the theme source.")
	}
}
