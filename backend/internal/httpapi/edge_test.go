package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/edge"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/themecontract"
)

// fakeBuildInRoot stages a succeeded build row whose artifact directory sits
// inside the given builds root (the preview server refuses paths outside it).
func fakeBuildInRoot(t *testing.T, env *testEnv, buildsRoot string, siteID, userID, themeRowID uuid.UUID, themeID, version string, manifestDigest []byte, marker string) uuid.UUID {
	t.Helper()
	buildID := uuid.New()
	artifactDir := filepath.Join(buildsRoot, buildID.String(), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"index.html": "<html>edge:" + marker + "</html>",
		"200.html":   "<html>fallback</html>",
	} {
		if err := os.WriteFile(filepath.Join(artifactDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	digests, _ := json.Marshal(map[string]string{
		"contentDigest":     "sha256:" + strings.Repeat("1", 64),
		"siteConfigDigest":  "sha256:" + strings.Repeat("2", 64),
		"themeConfigDigest": "sha256:" + strings.Repeat("3", 64),
	})
	_, err := env.pool.Exec(context.Background(),
		`INSERT INTO site_builds (id, site_id, requested_by, theme_row_id, theme_id, theme_version,
			manifest_digest, theme_config, site_settings, site_settings_revision, theme_selection_revision,
			status, artifact_path, input_digests, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'{}'::jsonb,'{"site":{"name":"x","description":"","defaultLocale":"zh-CN"}}'::jsonb,1,1,
			'succeeded',$8,$9::jsonb, now())`,
		buildID, siteID, userID, themeRowID, themeID, version, manifestDigest, artifactDir, string(digests))
	if err != nil {
		t.Fatalf("insert fake build: %v", err)
	}
	return buildID
}

// TestPreviewPortsExternalBaseAndClose proves the private preview front door
// semantics: platform-allocated ports, external-base URLs that never leak the
// bind address, working token exchange, and an explicit close that revokes
// tokens, stops the listener, frees the port, and is idempotent.
func TestPreviewPortsExternalBaseAndClose(t *testing.T) {
	buildsRoot := t.TempDir()
	var previews *buildrunner.PreviewServer
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		st := store.New(e.pool)
		previews = buildrunner.NewPreviewServer(buildrunner.PreviewConfig{
			Target:       st.Builds,
			BuildsRoot:   buildsRoot,
			TTL:          time.Hour,
			BindAddr:     "127.0.0.1",
			ExternalBase: "http://192.0.2.10",
			Ports:        st.NetworkEntries,
			PortMin:      21000,
			PortMax:      21010,
		})
		e.mutateConfig = func(cfg *Config) { cfg.Previews = previews }
	})
	ctx := context.Background()
	t.Cleanup(previews.Close)

	owner, token, csrf := env.createUserSession("edgeowner")
	site := createSite(ctx, t, env, "edge-site", "Edge Site", "", "zh-CN", owner.ID)
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	build1 := fakeBuildInRoot(t, env, buildsRoot, site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "one")
	build2 := fakeBuildInRoot(t, env, buildsRoot, site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "two")

	url1, _, err := previews.Issue(site.Site.ID, build1)
	if err != nil {
		t.Fatalf("issue preview 1: %v", err)
	}
	if !strings.HasPrefix(url1, "http://192.0.2.10:21") {
		t.Fatalf("preview URL must use the external base with an allocated port, got %s", url1)
	}
	entry1, err := env.store.NetworkEntries.GetPreviewEntry(ctx, build1)
	if err != nil {
		t.Fatalf("preview entry not persisted: %v", err)
	}
	if entry1.Port < 21000 || entry1.Port > 21010 {
		t.Fatalf("allocated port %d outside the approved range", entry1.Port)
	}

	url2, _, err := previews.Issue(site.Site.ID, build2)
	if err != nil {
		t.Fatalf("issue preview 2: %v", err)
	}
	entry2, _ := env.store.NetworkEntries.GetPreviewEntry(ctx, build2)
	if entry2.Port == entry1.Port {
		t.Fatal("two concurrent previews must not share a port")
	}

	// The bind address (loopback here) is reachable and the token exchange
	// works end-to-end over HTTP.
	bindURL := strings.Replace(url1, "http://192.0.2.10:", "http://127.0.0.1:", 1)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(bindURL)
	if err != nil {
		t.Fatalf("token exchange: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("token exchange status = %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "fuyao_preview_") {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("token exchange did not set the preview cookie")
	}

	// Close build 1 via the API: 204, idempotent, scoped.
	c := env.clientFor(token, csrf)
	status, _, _ := c.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds/"+build1.String()+"/preview-close", map[string]string{}, nil)
	if status != 204 {
		t.Fatalf("preview close status = %d", status)
	}
	status, _, _ = c.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds/"+build1.String()+"/preview-close", map[string]string{}, nil)
	if status != 204 {
		t.Fatalf("second close status = %d, want idempotent 204", status)
	}

	// The closed origin is gone: the token no longer redeems on the bind
	// address (connection refused or 404 — either proves the listener/entry
	// teardown; accept both, assert not 303).
	req, _ := http.NewRequest(http.MethodGet, bindURL, nil)
	served, err := client.Do(req)
	if err == nil {
		served.Body.Close()
		if served.StatusCode == http.StatusSeeOther {
			t.Fatal("closed preview still redeems tokens")
		}
	}
	if _, err := env.store.NetworkEntries.GetPreviewEntry(ctx, build1); err != store.ErrNotFound {
		t.Fatalf("closed preview port not released: %v", err)
	}

	// Build 2's preview is untouched and still serving.
	bindURL2 := strings.Replace(url2, "http://192.0.2.10:", "http://127.0.0.1:", 1)
	resp2, err := client.Get(bindURL2)
	if err != nil {
		t.Fatalf("second preview after closing the first: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("second preview token exchange status = %d", resp2.StatusCode)
	}

	// Reopening build 1 mints a fresh authorization.
	url1b, _, err := previews.Issue(site.Site.ID, build1)
	if err != nil {
		t.Fatalf("reopen preview: %v", err)
	}
	if url1b == url1 {
		t.Fatal("reopened preview must not reuse the revoked token URL")
	}
}

// TestEdgePublishVisibilityAndReconcile covers the published-site edge: the
// current pointer follows publish/rollback, the first publish allocates the
// site's IP:port entry, the managed snippet renders private/public bindings
// from the database, and reconcile repairs a drifted pointer.
func TestEdgePublishVisibilityAndReconcile(t *testing.T) {
	releasesRoot := t.TempDir()
	sitesRoot := t.TempDir()
	snippetDir := t.TempDir()
	var edgeMgr *edge.Manager
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		edgeMgr = edge.New(edge.Config{
			Pool:         e.pool,
			Entries:      store.New(e.pool).NetworkEntries,
			SnippetDir:   snippetDir,
			ApplyCmd:     []string{"true"},
			PrivateBind:  "192.0.2.1",
			PublicBind:   "",
			PublicHost:   "203.0.113.1",
			ReleasesRoot: releasesRoot,
			SitesRoot:    sitesRoot,
			PortMin:      21020,
			PortMax:      21030,
		})
		e.mutateConfig = func(cfg *Config) { cfg.Edge = edgeMgr }
	})
	ctx := context.Background()

	// Frozen release trees block TempDir cleanup; restore write bits first.
	t.Cleanup(func() { _ = buildrunner.MakeTreeWritable(releasesRoot) })

	owner, token, csrf := env.createUserSession("edgepub")
	site := createSite(ctx, t, env, "edge-pub", "Edge Pub", "", "zh-CN", owner.ID)
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	build1 := fakeBuildInRoot(t, env, t.TempDir(), site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "p1")

	contracts, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
	if err != nil {
		t.Fatalf("load contracts: %v", err)
	}
	manager := publish.NewManager(publish.Config{
		Pool:         env.pool,
		Releases:     env.store.Releases,
		Builds:       env.store.Builds,
		Themes:       env.store.Themes,
		ReleasesRoot: releasesRoot,
		Contracts:    contracts,
		Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
		Edge:         edgeMgr,
	})

	release1, err := manager.Publish(ctx, site.Site.ID, build1, owner.ID, []string{"manual"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Current pointer on disk follows the live release.
	siteRoot := filepath.Join(releasesRoot, site.Site.ID.String())
	link, err := os.Readlink(filepath.Join(siteRoot, "current"))
	if err != nil {
		t.Fatalf("current pointer missing: %v", err)
	}
	if link != release1.ReleaseDir {
		t.Fatalf("current pointer = %q, want %q", link, release1.ReleaseDir)
	}

	// The first publish allocated the site's edge entry; the snippet renders
	// the private binding (site defaults to private).
	entry, err := env.store.NetworkEntries.GetSiteEntry(ctx, site.Site.ID)
	if err != nil {
		t.Fatalf("site entry missing: %v", err)
	}
	if entry.Port < 21020 || entry.Port > 21030 {
		t.Fatalf("site port %d outside range", entry.Port)
	}
	snippet, err := os.ReadFile(filepath.Join(snippetDir, "fuyao-admin.caddy"))
	if err != nil {
		t.Fatalf("snippet missing: %v", err)
	}
	if !strings.Contains(string(snippet), "http://192.0.2.1:"+itoa(entry.Port)+" {") {
		t.Fatalf("snippet lacks the private binding:\n%s", snippet)
	}
	if !strings.Contains(string(snippet), filepath.Join(siteRoot, "current")) {
		t.Fatalf("snippet lacks the current root:\n%s", snippet)
	}
	if !strings.Contains(string(snippet), filepath.Join(sitesRoot, site.Site.ID.String(), "content", "albums", "photos")) {
		t.Fatalf("snippet lacks the media albums mapping:\n%s", snippet)
	}

	// The release endpoint reports the externally reachable entry URL.
	c := env.clientFor(token, csrf)
	status, _, raw := c.do("GET", "/api/v1/sites/"+site.Site.ID.String()+"/release", nil, nil)
	if status != 200 {
		t.Fatalf("current release status = %d body = %s", status, raw)
	}
	var releaseResp struct {
		OriginURL string `json:"originUrl"`
	}
	decodeJSONBody(t, raw, &releaseResp)
	if releaseResp.OriginURL != "http://192.0.2.1:"+itoa(entry.Port) {
		t.Fatalf("originUrl = %q", releaseResp.OriginURL)
	}

	// Visibility switch to public re-binds the entry on all interfaces and
	// reports the public host in URLs.
	status, _, raw = c.do("PUT", "/api/v1/sites/"+site.Site.ID.String()+"/visibility", map[string]string{"visibility": "public"}, nil)
	if status != 200 {
		t.Fatalf("visibility switch status = %d body = %s", status, raw)
	}
	snippet, _ = os.ReadFile(filepath.Join(snippetDir, "fuyao-admin.caddy"))
	if !strings.Contains(string(snippet), "http://:"+itoa(entry.Port)+" {") {
		t.Fatalf("public visibility must bind all interfaces:\n%s", snippet)
	}
	_, _, raw = c.do("GET", "/api/v1/sites/"+site.Site.ID.String()+"/release", nil, nil)
	decodeJSONBody(t, raw, &releaseResp)
	if releaseResp.OriginURL != "http://203.0.113.1:"+itoa(entry.Port) {
		t.Fatalf("public originUrl = %q", releaseResp.OriginURL)
	}

	// Reconcile repairs a drifted current pointer (crash between the database
	// switch and the pointer update).
	broken := filepath.Join(siteRoot, "not-a-release")
	if err := os.Symlink("not-a-release", filepath.Join(siteRoot, "current.tmp-x")); err != nil {
		t.Fatal(err)
	}
	_ = os.Rename(filepath.Join(siteRoot, "current.tmp-x"), filepath.Join(siteRoot, "current"))
	if err := edgeMgr.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	link, err = os.Readlink(filepath.Join(siteRoot, "current"))
	if err != nil || link != release1.ReleaseDir {
		t.Fatalf("reconcile did not repair the pointer: %v %q (want %q)", err, link, release1.ReleaseDir)
	}
	_ = broken

	// A second publish moves the pointer to the new release.
	build2 := fakeBuildInRoot(t, env, t.TempDir(), site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "p2")
	if _, err := manager.Publish(ctx, site.Site.ID, build2, owner.ID, []string{"manual"}); err != nil {
		t.Fatalf("second publish: %v", err)
	}
	link, err = os.Readlink(filepath.Join(siteRoot, "current"))
	if err != nil {
		t.Fatalf("current pointer missing after second publish: %v", err)
	}
	if link == release1.ReleaseDir {
		t.Fatal("second publish did not move the current pointer")
	}

	// Rollback re-points at release 1 without a rebuild.
	rolled, err := manager.Rollback(ctx, site.Site.ID, release1.ID, owner.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	link, err = os.Readlink(filepath.Join(siteRoot, "current"))
	if err != nil || link != rolled.ReleaseDir || rolled.ReleaseDir != release1.ReleaseDir {
		t.Fatalf("rollback pointer = %q (want %q)", link, release1.ReleaseDir)
	}
}

// TestEdgeApplyFailureRestoresSnippet proves a failing validate/reload keeps
// the last good snippet on disk and reports the failure instead of silently
// accepting a broken edge.
func TestEdgeApplyFailureRestoresSnippet(t *testing.T) {
	releasesRoot := t.TempDir()
	sitesRoot := t.TempDir()
	snippetDir := t.TempDir()
	var edgeMgr *edge.Manager
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		edgeMgr = edge.New(edge.Config{
			Pool:         e.pool,
			Entries:      store.New(e.pool).NetworkEntries,
			SnippetDir:   snippetDir,
			ApplyCmd:     []string{"false"}, // validate/reload always fails
			PrivateBind:  "192.0.2.1",
			ReleasesRoot: releasesRoot,
			SitesRoot:    sitesRoot,
			PortMin:      21040,
			PortMax:      21050,
		})
	})
	ctx := context.Background()

	owner, _, _ := env.createUserSession("edgefail")
	site := createSite(ctx, t, env, "edge-fail", "Edge Fail", "", "zh-CN", owner.ID)

	// Seed one live release row so the edge has something to render, without
	// going through the manager (whose publish would fail at the edge step).
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	build := fakeBuildInRoot(t, env, t.TempDir(), site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "f1")
	if err := seedLiveRelease(env, site.Site.ID, build, owner.ID, releasesRoot); err != nil {
		t.Fatal(err)
	}

	// First write the good snippet by hand (simulating a previously
	// successful apply), then trigger a re-apply with the failing command.
	good := []byte("# good snippet\n")
	if err := os.WriteFile(filepath.Join(snippetDir, "fuyao-admin.caddy"), good, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := edgeMgr.Reconcile(ctx); err == nil {
		t.Fatal("reconcile with a failing apply command must report an error")
	}
	after, err := os.ReadFile(filepath.Join(snippetDir, "fuyao-admin.caddy"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(good) {
		t.Fatalf("failing apply must restore the previous snippet, got:\n%s", after)
	}
}

// seedLiveRelease activates a release row directly for edge-only tests: the
// release directory exists with its metadata and the site pointer is live.
func seedLiveRelease(env *testEnv, siteID, buildID, userID uuid.UUID, releasesRoot string) error {
	ctx := context.Background()
	releaseDir := buildID.String()
	dir := filepath.Join(releasesRoot, siteID.String(), releaseDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(`{"contract":"fuyao.theme.release/v1"}`), 0o644); err != nil {
		return err
	}
	release := model.Release{
		ID: uuid.New(), SiteID: siteID, BuildID: buildID, ReleaseDir: releaseDir,
		ThemeID: "retry-fixture-theme", ThemeVersion: "1.0.0",
		ManifestDigest: make([]byte, 32), InputDigests: json.RawMessage(`{}`), Reasons: []string{"manual"},
		Status: model.ReleaseStatusLive, CreatedBy: userID,
	}
	tx, err := env.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := env.store.Releases.AcquirePublishLock(ctx, tx, siteID); err != nil {
		return err
	}
	if _, err := env.store.Releases.ActivateWithinTx(ctx, tx, release); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func itoa(v int) string { return strconv.Itoa(v) }
