package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

func caddyCapabilityFixture(t *testing.T) (*testEnv, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	website := filepath.Join(dir, "external-website")
	if err := os.Mkdir(website, 0755); err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(website, "index.html")
	if err := os.WriteFile(content, []byte("existing live content"), 0644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "capability-uid")
	snapshot := filepath.Join(dir, "snapshot.json")
	data, err := json.Marshal(map[string]any{"apps": map[string]any{"http": map[string]any{"servers": map[string]any{"srv0": map[string]any{
		"listen": []string{":443"}, "routes": []any{map[string]any{"match": []any{map[string]any{"host": []string{"actual.example.test"}}}, "handle": []any{
			map[string]any{"handler": "vars", "root": website},
			map[string]any{"handler": "authentication", "providers": map[string]any{"http_basic": map[string]any{"accounts": []any{map[string]any{"username": "private-name", "password": "SENSITIVE-CADDY-FIXTURE"}}}}},
			map[string]any{"handler": "file_server"},
		}}},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, data, 0600); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "fixed-read-capability.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n/usr/bin/id -u > \"$2\"\ncat \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env := setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatal(err)
		}
		env.mutateConfig = func(cfg *Config) { cfg.CaddyReadCmd = []string{"/bin/sh", helper, snapshot, marker} }
	})
	return env, marker, content, website
}

func TestCaddySyncCapabilityRequiresFullAdminAndCSRFBeforeExecution(t *testing.T) {
	env, marker, _, _ := caddyCapabilityFixture(t)
	_, admin := adminClient(env)
	_, token, csrf := env.createUserSession("member-capability")
	adminRequest(t, env.clientFor(token, csrf), "POST", "/api/v1/admin/caddy/sync", nil, 403)
	adminRequest(t, env.newClient(), "POST", "/api/v1/admin/caddy/sync", nil, 401)
	adminRequest(t, env.clientFor(admin.token, ""), "POST", "/api/v1/admin/caddy/sync", nil, 403)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("unprivileged request executed the configured capability")
	}
}

func TestCaddySyncUsesOnlyOperatorArgvAndDoesNotProvisionOrExposeSecrets(t *testing.T) {
	env, marker, content, website := caddyCapabilityFixture(t)
	op, admin := adminClient(env)
	forged := filepath.Join(t.TempDir(), "http-controlled-command-ran")
	_, body := adminRequest(t, admin, "POST", "/api/v1/admin/caddy/sync", map[string]any{
		"command": []string{"/bin/sh", "-c", "touch " + forged}, "root": "/etc", "config": "SENSITIVE-CADDY-FIXTURE",
	}, 200)
	if strings.Contains(string(body), "SENSITIVE-CADDY-FIXTURE") {
		t.Fatal("sync response exposed raw configuration")
	}
	if _, err := os.Stat(forged); !os.IsNotExist(err) {
		t.Fatal("HTTP body replaced the operator-configured command")
	}
	uid, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(uid)) != strconv.Itoa(os.Geteuid()) {
		t.Fatal("read capability unexpectedly changed effective UID")
	}
	sites, err := env.store.Sites.ListForUser(t.Context(), op.ID)
	if err != nil || len(sites) != 1 {
		t.Fatalf("imported sites: %d %v", len(sites), err)
	}
	_, body = adminRequest(t, admin, "GET", "/api/v1/sites/"+sites[0].Site.ID.String()+"/caddy", nil, 200)
	if strings.Contains(string(body), "SENSITIVE-CADDY-FIXTURE") || strings.Contains(string(body), "private-name") || strings.Contains(string(body), "providers") {
		t.Fatal("site metadata returned authentication configuration")
	}
	if !strings.Contains(string(body), website) {
		t.Fatal("safe static root was not retained")
	}
	actual, err := os.ReadFile(content)
	if err != nil || string(actual) != "existing live content" {
		t.Fatal("sync modified the external site's content")
	}
	if _, err := os.Stat(filepath.Join(env.provisionRoot, sites[0].Site.ID.String())); !os.IsNotExist(err) {
		t.Fatal("sync provisioned managed content for an external site")
	}
}

func TestImportedCaddySitesCannotReachThemeBuildOrContentOperations(t *testing.T) {
	env, _, content, _ := caddyCapabilityFixture(t)
	op, admin := adminClient(env)
	adminRequest(t, admin, "POST", "/api/v1/admin/caddy/sync", nil, 200)
	sites, err := env.store.Sites.ListForUser(t.Context(), op.ID)
	if err != nil || len(sites) != 1 {
		t.Fatal(err)
	}
	base := "/api/v1/sites/" + sites[0].Site.ID.String()
	for _, target := range []struct{ method, path string }{
		{"GET", "/theme"}, {"PUT", "/theme"}, {"GET", "/posts"}, {"POST", "/posts"},
		{"PUT", "/posts/example.md"}, {"DELETE", "/posts/example.md"},
		{"GET", "/content/friends"}, {"PUT", "/content/friends"}, {"GET", "/site-config"}, {"PUT", "/site-config"},
		{"POST", "/albums/photos"}, {"POST", "/albums/process"}, {"PUT", "/albums/2026"}, {"DELETE", "/albums/2026/example.jpg"},
		{"POST", "/builds"}, {"POST", "/builds/" + uuid.NewString() + "/preview-links"}, {"POST", "/releases"}, {"POST", "/edge/reapply"},
	} {
		adminRequest(t, admin, target.method, base+target.path, map[string]any{}, 409)
	}
	actual, err := os.ReadFile(content)
	if err != nil || string(actual) != "existing live content" {
		t.Fatal("blocked operation modified external site data")
	}
	var builds, selections int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM site_builds WHERE site_id=$1`, sites[0].Site.ID).Scan(&builds); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM site_theme_selections WHERE site_id=$1`, sites[0].Site.ID).Scan(&selections); err != nil {
		t.Fatal(err)
	}
	if builds != 0 || selections != 0 {
		t.Fatal("external-site guard let a theme or build mutation reach the store")
	}
	viewer, token, csrf := env.createUserSession("assigned-viewer")
	if _, err := env.pool.Exec(t.Context(), `INSERT INTO site_memberships(site_id,user_id,role) VALUES($1,$2,$3)`, sites[0].Site.ID, viewer.ID, model.RoleViewer); err != nil {
		t.Fatal(err)
	}
	adminRequest(t, env.clientFor(token, csrf), "GET", base+"/caddy", nil, 200)
	adminRequest(t, env.clientFor(token, csrf), "POST", "/api/v1/admin/caddy/sync", nil, 403)
}
