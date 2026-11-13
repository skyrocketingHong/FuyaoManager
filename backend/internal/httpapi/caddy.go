package httpapi

import (
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyinventory"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"net/http"
)

func (a *API) handleSyncCaddy(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if len(a.cfg.CaddyReadCmd) == 0 {
		writeProblem(w, 503, "caddy_unavailable", "Caddy inventory is not configured.")
		return
	}
	entries, err := caddyinventory.Read(r.Context(), a.cfg.CaddyReadCmd)
	if err != nil {
		writeProblem(w, 422, "caddy_invalid", "The Caddy snapshot could not be parsed.")
		return
	}
	count, err := a.store.SyncCaddySites(r.Context(), auth.User.ID, entries)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int{"imported": count})
}
func (a *API) handleGetCaddySite(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	a.writeCaddyState(w, r, auth, id)
}
func (a *API) handleOpenAPI(w http.ResponseWriter, r *http.Request, _ store.AuthSession) {
	if len(a.cfg.OpenAPIJSON) == 0 {
		writeProblem(w, 503, "catalog_unavailable", "The platform contract is not configured.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(a.cfg.OpenAPIJSON)
}
