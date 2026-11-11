package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/themeregistry"
)

// themeVersionFromCatalog reads the first theme's version the way the test
// world registered it (from site/version.json).
func themeVersionFromCatalog(t *testing.T, env *testEnv) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "site", "version.json"))
	if err != nil {
		t.Fatalf("read site version: %v", err)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || v.Version == "" {
		t.Fatalf("decode site version: %v", err)
	}
	return v.Version
}

// registerThemeDir registers an arbitrary (contract-valid) theme root through
// the operator path.
func registerThemeDir(t *testing.T, env *testEnv, dir string) {
	t.Helper()
	if _, _, err := themeregistry.Register(context.Background(), env.store.Themes,
		filepath.Join(repoRoot(t), "contracts", "theme"), dir); err != nil {
		t.Fatalf("register theme dir: %v", err)
	}
}

// lateEnqueuer defers runner wiring until after the handler is constructed;
// handing the handler a typed-nil interface would panic on first Enqueue.
type lateEnqueuer struct {
	mu sync.Mutex
	r  BuildEnqueuer
}

func (l *lateEnqueuer) set(r BuildEnqueuer) {
	l.mu.Lock()
	l.r = r
	l.mu.Unlock()
}

func (l *lateEnqueuer) Enqueue(id uuid.UUID) {
	l.mu.Lock()
	r := l.r
	l.mu.Unlock()
	if r != nil {
		r.Enqueue(id)
	}
}

// latePreviews defers preview-server wiring the same way.
type latePreviews struct {
	mu sync.Mutex
	p  PreviewIssuer
}

func (l *latePreviews) set(p PreviewIssuer) {
	l.mu.Lock()
	l.p = p
	l.mu.Unlock()
}

func (l *latePreviews) Issue(siteID, buildID uuid.UUID) (string, time.Time, error) {
	l.mu.Lock()
	p := l.p
	l.mu.Unlock()
	if p == nil {
		return "", time.Time{}, errors.New("preview server not wired yet")
	}
	return p.Issue(siteID, buildID)
}

// setupBuilds provisions a test world whose handler is wired to a real
// buildrunner.Runner and PreviewServer. BuildsRoot lives inside the
// repository tree so the task's private source copy resolves the repository
// node_modules, and each test gets its own subdirectory under it.
func setupBuilds(t *testing.T) (*testEnv, *buildrunner.Runner, *buildrunner.PreviewServer, string) {
	t.Helper()
	// BuildsRoot deliberately sits OUTSIDE the source tree (a fresh temp
	// directory, on another volume from the repo): dependency resolution is
	// anchored at the registered source via the copy's private node_modules
	// reference and injected bin paths, never at the task root's ancestry.
	buildsRoot := filepath.Join(t.TempDir(), "admin-builds")
	t.Cleanup(func() {
		if t.Failed() {
			// keep the failed task's logs for inspection before unlocking
			// the frozen trees
			_ = os.MkdirAll("/tmp/t3b1-failed-tasks", 0o755)
			_ = buildrunner.MakeTreeWritable(buildsRoot)
			_ = execCopyTree(buildsRoot, "/tmp/t3b1-failed-tasks")
		}
		_ = buildrunner.MakeTreeWritable(buildsRoot)
	})
	return setupBuildsAt(t, buildsRoot)
}

// setupBuildsAt is setupBuilds with an explicit BuildsRoot.
func setupBuildsAt(t *testing.T, buildsRoot string) (*testEnv, *buildrunner.Runner, *buildrunner.PreviewServer, string) {
	t.Helper()
	enq := &lateEnqueuer{}
	prev := &latePreviews{}
	env := setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		env.mutateConfig = func(cfg *Config) {
			cfg.BuildEnqueuer = enq
			cfg.Previews = prev
		}
	})
	runner := buildrunner.NewRunner(buildrunner.Config{
		BuildStore:   env.store.Builds,
		ThemeStore:   env.store.Themes,
		Pool:         env.pool,
		RepoRoot:     repoRoot(t),
		BuildsRoot:   buildsRoot,
		Mode:         "ci",
		Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
	})
	previews := buildrunner.NewPreviewServer(buildrunner.PreviewConfig{Target: env.store.Builds, BuildsRoot: buildsRoot, TTL: time.Hour})
	enq.set(runner)
	prev.set(previews)
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(runner.Stop)
	t.Cleanup(previews.Close)
	// Frozen snapshot/artifact trees are read-only by design; let TempDir
	// cleanup remove them by restoring write bits first.
	t.Cleanup(func() { _ = buildrunner.MakeTreeWritable(buildsRoot) })
	return env, runner, previews, buildsRoot
}

type buildResp struct {
	ID              string           `json:"id"`
	Status          string           `json:"status"`
	Theme           selectedThemeDTO `json:"theme"`
	Failure         *buildFailureDTO `json:"failure"`
	InputDigests    json.RawMessage  `json:"inputDigests"`
	SiteSettings    json.RawMessage  `json:"siteSettings"`
	SourceDigest    *string          `json:"sourceDigest"`
	SourceFileCount *int64           `json:"sourceFileCount"`
}

// TestBuildPreviewClosedLoop drives the real closed loop end to end: an owner
// selects the first theme, triggers a build through the API, the runner runs
// the real Node snapshot + SvelteKit build, the task lands succeeded with the
// input digests recorded, a preview link serves the real artifacts, and
// unauthorized preview access is rejected.
func TestBuildPreviewClosedLoop(t *testing.T) {
	env, _, previews, buildsRoot := setupBuilds(t)
	ctx := context.Background()

	owner, ownerToken, ownerCSRF := env.createUserSession("buildowner")
	site, err := env.store.Sites.Create(ctx, "build-site", "Build Site 标记名", "站点描述", "zh-CN", owner.ID)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	_, outsiderToken, outsiderCSRF := env.createUserSession("buildoutsider")
	viewer, viewerToken, viewerCSRF := env.createUserSession("buildviewer")
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		site.Site.ID, viewer.ID); err != nil {
		t.Fatalf("add viewer: %v", err)
	}
	registerFirstTheme(t, env)

	ownerClient := env.clientFor(ownerToken, ownerCSRF)
	viewerClient := env.clientFor(viewerToken, viewerCSRF)
	outsiderClient := env.clientFor(outsiderToken, outsiderCSRF)

	// Baseline the registered theme workspace before any build runs: the
	// whole point of task workspaces is that these trees never change.
	baseline := workspaceBaseline(t, repoRoot(t))

	// Viewer cannot trigger builds; a site without selection gets a 422.
	if status, _, _ := viewerClient.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds", nil, nil); status != http.StatusForbidden {
		t.Fatalf("viewer create build: status %d, want 403", status)
	}
	if status, _, raw := ownerClient.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds", nil, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("create build without selection: status %d body %s, want 422", status, raw)
	}

	// Select the theme (revision t0), then create the build.
	if status, _, raw := ownerClient.do("PUT", "/api/v1/sites/"+site.Site.ID.String()+"/theme",
		themePutBody("fuyao-homepage", themeVersionFromCatalog(t, env), ""), map[string]string{"If-Match": fmt.Sprintf(`"%s-t0"`, site.Site.ID)}); status != http.StatusOK {
		t.Fatalf("select theme: status %d body %s", status, raw)
	}
	status, _, raw := ownerClient.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("create build: status %d body %s", status, raw)
	}
	var created buildResp
	decodeJSONBody(t, raw, &created)
	if created.Status != "queued" || created.Theme.ThemeID != "fuyao-homepage" {
		t.Fatalf("unexpected created task: %+v", created)
	}
	buildPath := "/api/v1/sites/" + site.Site.ID.String() + "/builds/" + created.ID

	// Outsiders cannot see the task; members can re-query state.
	if status, _, _ := outsiderClient.do("GET", buildPath, nil, nil); status != http.StatusNotFound {
		t.Fatalf("outsider build visibility: status %d, want 404", status)
	}

	deadline := time.Now().Add(8 * time.Minute)
	var final buildResp
	for time.Now().Before(deadline) {
		status, _, raw = ownerClient.do("GET", buildPath, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("get build: status %d body %s", status, raw)
		}
		decodeJSONBody(t, raw, &final)
		if final.Status == "succeeded" || final.Status == "failed" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if final.Status != "succeeded" {
		t.Fatalf("build did not succeed: %+v", final.Failure)
	}
	if len(final.InputDigests) == 0 {
		t.Fatal("succeeded build missing input digests")
	}

	// The frozen snapshot carries the DB-saved site settings (name marker).
	snapshotSettings, err := os.ReadFile(filepath.Join(buildsRoot, created.ID, "snapshot", "private", "site-config.json"))
	if err != nil || !strings.Contains(string(snapshotSettings), "Build Site 标记名") {
		t.Fatalf("snapshot site-config does not carry the DB site name: err=%v", err)
	}
	// The snapshot input digests equal the DB-recorded digests.
	inputJSON, err := os.ReadFile(filepath.Join(buildsRoot, created.ID, "snapshot", "private", "input.json"))
	if err != nil {
		t.Fatalf("read input.json: %v", err)
	}
	var inputDoc struct {
		Inputs json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal(inputJSON, &inputDoc); err != nil {
		t.Fatalf("decode input.json: %v", err)
	}
	if !jsonEqualValueTest(inputDoc.Inputs, final.InputDigests) {
		t.Fatalf("digest mismatch: snapshot %s vs task %s", inputDoc.Inputs, final.InputDigests)
	}
	// Artifacts exist and are frozen read-only.
	info, err := os.Stat(filepath.Join(buildsRoot, created.ID, "artifacts"))
	if err != nil || !info.IsDir() {
		t.Fatalf("artifacts dir missing: %v", err)
	}
	if index, err := os.Stat(filepath.Join(buildsRoot, created.ID, "artifacts", "index.html")); err != nil || index.Mode().Perm()&0o222 != 0 {
		t.Fatalf("index.html missing or writable: %v %v", err, index)
	}
	// The built page really uses the snapshot's DB site name.
	pageHTML, err := os.ReadFile(filepath.Join(buildsRoot, created.ID, "artifacts", "index.html"))
	if err != nil || !strings.Contains(string(pageHTML), "Build Site 标记名") {
		t.Fatalf("built index.html does not carry the DB site name (err=%v)", err)
	}
	// Source content untouched: the fixtures content tree still has no marker.
	fixtures, err := os.ReadFile(filepath.Join(repoRoot(t), "site", "fixtures", "content", "posts", "hello-world.md"))
	if err == nil && strings.Contains(string(fixtures), "Build Site 标记名") {
		t.Fatal("source content was rewritten")
	}

	// Preview links: any member may issue; the URL points at the build's own
	// loopback origin and the entry path exchanges the token for a cookie.
	if status, _, _ := viewerClient.do("POST", buildPath+"/preview-links", nil, nil); status != http.StatusCreated {
		t.Fatalf("viewer preview link: status %d, want 201", status)
	}
	status, _, raw = ownerClient.do("POST", buildPath+"/preview-links", nil, nil)
	if status != http.StatusCreated {
		t.Fatalf("owner preview link: status %d body %s", status, raw)
	}
	var link struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expiresAt"`
	}
	decodeJSONBody(t, raw, &link)
	if !strings.Contains(link.URL, "/__preview__/") {
		t.Fatalf("preview URL is not a token entry URL: %s", link.URL)
	}
	originBase := link.URL[:strings.Index(link.URL, "/__preview__/")]

	// Open the issued URL exactly as a browser would: the entry swaps the
	// Redeem the entry URL exactly as a browser would: one shared cookie
	// jar and default redirect following store the origin's uniquely named
	// authorization cookie (HttpOnly, Lax) — never a manually re-attached
	// fixed-name cookie. The entry response attributes are asserted first.
	noFollow := &http.Client{Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	entryResp, err := noFollow.Get(link.URL)
	if err != nil || entryResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("preview entry: err=%v status=%v", err, entryResp)
	}
	var setCookie *http.Cookie
	for _, c := range entryResp.Cookies() {
		if strings.HasPrefix(c.Name, "fuyao_preview_") {
			setCookie = c
		}
	}
	entryResp.Body.Close()
	if setCookie == nil || !setCookie.HttpOnly || setCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("preview entry cookie must be uniquely named, HttpOnly, and Lax: %+v", setCookie)
	}

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Timeout: 15 * time.Second, Jar: jar}
	rootResp, err := browser.Get(link.URL)
	if err != nil || rootResp.StatusCode != http.StatusOK {
		t.Fatalf("preview open: err=%v status=%v", err, rootResp)
	}
	pageBytes := readAll(t, rootResp)
	if !strings.Contains(pageBytes, "Build Site 标记名") {
		t.Fatalf("preview page does not carry the DB site name")
	}
	if csp := rootResp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("preview CSP must allow same-origin data fetches: %s", csp)
	}

	// Root-absolute assets referenced by the page resolve on the origin.
	asset := extractRootAsset(t, pageBytes)
	if asset != "" {
		assetResp, err := browser.Get(originBase + asset)
		if err != nil || assetResp.StatusCode != http.StatusOK {
			t.Fatalf("root-absolute asset %s: err=%v status=%d", asset, err, assetResp.StatusCode)
		}
		assetResp.Body.Close()
	}
	// Deep prerendered page and data URL conventions serve real content;
	// a client-only route falls back to the declared SPA fallback file.
	blogResp, err := browser.Get(originBase + "/blog/")
	if err != nil {
		t.Fatalf("deep blog page fetch: %v", err)
	}
	blogBody := readAll(t, blogResp)
	if blogResp.StatusCode != http.StatusOK || !strings.Contains(blogBody, "博客") {
		t.Fatalf("deep blog page missing on preview origin: status %v", blogResp.StatusCode)
	}
	dataResp, err := browser.Get(originBase + "/posts/all.json")
	if err != nil || dataResp.StatusCode != http.StatusOK {
		t.Fatalf("posts/all.json (posts URL convention) missing: err=%v status=%v", err, dataResp)
	}
	dataResp.Body.Close()
	clientRoute, err := browser.Get(originBase + "/some/client-only/route")
	if err != nil || clientRoute.StatusCode != http.StatusOK {
		t.Fatalf("client-only route must hit the SPA fallback: err=%v status=%v", err, clientRoute)
	}
	readAll(t, clientRoute)

	// Second build with a changed site name: its preview origin must serve
	// its own artifacts, never the first build's.
	if _, err := env.store.Sites.UpdateSettings(ctx, site.Site.ID, site.Settings.Revision,
		strPtr("Build Site 第二名"), nil, nil); err != nil {
		t.Fatalf("update settings for second build: %v", err)
	}
	status, _, raw = ownerClient.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("second build: status %d body %s", status, raw)
	}
	var second buildResp
	decodeJSONBody(t, raw, &second)
	deadline2 := time.Now().Add(8 * time.Minute)
	var final2 buildResp
	for time.Now().Before(deadline2) {
		status, _, raw = ownerClient.do("GET", "/api/v1/sites/"+site.Site.ID.String()+"/builds/"+second.ID, nil, nil)
		decodeJSONBody(t, raw, &final2)
		if final2.Status == "succeeded" || final2.Status == "failed" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if final2.Status != "succeeded" {
		t.Fatalf("second build did not succeed: %+v", final2.Failure)
	}
	status, _, raw = ownerClient.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds/"+second.ID+"/preview-links", nil, nil)
	if status != http.StatusCreated {
		t.Fatalf("second preview link: status %d body %s", status, raw)
	}
	var link2 struct {
		URL string `json:"url"`
	}
	decodeJSONBody(t, raw, &link2)
	origin2 := link2.URL[:strings.Index(link2.URL, "/__preview__/")]
	// Redeem the second preview through the same browser client: its
	// uniquely named cookie joins the shared jar without disturbing the
	// first preview's authorization.
	secondPage, err := browser.Get(link2.URL)
	if err != nil || secondPage.StatusCode != http.StatusOK {
		t.Fatalf("second preview open: err=%v status=%v", err, secondPage)
	}
	if page2 := readAll(t, secondPage); !strings.Contains(page2, "Build Site 第二名") {
		t.Fatalf("second preview shows the wrong build content")
	}
	// The first origin still serves the first build through the same client
	// (no cross-contamination, no overwritten cookie).
	firstAgain, err := browser.Get(originBase + "/")
	if err != nil {
		t.Fatalf("first preview re-visit: %v", err)
	}
	if page1 := readAll(t, firstAgain); !strings.Contains(page1, "Build Site 标记名") || strings.Contains(page1, "Build Site 第二名") {
		t.Fatalf("first preview origin was contaminated by the second build")
	}
	// The first build's entry token is invalid on the second origin.
	firstToken := link.URL[strings.LastIndex(link.URL, "/")+1:]
	crossResp, err := browser.Get(origin2 + "/__preview__/" + firstToken)
	if err != nil || crossResp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-origin token must be rejected: err=%v status=%v", err, crossResp)
	}
	crossResp.Body.Close()

	// Without any cookie nothing is served on any origin: a separate
	// cookie-less client (no jar) gets 404 everywhere.
	bare := &http.Client{Timeout: 15 * time.Second}
	for _, origin := range []string{originBase, origin2} {
		anonResp, err := bare.Get(origin + "/")
		if err != nil || anonResp.StatusCode != http.StatusNotFound {
			t.Fatalf("anonymous preview root %s: err=%v status=%v", origin, err, anonResp)
		}
		anonResp.Body.Close()
	}

	// The registered source and its shared working trees are byte-identical
	// to the pre-build baseline: tasks wrote only inside their own
	// directories.
	assertWorkspaceUnchanged(t, repoRoot(t), baseline)

	// The task recorded its full source-copy identity, broader than the
	// theme.json manifest digest.
	if final.SourceDigest == nil || final.SourceFileCount == nil || *final.SourceFileCount < 10 {
		t.Fatalf("task missing source-copy identity: %+v", final.SourceDigest)
	}
	_ = previews
}

// workspaceBaseline digests the theme's shared trees that tasks must never
// touch: sources, configs, the fresh snapshot staging area, the build
// output, and the build context.
func workspaceBaseline(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range []string{
		"theme.json", "package.json", "src", "scripts", "config", "static", "fixtures",
		filepath.Join(".fuyao", "build-context.json"),
		filepath.Join(".fuyao", "build-inputs", "current"),
		"build",
	} {
		path := filepath.Join(root, "site", rel)
		if _, err := os.Stat(path); err != nil {
			out[rel] = "absent"
			continue
		}
		digest, err := buildrunner.TreeDigest(path)
		if err != nil {
			t.Fatalf("digest baseline %s: %v", rel, err)
		}
		out[rel] = digest
	}
	return out
}

// assertWorkspaceUnchanged re-digests the baselined trees and requires exact
// equality.
func assertWorkspaceUnchanged(t *testing.T, root string, baseline map[string]string) {
	t.Helper()
	fresh := workspaceBaseline(t, root)
	for rel, want := range baseline {
		if got := fresh[rel]; got != want {
			t.Fatalf("theme workspace tree %q changed across builds", rel)
		}
	}
}

// readAll drains and closes a response body.
func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(data)
}

// extractRootAsset pulls the first root-absolute asset URL out of the page.
func extractRootAsset(t *testing.T, page string) string {
	t.Helper()
	for _, prefix := range []string{"src=\"/", "href=\"/"} {
		if idx := strings.Index(page, prefix); idx >= 0 {
			rest := page[idx+len(prefix):]
			if end := strings.IndexByte(rest, '"'); end > 0 {
				return "/" + rest[:end]
			}
		}
	}
	return ""
}

func strPtr(v string) *string { return &v }

// jsonEqualValueTest compares two JSON documents by parsed value; the task
// row stores them through jsonb which re-serializes (indentation, key
// order) without changing the digests themselves.
func jsonEqualValueTest(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(ab) == string(bb)
}

// TestBuildCapabilityBlock verifies a theme missing the site's required
// capabilities is blocked with an explicit, persisted failure.
func TestBuildCapabilityBlock(t *testing.T) {
	env, _, _, _ := setupBuilds(t)
	ctx := context.Background()

	owner, ownerToken, ownerCSRF := env.createUserSession("capowner")
	site, err := env.store.Sites.Create(ctx, "cap-site", "Cap Site", "", "zh-CN", owner.ID)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	// A fake theme root whose manifest lacks every capability: register it
	// through the operator path so the catalog row is contract-valid.
	fakeRoot := t.TempDir()
	manifest := `{"contract":"fuyao.theme.manifest/v1","id":"minimal-theme","version":"1.0.0","name":"Minimal","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["favicon"],"outputs":{"spaFallback":"200.html","urlConventions":["data"],"requiredArtifacts":[]}}`
	if err := os.WriteFile(filepath.Join(fakeRoot, "theme.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	registerThemeDir(t, env, fakeRoot)

	ownerClient := env.clientFor(ownerToken, ownerCSRF)
	if status, _, raw := ownerClient.do("PUT", "/api/v1/sites/"+site.Site.ID.String()+"/theme",
		themePutBody("minimal-theme", "1.0.0", ""), map[string]string{"If-Match": fmt.Sprintf(`"%s-t0"`, site.Site.ID)}); status != http.StatusOK {
		t.Fatalf("select minimal theme: status %d body %s", status, raw)
	}
	status, _, raw := ownerClient.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/builds", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("create build: status %d body %s", status, raw)
	}
	var created buildResp
	decodeJSONBody(t, raw, &created)

	deadline := time.Now().Add(30 * time.Second)
	var final buildResp
	for time.Now().Before(deadline) {
		status, _, raw = ownerClient.do("GET", "/api/v1/sites/"+site.Site.ID.String()+"/builds/"+created.ID, nil, nil)
		decodeJSONBody(t, raw, &final)
		if final.Status == "succeeded" || final.Status == "failed" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if final.Status != "failed" || final.Failure == nil || final.Failure.Stage != "capability-check" {
		t.Fatalf("capability gap not blocked: %+v", final.Failure)
	}
	if len(final.Failure.MissingCapabilities) == 0 || len(final.Failure.MissingURLConventions) == 0 {
		t.Fatalf("missing items not reported: %+v", final.Failure)
	}
}

// TestBuildRecoveryOnRestart verifies the executor-owned recovery contract:
// a row stuck in running from a dead process is marked failed with an
// interrupted record when the next executor starts; queued rows survive and
// re-enqueue.
func TestBuildRecoveryOnRestart(t *testing.T) {
	env, runner, _, _ := setupBuilds(t)
	ctx := context.Background()
	owner, ownerToken, _ := env.createUserSession("recowner")
	site, err := env.store.Sites.Create(ctx, "rec-site", "Rec Site", "", "zh-CN", owner.ID)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	registerFirstTheme(t, env)
	var rowID uuid.UUID
	if err := env.pool.QueryRow(ctx, `SELECT id FROM themes WHERE theme_id='fuyao-homepage'`).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.Themes.SaveSelection(ctx, site.Site.ID, rowID, owner.ID, []byte("{}"), 0); err != nil {
		t.Fatal(err)
	}
	build, err := env.store.Builds.CreateFromSiteSnapshot(ctx, site.Site.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a dead executor: the row is running but nothing will finish it.
	if _, err := env.pool.Exec(ctx,
		`UPDATE site_builds SET status='running', started_at=now() WHERE id=$1`, build.ID); err != nil {
		t.Fatal(err)
	}
	// A fresh runner generation (as after a restart) recovers it.
	if _, err := env.pool.Exec(ctx, `SELECT pg_advisory_unlock($1)`, 0x46594c42); err != nil {
		t.Fatal(err)
	}
	runner.Stop()
	recov := buildrunner.NewRunner(runnerConfigFor(env))
	if err := recov.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recov.Stop)

	client := env.clientFor(ownerToken, "")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status, _, raw := client.do("GET", "/api/v1/sites/"+site.Site.ID.String()+"/builds/"+build.ID.String(), nil, nil)
		if status == http.StatusOK {
			var got buildResp
			decodeJSONBody(t, raw, &got)
			if got.Status == "failed" {
				if got.Failure == nil || got.Failure.Stage != "interrupted" {
					t.Fatalf("unexpected failure record: %+v", got.Failure)
				}
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("interrupted running row was not recovered as failed")
}

// runnerConfigFor mirrors the wiring setupBuilds uses, for a second runner
// generation in the same test world.
func runnerConfigFor(env *testEnv) buildrunner.Config {
	return buildrunner.Config{
		BuildStore:   env.store.Builds,
		ThemeStore:   env.store.Themes,
		Pool:         env.pool,
		RepoRoot:     repoRoot(env.t),
		BuildsRoot:   filepath.Join(tTempDir(env.t), "admin-builds"),
		Mode:         "ci",
		Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
	}
}

func tTempDir(t *testing.T) string { return t.TempDir() }

// execCopyTree is a test-only recursive copy (cp -R semantics).
func execCopyTree(src, dst string) error {
	out, err := exec.Command("cp", "-R", src, dst).CombinedOutput()
	_ = out
	return err
}
