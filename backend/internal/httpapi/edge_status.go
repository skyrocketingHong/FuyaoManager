package httpapi

import (
	"errors"
	"net/http"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/edge"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// handleGetSiteEdge returns the managed-entry view for one site: the
// effective address, apply health, and the snippet's desired-vs-applied
// state. Read-only; the reapply endpoint is the recovery path.
func (a *API) handleGetSiteEdge(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rec, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	if a.edge == nil {
		writeProblem(w, http.StatusServiceUnavailable, "edge_unavailable", "Edge management is not configured on this server.")
		return
	}
	full := a.edge.State(r.Context())
	entry := edge.StateSite{
		SiteID:     siteID,
		Slug:       rec.Site.Slug,
		Visibility: rec.Site.Visibility,
	}
	if e, err := a.store.NetworkEntries.GetSiteEntry(r.Context(), siteID); err == nil {
		entry.Port = e.Port
		entry.OriginURL = a.edge.EntryURL(rec.Site.Visibility, e.Port)
	} else if !errors.Is(err, store.ErrNotFound) {
		a.logger.Error("read site entry for edge status", "siteId", siteID, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"site":          entry,
		"inSync":        full.InSync,
		"lastAppliedAt": full.LastApplied,
		"lastError":     full.LastError,
		"snippetPath":   full.SnippetPath,
		"privateBind":   full.PrivateBind,
		"publicHost":    full.PublicHost,
	})
}

// handleReapplySiteEdge re-runs the managed apply (render + validate +
// reload) and reports the outcome — the recovery path for a failed or
// drifted edge configuration. Owners and editors only.
func (a *API) handleReapplySiteEdge(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rec, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	if rec.Role != "owner" && rec.Role != "editor" {
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may re-apply the site entry.")
		return
	}
	if a.edge == nil {
		writeProblem(w, http.StatusServiceUnavailable, "edge_unavailable", "Edge management is not configured on this server.")
		return
	}
	if err := a.edge.Reconcile(r.Context()); err != nil {
		a.logger.Error("edge reapply", "siteId", siteID, "error", err)
		writeProblem(w, http.StatusConflict, "edge_apply_failed", "The edge re-apply failed; the last good configuration keeps serving. Check the server logs and retry.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
