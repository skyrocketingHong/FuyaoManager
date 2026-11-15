package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildinfo"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/platformupdate"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func (a *API) handleApplicationVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, buildinfo.Current())
}

func (a *API) handlePlatformUpdates(w http.ResponseWriter, r *http.Request, _ store.AuthSession) {
	if a.cfg.UpdaterSocket == "" {
		writeJSON(w, 200, platformupdate.Status{Versions: []platformupdate.Version{}, Logs: []string{}})
		return
	}
	a.proxyUpdate(w, r, "GET", "/info", nil)
}

func (a *API) handleUpdateAction(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if a.cfg.UpdaterSocket == "" {
		writeProblem(w, 503, "update_unavailable", "The platform updater is not configured.")
		return
	}
	var action platformupdate.Action
	if !decodeJSON(w, r, &action, false) {
		return
	}
	if action.Action == "check" {
		raw, _ := json.Marshal(action)
		a.proxyUpdate(w, r, "POST", "/action", raw)
		return
	}
	// ASVS 8.3.1/15.4.2: repeat the administrator and session check while
	// holding the account lifecycle lock through supervisor admission.
	err := a.store.AdmitPlatformUpdate(r.Context(), auth, func(ctx context.Context) error {
		raw, _ := json.Marshal(action)
		a.proxyUpdate(w, r.WithContext(ctx), "POST", "/action", raw)
		return nil
	})
	if err != nil {
		a.adminError(w, r, err)
	}
}

func (a *API) proxyUpdate(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	req, err := http.NewRequestWithContext(r.Context(), method, "http://updater"+path, bytes.NewReader(body))
	if err != nil {
		writeProblem(w, 503, "update_unavailable", "The platform updater request failed.")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := platformupdate.Client(a.cfg.UpdaterSocket)
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		writeProblem(w, 503, "update_unavailable", "The update receipt is unavailable. Refresh the original task before another action.")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&problem)
		if problem.Error == "" {
			problem.Error = "The update action was rejected."
		}
		writeProblem(w, 409, "update_rejected", problem.Error)
		return
	}
	var status platformupdate.Status
	if err := json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&status); err != nil && !errors.Is(err, io.EOF) {
		writeProblem(w, 503, "update_unavailable", "The platform updater response is invalid.")
		return
	}
	writeJSON(w, 200, status)
}
