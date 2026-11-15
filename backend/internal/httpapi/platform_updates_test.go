package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func TestPlatformUpdatesRequireFullAdministratorAndCSRF(t *testing.T) {
	dir, err := os.MkdirTemp("", "fu-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "updater.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	updater := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"enabled": true, "repository": "owner/repo", "versions": []any{}, "logs": []string{}})
	})}
	go func() { _ = updater.Serve(listener) }()
	t.Cleanup(func() { _ = updater.Close() })
	env := setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatal(err)
		}
		env.mutateConfig = func(cfg *Config) { cfg.UpdaterSocket = socket }
	})
	user, admin := adminClient(env)
	_, token, csrf := env.createUserSession("member-update")
	path := "/api/v1/admin/updates"
	for _, method := range []string{"GET", "POST"} {
		var body any
		if method == "POST" {
			body = map[string]any{"action": "pin", "on": true}
		}
		adminRequest(t, env.newClient(), method, path, body, 401)
		adminRequest(t, env.clientFor(token, csrf), method, path, body, 403)
	}
	restricted, restrictedCSRF, _, err := env.store.Sessions.CreateWithKind(t.Context(), user.ID, model.SessionKindRestricted, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	adminRequest(t, env.clientFor(restricted, restrictedCSRF), "POST", path, map[string]any{"action": "pin", "on": true}, 403)
	adminRequest(t, env.clientFor(admin.token, ""), "POST", path, map[string]any{"action": "pin", "on": true}, 403)
	if calls.Load() != 0 {
		t.Fatal("unprivileged request reached root supervisor")
	}
	adminRequest(t, admin, "GET", path, nil, 200)
	adminRequest(t, admin, "POST", path, map[string]any{"action": "pin", "on": true}, 200)
	if calls.Load() != 2 {
		t.Fatal("authorized request did not reach supervisor")
	}
	// Admission repeats the session validity check, even for a stale AuthSession.
	var sessionID uuid.UUID
	err = env.pool.QueryRow(t.Context(), `SELECT id FROM sessions WHERE user_id=$1 AND kind='full'`, user.ID).Scan(&sessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.pool.Exec(t.Context(), `UPDATE sessions SET revoked_at=now() WHERE id=$1`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = env.store.AdmitPlatformUpdate(t.Context(), store.AuthSession{User: user, Session: model.Session{ID: sessionID}}, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, store.ErrOperatorForbidden) || called {
		t.Fatal("revoked session reached supervisor admission", err)
	}
}

func TestPlatformUpdaterAbsentAndVersionIdentity(t *testing.T) {
	env := setup(t)
	_, admin := adminClient(env)
	_, raw := adminRequest(t, admin, "GET", "/api/v1/admin/updates", nil, 200)
	var info struct {
		Enabled bool `json:"enabled"`
	}
	decodeJSONBody(t, raw, &info)
	if info.Enabled {
		t.Fatal("invented an installed supervisor")
	}
	adminRequest(t, admin, "POST", "/api/v1/admin/updates", map[string]string{"action": "check"}, 503)
	adminRequest(t, env.newClient(), "GET", "/api/v1/version", nil, 200)
}
