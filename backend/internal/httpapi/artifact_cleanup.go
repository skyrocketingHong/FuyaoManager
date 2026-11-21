package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/managedpublish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func (a *API) handleDeleteBuildArtifacts(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	a.deleteStaticArtifacts(w, r, auth, false)
}

func (a *API) handleDeleteReleaseArtifacts(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	a.deleteStaticArtifacts(w, r, auth, true)
}

func (a *API) deleteStaticArtifacts(w http.ResponseWriter, r *http.Request, auth store.AuthSession, releases bool) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if a.artifactCleaner == nil {
		writeProblem(w, http.StatusServiceUnavailable, "artifact_cleanup_unavailable", "Static artifact cleanup is unavailable.")
		return
	}
	var body struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	seen := make(map[uuid.UUID]bool, len(body.IDs))
	for _, id := range body.IDs {
		if id == uuid.Nil || seen[id] {
			writeProblem(w, http.StatusBadRequest, "validation_error", "Use distinct build or release IDs.")
			return
		}
		seen[id] = true
	}
	if len(body.IDs) < 1 || len(body.IDs) > 100 {
		writeProblem(w, http.StatusBadRequest, "validation_error", "Select between 1 and 100 artifacts.")
		return
	}
	err := a.artifactCleaner.Delete(r.Context(), siteID, auth.User.ID, body.IDs, releases)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "artifact_not_found", "One or more artifacts do not belong to this site.")
	case errors.Is(err, store.ErrOperatorForbidden):
		writeProblem(w, http.StatusForbidden, "forbidden", "Artifact deletion requires the platform administrator and site owner role.")
	case errors.Is(err, publish.ErrArtifactProtected), errors.Is(err, store.ErrConflict), errors.Is(err, managedpublish.ErrStateConflict):
		writeProblem(w, http.StatusConflict, "artifact_protected", "One or more artifacts are live, in use, or otherwise protected.")
	case errors.Is(err, managedpublish.ErrDisabled):
		writeProblem(w, http.StatusServiceUnavailable, "artifact_cleanup_unavailable", "Cannot verify the site's active publication.")
	default:
		a.logger.Error("delete static artifacts", "siteId", siteID, "error", err)
		writeProblem(w, http.StatusServiceUnavailable, "artifact_cleanup_incomplete", "Artifact cleanup could not complete. Refresh and retry.")
	}
}
