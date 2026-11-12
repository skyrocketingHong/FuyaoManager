package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/edge"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// TestSiteURLFollowsManagedEntry proves the site's canonical URL is real at
// every stage: site creation seeds site.yaml with the allocated entry
// (http://host:port, never an invented domain), a visibility switch rewrites
// it, and the config surface reports the effective origin.
func TestSiteURLFollowsManagedEntry(t *testing.T) {
	snippet := t.TempDir()
	var em *edge.Manager
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		contentStore, err := content.New(e.provisionRoot)
		if err != nil {
			t.Fatal(err)
		}
		em = edge.New(edge.Config{
			Pool: e.pool, Entries: store.New(e.pool).NetworkEntries,
			SnippetDir:  snippet,
			ApplyCmd:    []string{"true"},
			PrivateBind: "192.0.2.1", PublicHost: "203.0.113.1",
			ReleasesRoot: t.TempDir(), SitesRoot: t.TempDir(),
			PortMin: 28110, PortMax: 28130,
		})
		e.mutateConfig = func(cfg *Config) {
			cfg.Edge = em
			cfg.Content = contentStore
		}
	})
	ctx := context.Background()
	_, token, csrf := env.createUserSession("urlowner")
	c := env.clientFor(token, csrf)

	status, _, raw := c.do("POST", "/api/v1/sites", map[string]any{
		"slug": "real-url", "name": "Real URL", "defaultLocale": "zh-CN",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create site = %d %s", status, raw)
	}
	var created siteDTO
	decodeJSONBody(t, raw, &created)
	if created.ProvisionState != "ready" {
		t.Fatalf("provision state = %s failure = %+v", created.ProvisionState, created.ProvisionFailure)
	}

	// The scaffolded site.yaml carries the real private entry.
	siteUUID := mustUUID(created.SiteID)
	entry, err := env.store.NetworkEntries.GetSiteEntry(ctx, siteUUID)
	if err != nil {
		t.Fatalf("entry not allocated at creation: %v", err)
	}
	yamlBytes, err := os.ReadFile(filepath.Join(env.provisionRoot, siteUUID.String(), "config", "site.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "http://192.0.2.1:" + itoa(entry.Port)
	if !strings.Contains(string(yamlBytes), wantURL) {
		t.Fatalf("site.yaml must seed the real entry %s, got:\n%s", wantURL, yamlBytes)
	}
	if strings.Contains(string(yamlBytes), "fuyao.local") {
		t.Fatalf("site.yaml still seeds an invented domain:\n%s", yamlBytes)
	}

	// The config surface reports the effective origin.
	status, _, raw = c.do("GET", "/api/v1/sites/"+created.SiteID+"/site-config", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("site-config = %d %s", status, raw)
	}
	var cfgEnv struct {
		EffectiveOrigin string `json:"effectiveOrigin"`
	}
	decodeJSONBody(t, raw, &cfgEnv)
	if cfgEnv.EffectiveOrigin != wantURL {
		t.Fatalf("effectiveOrigin = %q, want %q", cfgEnv.EffectiveOrigin, wantURL)
	}

	// A visibility switch rewrites the canonical URL to the public host.
	status, _, raw = c.do("PUT", "/api/v1/sites/"+created.SiteID+"/visibility",
		map[string]string{"visibility": "public"}, nil)
	if status != http.StatusOK {
		t.Fatalf("visibility switch = %d %s", status, raw)
	}
	yamlBytes, err = os.ReadFile(filepath.Join(env.provisionRoot, siteUUID.String(), "config", "site.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	wantURL = "http://203.0.113.1:" + itoa(entry.Port)
	if !strings.Contains(string(yamlBytes), wantURL) {
		t.Fatalf("site.yaml must carry the public entry %s after the switch, got:\n%s", wantURL, yamlBytes)
	}
}

// yamlQuoted mirrors the scalar quoting the YAML emitter applies to URLs
// (they contain a colon).
func yamlQuoted(v string) string {
	return " + v + "
}
