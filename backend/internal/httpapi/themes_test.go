package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themeregistry"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/wa"
)

type themeListResp struct {
	Themes []themeDTO `json:"themes"`
}

type siteThemeResp struct {
	SiteID      string            `json:"siteId"`
	Selected    bool              `json:"selected"`
	Theme       *selectedThemeDTO `json:"theme"`
	ThemeConfig json.RawMessage   `json:"themeConfig"`
	Revision    int64             `json:"revision"`
}

// registerFirstTheme registers the repository's contract-valid fixture theme
// into the test world through the operator registration path. Real-theme
// (fuyao-homepage) registration happens through registerThemeDir with
// FUYAO_TEST_THEME_ROOT in the build tests.
func registerFirstTheme(t *testing.T, env *testEnv) {
	t.Helper()
	_, created, err := themeregistry.Register(context.Background(), env.store.Themes,
		filepath.Join(repoRoot(t), "contracts", "theme"), fixtureThemeRoot(t))
	if err != nil {
		t.Fatalf("register first theme: %v", err)
	}
	if !created {
		t.Fatal("expected the first registration to create the catalog row")
	}
}

// themePutBody builds a PUT body. config "" omits themeConfig (the {} default
// path); "null" sends an explicit JSON null; anything else is embedded as raw
// JSON.
func themePutBody(themeID, version string, config string) map[string]any {
	if config == "" {
		return map[string]any{"themeId": themeID, "version": version}
	}
	if config == "null" {
		return map[string]any{"themeId": themeID, "version": version, "themeConfig": nil}
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(config), &raw); err != nil {
		panic(err)
	}
	return map[string]any{"themeId": themeID, "version": version, "themeConfig": raw}
}

// TestThemeCatalogAndSelectionLifecycle covers the full T3-a surface: catalog
// read, per-site selection persistence, schema-checked configuration,
// role/tenant isolation, and the independent ETag counter.
func TestThemeCatalogAndSelectionLifecycle(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	owner, ownerToken, ownerCSRF := env.createUserSession("themeowner")
	site := createSite(ctx, t, env, "theme-site", "Theme Site", "", "zh-CN", owner.ID)
	viewer, viewerToken, viewerCSRF := env.createUserSession("themeviewer")
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		site.Site.ID, viewer.ID); err != nil {
		t.Fatalf("add viewer: %v", err)
	}
	_, outsiderToken, outsiderCSRF := env.createUserSession("themeoutsider")

	ownerClient := env.clientFor(ownerToken, ownerCSRF)
	viewerClient := env.clientFor(viewerToken, viewerCSRF)
	outsiderClient := env.clientFor(outsiderToken, outsiderCSRF)

	// Unauthenticated catalog access is rejected.
	c := env.newClient()
	if status, _, _ := c.do("GET", "/api/v1/themes", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated themes: status %d, want 401", status)
	}

	// Catalog is empty before registration.
	status, _, raw := ownerClient.do("GET", "/api/v1/themes", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list themes: status %d body %s", status, raw)
	}
	var list themeListResp
	decodeJSONBody(t, raw, &list)
	if len(list.Themes) != 0 {
		t.Fatalf("expected empty catalog, got %d", len(list.Themes))
	}

	registerFirstTheme(t, env)
	version := themeVersionFromCatalog(t, env)

	// The catalog exposes manifest content without server paths.
	status, _, raw = viewerClient.do("GET", "/api/v1/themes", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("viewer list themes: status %d body %s", status, raw)
	}
	decodeJSONBody(t, raw, &list)
	if len(list.Themes) != 1 {
		t.Fatalf("expected one theme, got %d", len(list.Themes))
	}
	entry := list.Themes[0]
	if entry.ThemeID != fixtureThemeID || entry.Adapter != "node22-sveltekit-static" {
		t.Fatalf("unexpected entry %+v", entry)
	}
	if entry.ManifestDigest == "" || entry.ConfigSchema == nil {
		t.Fatal("catalog entry must carry the manifest digest and config schema")
	}
	if strings.Contains(string(raw), "sourcePath") || strings.Contains(string(raw), repoRoot(t)) {
		t.Fatal("catalog response leaks server paths")
	}

	themePath := "/api/v1/sites/" + site.Site.ID.String() + "/theme"

	// Unselected selection reads as t0.
	status, hdr, raw := ownerClient.do("GET", themePath, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get theme: status %d body %s", status, raw)
	}
	var sel siteThemeResp
	decodeJSONBody(t, raw, &sel)
	if sel.Selected || sel.Theme != nil || sel.Revision != 0 {
		t.Fatalf("expected unselected t0, got %+v", sel)
	}
	if etag := hdr.Get("ETag"); etag != themeETag(site.Site.ID.String(), 0) {
		t.Fatalf("unselected ETag %q, want t0", etag)
	}
	t0 := hdr.Get("ETag")

	// PUT requires CSRF, If-Match, and a known theme with a valid config.
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"X-CSRF-Token": "wrong"}); status != http.StatusForbidden {
		t.Fatalf("bad csrf: status %d body %s, want 403", status, raw)
	}
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"If-Match": ""}); status != http.StatusPreconditionRequired {
		t.Fatalf("missing if-match: status %d body %s, want 428", status, raw)
	}
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"If-Match": "*"}); status != http.StatusBadRequest {
		t.Fatalf("wildcard if-match: status %d body %s, want 400", status, raw)
	}
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, "99.0.0", "{}"), map[string]string{"If-Match": t0}); status != http.StatusNotFound {
		t.Fatalf("unknown version: status %d body %s, want 404", status, raw)
	}
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody("other-theme", version, "{}"), map[string]string{"If-Match": t0}); status != http.StatusNotFound {
		t.Fatalf("unknown theme: status %d body %s, want 404", status, raw)
	}
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, "27.1", "{}"), map[string]string{"If-Match": t0}); status != http.StatusBadRequest {
		t.Fatalf("malformed version: status %d body %s, want 400", status, raw)
	}
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody("Fuyao Homepage", version, "{}"), map[string]string{"If-Match": t0}); status != http.StatusBadRequest {
		t.Fatalf("malformed themeId: status %d body %s, want 400", status, raw)
	}
	// The first theme's config schema only accepts {}.
	status, _, raw = ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, `{"unknown":true}`), map[string]string{"If-Match": t0})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid config: status %d body %s, want 422", status, raw)
	}
	// The 422 detail is a fixed sentence, never validator internals that can
	// embed local schema resource paths.
	var problem struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	decodeJSONBody(t, raw, &problem)
	if problem.Code != "invalid_theme_config" || strings.Contains(problem.Detail, "file:") || strings.Contains(problem.Detail, "/") {
		t.Fatalf("422 detail must be path-free, got code=%q detail=%q", problem.Code, problem.Detail)
	}

	// An explicit null themeConfig is a request error, not the {} default.
	if status, _, raw := ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "null"), map[string]string{"If-Match": t0}); status != http.StatusBadRequest {
		t.Fatalf("explicit null themeConfig: status %d body %s, want 400", status, raw)
	}

	// Happy path: select with the t0 precondition.
	status, hdr, raw = ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"If-Match": t0})
	if status != http.StatusOK {
		t.Fatalf("put theme: status %d body %s", status, raw)
	}
	decodeJSONBody(t, raw, &sel)
	if !sel.Selected || sel.Theme == nil || sel.Theme.ThemeID != fixtureThemeID || sel.Theme.Version != version || sel.Revision != 1 {
		t.Fatalf("unexpected selection %+v", sel)
	}
	if sel.Theme.ManifestDigest != entry.ManifestDigest {
		t.Fatal("selection digest must match the catalog entry")
	}
	t1 := hdr.Get("ETag")
	if t1 != themeETag(site.Site.ID.String(), 1) {
		t.Fatalf("saved ETag %q, want t1", t1)
	}

	// The t0 precondition is now stale and yields 412 with the current ETag.
	if status, hdr, raw = ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"If-Match": t0}); status != http.StatusPreconditionFailed {
		t.Fatalf("stale if-match: status %d body %s, want 412", status, raw)
	}
	if hdr.Get("ETag") != t1 {
		t.Fatalf("conflict must return the current ETag, got %q", hdr.Get("ETag"))
	}

	// The selection persists through a fresh store over the same database.
	fresh := store.New(env.pool)
	persisted, err := fresh.Themes.GetSelectionForUser(ctx, site.Site.ID, owner.ID)
	if err != nil || persisted.Selection == nil || persisted.Selection.Revision != 1 {
		t.Fatalf("selection not durable: %+v err %v", persisted.Selection, err)
	}

	// Viewer reads but cannot write; outsider gets no site at all.
	if status, _, raw = viewerClient.do("GET", themePath, nil, nil); status != http.StatusOK {
		t.Fatalf("viewer get theme: status %d body %s", status, raw)
	}
	if status, _, raw = viewerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"If-Match": t1}); status != http.StatusForbidden {
		t.Fatalf("viewer put theme: status %d body %s, want 403", status, raw)
	}
	if status, _, raw = outsiderClient.do("GET", themePath, nil, nil); status != http.StatusNotFound {
		t.Fatalf("outsider get theme: status %d body %s, want 404", status, raw)
	}
	if status, _, raw = outsiderClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"If-Match": t1}); status != http.StatusNotFound {
		t.Fatalf("outsider put theme: status %d body %s, want 404", status, raw)
	}

	// Settings and theme selection keep independent revisions.
	settingsPath := "/api/v1/sites/" + site.Site.ID.String() + "/settings"
	status, hdr, raw = ownerClient.do("GET", settingsPath, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get settings: status %d", status)
	}
	var settings struct {
		Revision int64 `json:"revision"`
	}
	decodeJSONBody(t, raw, &settings)
	settingsRev := settings.Revision
	settingsETag := hdr.Get("ETag")

	if status, hdr, raw = ownerClient.do("PUT", themePath, themePutBody(fixtureThemeID, version, "{}"), map[string]string{"If-Match": t1}); status != http.StatusOK {
		t.Fatalf("update theme selection: status %d body %s", status, raw)
	}
	decodeJSONBody(t, raw, &sel)
	if sel.Revision != 2 {
		t.Fatalf("theme revision after update %d, want 2", sel.Revision)
	}
	status, _, raw = ownerClient.do("GET", settingsPath, nil, nil)
	decodeJSONBody(t, raw, &settings)
	if settings.Revision != settingsRev {
		t.Fatalf("theme save must not bump settings revision: %d -> %d", settingsRev, settings.Revision)
	}

	patch := map[string]any{"name": "Renamed Site"}
	if status, _, raw = ownerClient.do("PATCH", settingsPath, patch, map[string]string{"If-Match": settingsETag}); status != http.StatusOK {
		t.Fatalf("patch settings: status %d body %s", status, raw)
	}
	status, hdr, raw = ownerClient.do("GET", themePath, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get theme after settings patch: %d", status)
	}
	decodeJSONBody(t, raw, &sel)
	if sel.Revision != 2 {
		t.Fatalf("settings patch must not bump theme revision: %d", sel.Revision)
	}
}

// TestThemeRegistrationIdempotency covers the store registration semantics:
// the full registration identity (manifest digest plus configuration schema,
// including declared/omitted) governs idempotency; any change under the same
// version is rejected.
func TestThemeRegistrationIdempotency(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	version := themeVersionFromCatalog(t, env)

	root := fixtureThemeRoot(t)
	contracts := filepath.Join(repoRoot(t), "contracts", "theme")

	if _, created, err := themeregistry.Register(ctx, env.store.Themes, contracts, root); err != nil || !created {
		t.Fatalf("first register: created=%v err=%v", created, err)
	}
	if _, created, err := themeregistry.Register(ctx, env.store.Themes, contracts, root); err != nil || created {
		t.Fatalf("identical re-register: created=%v err=%v", created, err)
	}

	manifestRaw, err := os.ReadFile(filepath.Join(root, "theme.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Same identity, different manifest bytes: rejected.
	changedManifest, err := editJSON(manifestRaw, "name", "Changed display name")
	if err != nil {
		t.Fatal(err)
	}
	clone := t.TempDir()
	if err := os.WriteFile(filepath.Join(clone, "theme.json"), changedManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "theme.config.schema.json"),
		[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := themeregistry.Register(ctx, env.store.Themes, contracts, clone); err != store.ErrConflict {
		t.Fatalf("changed manifest under same version: err=%v, want ErrConflict", err)
	}

	// Identical manifest but a changed configuration schema document: also a
	// conflict — silently keeping the old schema would let the management side
	// validate against a definition the theme itself no longer ships.
	schemaChanged := t.TempDir()
	if err := os.WriteFile(filepath.Join(schemaChanged, "theme.json"), manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(schemaChanged, "theme.config.schema.json"),
		[]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"properties":{"accent":{"type":"string"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := themeregistry.Register(ctx, env.store.Themes, contracts, schemaChanged); err != store.ErrConflict {
		t.Fatalf("changed config schema under same version: err=%v, want ErrConflict", err)
	}

	// The catalog still holds the original definition.
	kept, err := env.store.Themes.Get(ctx, fixtureThemeID, version)
	if err != nil {
		t.Fatal(err)
	}
	if kept.ConfigSchema == nil || bytes.Contains(kept.ConfigSchema, []byte("accent")) {
		t.Fatal("rejected re-registration must not alter the stored definition")
	}
}

// TestConcurrentSelectionCreation proves two racing first-time saves admit
// exactly one winner; the loser gets a conflict, not a lost update.
func TestConcurrentSelectionCreation(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	owner, _, _ := env.createUserSession("racesel")
	site := createSite(ctx, t, env, "race-site", "Race", "", "zh-CN", owner.ID)
	theme, created, err := themeregistry.Register(ctx, env.store.Themes,
		filepath.Join(repoRoot(t), "contracts", "theme"), fixtureThemeRoot(t))
	if err != nil || !created {
		t.Fatalf("register: %v", err)
	}

	const writers = 4
	results := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func() {
			_, err := env.store.Themes.SaveSelection(ctx, site.Site.ID, theme.ID, owner.ID, []byte("{}"), 0)
			results <- err
		}()
	}
	var conflicts, successes int
	for i := 0; i < writers; i++ {
		if err := <-results; err == nil {
			successes++
		} else if errors.Is(err, store.ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected save error: %v", err)
		}
	}
	if successes != 1 || conflicts != writers-1 {
		t.Fatalf("want exactly 1 success and %d conflicts, got %d/%d", writers-1, successes, conflicts)
	}
	rec, err := env.store.Themes.GetSelectionForUser(ctx, site.Site.ID, owner.ID)
	if err != nil || rec.Selection == nil || rec.Selection.Revision != 1 {
		t.Fatalf("selection after race: %+v err=%v", rec.Selection, err)
	}
}

// editJSON rewrites one top-level string field of a JSON document.
func editJSON(raw []byte, field, value string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	doc[field] = value
	return json.Marshal(doc)
}

// TestThemeSelectionWithoutContracts proves the server reports an explicit
// 503 instead of silently skipping configuration validation when the theme
// contracts are not configured.
func TestThemeSelectionWithoutContracts(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	registerFirstTheme(t, env)
	version := themeVersionFromCatalog(t, env)
	owner, token, csrf := env.createUserSession("nocontract")
	site := createSite(ctx, t, env, "nocontract-site", "Site", "", "zh-CN", owner.ID)

	// A second handler over the same store without the contract validator.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://localhost:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	webAuthn, err := wa.New(wa.Params{
		RPID:          "localhost",
		RPDisplayName: "Fuyao Admin Test",
		RPOrigins:     []string{origin},
		ChallengeTTL:  5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	bare := New(env.store, webAuthn, Config{
		RPID:       "localhost",
		RPOrigins:  []string{origin},
		SessionTTL: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := &http.Server{Handler: bare, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	env2 := &testEnv{t: t, base: origin}
	client := env2.clientFor(token, csrf)
	client.env = env2

	status, _, raw := client.do("PUT",
		"/api/v1/sites/"+site.Site.ID.String()+"/theme",
		themePutBody(fixtureThemeID, version, "{}"),
		map[string]string{"If-Match": themeETag(site.Site.ID.String(), 0)})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("put without contracts: status %d body %s, want 503", status, raw)
	}
	var p problemResp
	decodeJSONBody(t, raw, &p)
	if p.Code != "theme_contracts_unavailable" {
		t.Fatalf("problem code %q, want theme_contracts_unavailable", p.Code)
	}
}
