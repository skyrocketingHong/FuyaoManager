package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
)

// setupPublish is the full world for release tests: build runner plus the
// publish pipeline (manager and published origins) wired into the API. Both
// roots live in temp directories outside the repository.
func setupPublish(t *testing.T) (*testEnv, string, string) {
	t.Helper()
	return setupPublishWithContract(t, nil)
}

// setupPublishWithContract is setupPublish with an injectable release
// contract (nil loads the real one); the retry regression uses a flaky
// wrapper to inject exactly one publish failure.
func setupPublishWithContract(t *testing.T, contract publish.ReleaseContract) (*testEnv, string, string) {
	t.Helper()
	buildsRoot := filepath.Join(t.TempDir(), "admin-builds")
	releasesRoot := filepath.Join(t.TempDir(), "admin-releases")
	var server *publish.Server
	env, _, _, _ := setupBuildsWith(t, buildsRoot, func(cfg *Config, env *testEnv) {
		if contract == nil {
			set, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
			if err != nil {
				t.Fatalf("load theme contracts: %v", err)
			}
			contract = set
		}
		// The handler gets its own store aggregation from the same pool;
		// stores are stateless wrappers, so sharing the pool is enough.
		st := store.New(env.pool)
		manager := publish.NewManager(publish.Config{
			Pool:         env.pool,
			Releases:     st.Releases,
			Builds:       st.Builds,
			Themes:       st.Themes,
			ReleasesRoot: releasesRoot,
			Contracts:    contract,
			Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
		})
		server = publish.NewServer(st.Releases, releasesRoot)
		cfg.Publisher = manager
		cfg.PublishedOrigins = server
	})
	t.Cleanup(func() {
		if server != nil {
			server.Close()
		}
		_ = buildrunner.MakeTreeWritable(releasesRoot)
	})
	return env, buildsRoot, releasesRoot
}

// waitForBuildSucceeded polls the build endpoint until terminal state.
func waitForBuildSucceeded(t *testing.T, client *tclient, siteID uuid.UUID, buildID string) buildResp {
	t.Helper()
	path := "/api/v1/sites/" + siteID.String() + "/builds/" + buildID
	deadline := time.Now().Add(8 * time.Minute)
	var final buildResp
	for time.Now().Before(deadline) {
		status, _, raw := client.do(http.MethodGet, path, "", nil)
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
	return final
}

// createBuildAndWait drives one build through the API to success.
func createBuildAndWait(t *testing.T, client *tclient, siteID uuid.UUID) buildResp {
	t.Helper()
	status, _, raw := client.do(http.MethodPost, "/api/v1/sites/"+siteID.String()+"/builds", "", nil)
	if status != http.StatusAccepted {
		t.Fatalf("create build: status %d body %s", status, raw)
	}
	var created buildResp
	decodeJSONBody(t, raw, &created)
	return waitForBuildSucceeded(t, client, siteID, created.ID)
}

type releaseResp struct {
	ID           uuid.UUID          `json:"id"`
	SiteID       uuid.UUID          `json:"siteId"`
	BuildID      uuid.UUID          `json:"buildId"`
	Status       string             `json:"status"`
	Reasons      []string           `json:"reasons"`
	Routes       map[string]any     `json:"routes"`
	InputDigests json.RawMessage    `json:"inputDigests"`
	Failure      *releaseFailureDTO `json:"failure"`
	Theme        struct {
		ThemeID string `json:"themeId"`
		Version string `json:"version"`
	} `json:"theme"`
}

// TestPublishRollbackClosedLoop drives the real pipeline end to end: real
// Node build, publish, published origin, second publish after an unpublished
// settings edit, rollback restoring the old artifacts without a rebuild and
// without discarding the edit, and the permission boundaries.
func TestPublishRollbackClosedLoop(t *testing.T) {
	themeRoot := testThemeRoot(t)
	env, buildsRoot, releasesRoot := setupPublish(t)
	ctx := context.Background()

	owner, ownerToken, ownerCSRF := env.createUserSession("pubowner")
	viewer, viewerToken, viewerCSRF := env.createUserSession("pubviewer")
	_, outsiderToken, outsiderCSRF := env.createUserSession("puboutsider")
	site := createSite(ctx, t, env, "pub-site", "发布站甲", "描述", "zh-CN", owner.ID)
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		site.Site.ID, viewer.ID); err != nil {
		t.Fatal(err)
	}
	registerThemeDir(t, env, themeRoot)

	ownerClient := env.clientFor(ownerToken, ownerCSRF)
	viewerClient := env.clientFor(viewerToken, viewerCSRF)
	outsiderClient := env.clientFor(outsiderToken, outsiderCSRF)

	// Select the theme, build, and publish the first release.
	selectThemeForSite(t, ownerClient, site.Site.ID, themeIDAt(t, themeRoot), themeVersionAt(t, themeRoot))
	firstBuild := createBuildAndWait(t, ownerClient, site.Site.ID)

	// Viewer cannot publish; outsider sees nothing.
	if status, _, _ := viewerClient.do(http.MethodPost, "/api/v1/sites/"+site.Site.ID.String()+"/releases",
		map[string]any{"buildId": firstBuild.ID}, nil); status != http.StatusForbidden {
		t.Fatalf("viewer publish: status %d, want 403", status)
	}
	if status, _, _ := outsiderClient.do(http.MethodGet, "/api/v1/sites/"+site.Site.ID.String()+"/releases", "", nil); status != http.StatusNotFound {
		t.Fatalf("outsider list releases: status %d, want 404", status)
	}

	first := publishViaAPI(t, ownerClient, site.Site.ID, firstBuild.ID)

	// The release directory is frozen and carries contract-valid metadata.
	releaseDir := filepath.Join(releasesRoot, site.Site.ID.String(), firstBuild.ID)
	metaBytes, err := os.ReadFile(filepath.Join(releaseDir, "release.json"))
	if err != nil {
		t.Fatalf("read release.json: %v", err)
	}
	var meta struct {
		Contract string   `json:"contract"`
		Release  string   `json:"releaseId"`
		Reasons  []string `json:"reasons"`
		Theme    struct {
			ID string `json:"id"`
		} `json:"theme"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil || meta.Contract != "fuyao.theme.release/v1" || meta.Theme.ID != themeIDAt(t, themeRoot) {
		t.Fatalf("release.json malformed: %s err=%v", metaBytes, err)
	}
	if meta.Release != firstBuild.ID || len(meta.Reasons) != 1 || meta.Reasons[0] != "manual" {
		t.Fatalf("release.json identity mismatch: %+v", meta)
	}
	if info, err := os.Stat(filepath.Join(releaseDir, "release.json")); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("release.json missing or writable: %v %v", err, info)
	}
	// The build artifacts backing the release stay untouched and frozen.
	if info, err := os.Stat(filepath.Join(buildsRoot, firstBuild.ID, "artifacts", "index.html")); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("build artifacts missing or writable: %v %v", err, info)
	}

	// The published origin serves the first release's content.
	current := getCurrentRelease(t, ownerClient, site.Site.ID)
	if current.ID != first.ID || current.Status != "live" {
		t.Fatalf("current release mismatch: %+v vs %+v", current, first)
	}
	originBase := currentOriginURL(t, ownerClient, site.Site.ID)
	page1 := fetchPublished(t, originBase, "")
	if !strings.Contains(page1, "发布站甲") {
		t.Fatalf("published origin does not serve the first release: %s", firstSnippet(page1))
	}
	// A deep prerendered route keeps production URL semantics.
	if deep := fetchPublished(t, originBase, "/blog/"); !strings.Contains(strings.ToLower(deep[:32]), "doctype") {
		t.Fatalf("deep route missing on published origin: %s", firstSnippet(deep))
	}

	// Edit settings (an unpublished edit), rebuild, publish the second
	// release; the same origin switches content.
	patchSettingsName(t, ownerClient, site.Site.ID, "发布站乙")
	secondBuild := createBuildAndWait(t, ownerClient, site.Site.ID)
	second := publishViaAPI(t, ownerClient, site.Site.ID, secondBuild.ID)
	if second.ID == first.ID {
		t.Fatal("second publish reused the first release row")
	}
	page2 := fetchPublished(t, originBase, "")
	if !strings.Contains(page2, "发布站乙") {
		t.Fatalf("published origin did not switch to the second release: %s", firstSnippet(page2))
	}

	// The history reports live + superseded.
	status, _, raw := ownerClient.do(http.MethodGet, "/api/v1/sites/"+site.Site.ID.String()+"/releases", "", nil)
	if status != http.StatusOK {
		t.Fatalf("list releases: status %d body %s", status, raw)
	}
	var list struct {
		Releases []releaseResp `json:"releases"`
	}
	decodeJSONBody(t, raw, &list)
	if len(list.Releases) != 2 || list.Releases[0].Status != "live" || list.Releases[1].Status != "superseded" {
		t.Fatalf("release history wrong: %+v", list.Releases)
	}

	// Rollback re-points at the first release; the origin switches back
	// without a rebuild and the unpublished settings edit stays in place.
	status, _, raw = ownerClient.do(http.MethodPost,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+first.ID.String()+"/rollback", "", nil)
	if status != http.StatusOK {
		t.Fatalf("rollback: status %d body %s", status, raw)
	}
	page3 := fetchPublished(t, originBase, "")
	if !strings.Contains(page3, "发布站甲") || strings.Contains(page3, "发布站乙") {
		t.Fatalf("rollback did not restore the first release: %s", firstSnippet(page3))
	}
	var settingsAfter struct {
		Name string `json:"name"`
	}
	sStatus, _, sRaw := ownerClient.do(http.MethodGet, "/api/v1/sites/"+site.Site.ID.String()+"/settings", "", nil)
	if sStatus != http.StatusOK {
		t.Fatalf("get settings after rollback: %d %s", sStatus, sRaw)
	}
	decodeJSONBody(t, sRaw, &settingsAfter)
	if settingsAfter.Name != "发布站乙" {
		t.Fatalf("rollback discarded the unpublished settings edit: %+v", settingsAfter)
	}

	// Rolling back to the already-live release is a conflict; unknown ids 404.
	if status, _, _ := ownerClient.do(http.MethodPost,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+first.ID.String()+"/rollback", "", nil); status != http.StatusConflict {
		t.Fatalf("rollback to live release: status %d, want 409", status)
	}
	if status, _, _ := ownerClient.do(http.MethodPost,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+uuid.NewString()+"/rollback", "", nil); status != http.StatusNotFound {
		t.Fatalf("rollback to unknown release: status %d, want 404", status)
	}
	// Viewer cannot roll back.
	if status, _, _ := viewerClient.do(http.MethodPost,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+second.ID.String()+"/rollback", "", nil); status != http.StatusForbidden {
		t.Fatalf("viewer rollback: status %d, want 403", status)
	}

	// Publishing a build that does not back this site is rejected.
	if status, _, _ := ownerClient.do(http.MethodPost, "/api/v1/sites/"+site.Site.ID.String()+"/releases",
		map[string]any{"buildId": uuid.NewString()}, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("publish unknown build: want 422")
	}
}

// TestPublishRequiredArtifactsFailure proves a publish blocked by the
// theme's own requiredArtifacts fails with the precise missing list, keeps
// the previous release live, and persists the failed attempt.
func TestPublishRequiredArtifactsFailure(t *testing.T) {
	themeRoot := testThemeRoot(t)
	env, _, _ := setupPublish(t)
	ctx := context.Background()

	owner, ownerToken, ownerCSRF := env.createUserSession("pubfailowner")
	site := createSite(ctx, t, env, "pubfail-site", "发布站丙", "", "zh-CN", owner.ID)
	registerThemeDir(t, env, themeRoot)
	ownerClient := env.clientFor(ownerToken, ownerCSRF)
	selectThemeForSite(t, ownerClient, site.Site.ID, themeIDAt(t, themeRoot), themeVersionAt(t, themeRoot))

	// First publish succeeds and goes live.
	goodBuild := createBuildAndWait(t, ownerClient, site.Site.ID)
	publishViaAPI(t, ownerClient, site.Site.ID, goodBuild.ID)
	originBase := currentOriginURL(t, ownerClient, site.Site.ID)

	// A fixture theme that builds fine but declares an artifact its build
	// never produces: the build succeeds, the publish must not.
	fixtureRoot := t.TempDir()
	writePublishFixtureTheme(t, fixtureRoot)
	registerThemeDir(t, env, fixtureRoot)
	selectThemeForSite(t, ownerClient, site.Site.ID, "pub-fixture-theme", "1.0.0")
	badBuild := createBuildAndWait(t, ownerClient, site.Site.ID)

	status, _, raw := ownerClient.do(http.MethodPost, "/api/v1/sites/"+site.Site.ID.String()+"/releases",
		map[string]any{"buildId": badBuild.ID, "reasons": []string{"static"}}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("publish missing-artifact build: status %d body %s, want 422", status, raw)
	}
	var failed releaseResp
	decodeJSONBody(t, raw, &failed)
	if failed.Status != "failed" || failed.Failure == nil ||
		failed.Failure.Stage != "artifacts" ||
		len(failed.Failure.MissingArtifacts) == 0 || failed.Failure.MissingArtifacts[0] != "nope.txt" {
		t.Fatalf("failure record wrong: %+v", failed.Failure)
	}

	// The previous release is still the live one and still serving.
	current := getCurrentRelease(t, ownerClient, site.Site.ID)
	if current.BuildID.String() != goodBuild.ID {
		t.Fatalf("failed publish changed the live release: %+v", current)
	}
	page := fetchPublished(t, originBase, "")
	if !strings.Contains(page, "发布站丙") {
		t.Fatal("published origin stopped serving after a failed publish")
	}
	// The failed attempt is part of the history and cannot be rolled back to.
	var list struct {
		Releases []releaseResp `json:"releases"`
	}
	_, _, lRaw := ownerClient.do(http.MethodGet, "/api/v1/sites/"+site.Site.ID.String()+"/releases", "", nil)
	decodeJSONBody(t, lRaw, &list)
	if len(list.Releases) != 2 || list.Releases[0].Status != "failed" || list.Releases[1].Status != "live" {
		t.Fatalf("history after failed publish wrong: %+v", list.Releases)
	}
	if status, _, _ := ownerClient.do(http.MethodPost,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+failed.ID.String()+"/rollback", "", nil); status != http.StatusConflict {
		t.Fatalf("rollback to failed release: status %d, want 409", status)
	}
}

// --- helpers ---

func publishViaAPI(t *testing.T, client *tclient, siteID uuid.UUID, buildID string) releaseResp {
	t.Helper()
	status, _, raw := client.do(http.MethodPost, "/api/v1/sites/"+siteID.String()+"/releases",
		map[string]any{"buildId": buildID, "reasons": []string{"manual"}}, nil)
	if status != http.StatusCreated {
		t.Fatalf("publish: status %d body %s", status, raw)
	}
	var release releaseResp
	decodeJSONBody(t, raw, &release)
	if release.Status != "live" {
		t.Fatalf("published release not live: %+v", release)
	}
	return release
}

type currentReleaseResp struct {
	Release   releaseResp `json:"release"`
	OriginURL string      `json:"originUrl"`
}

func getCurrentRelease(t *testing.T, client *tclient, siteID uuid.UUID) releaseResp {
	t.Helper()
	status, _, raw := client.do(http.MethodGet, "/api/v1/sites/"+siteID.String()+"/release", "", nil)
	if status != http.StatusOK {
		t.Fatalf("get current release: status %d body %s", status, raw)
	}
	var current currentReleaseResp
	decodeJSONBody(t, raw, &current)
	return current.Release
}

func currentOriginURL(t *testing.T, client *tclient, siteID uuid.UUID) string {
	t.Helper()
	status, _, raw := client.do(http.MethodGet, "/api/v1/sites/"+siteID.String()+"/release", "", nil)
	if status != http.StatusOK {
		t.Fatalf("get current release: status %d body %s", status, raw)
	}
	var current currentReleaseResp
	decodeJSONBody(t, raw, &current)
	if current.OriginURL == "" {
		t.Fatal("current release carries no origin URL")
	}
	return current.OriginURL
}

// fetchPublished GETs the published origin with the path appended.
func fetchPublished(t *testing.T, base, path string) string {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("fetch published origin %s: %v", base+path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("published origin %s: status %d", base+path, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read published page: %v", err)
	}
	return string(data)
}

func firstSnippet(s string) string {
	if len(s) > 60 {
		return s[:60]
	}
	return s
}

// selectThemeForSite PUTs the theme selection using the current selection
// ETag (revision t0 for a first selection).
func selectThemeForSite(t *testing.T, client *tclient, siteID uuid.UUID, themeID, version string) {
	t.Helper()
	gStatus, header, _ := client.do(http.MethodGet, "/api/v1/sites/"+siteID.String()+"/theme", "", nil)
	if gStatus != http.StatusOK {
		t.Fatalf("get theme selection: status %d", gStatus)
	}
	etag := header.Get("ETag")
	if etag == "" {
		t.Fatal("theme selection response carries no ETag")
	}
	status, _, raw := client.do(http.MethodPut, "/api/v1/sites/"+siteID.String()+"/theme",
		themePutBody(themeID, version, ""), map[string]string{"If-Match": etag})
	if status != http.StatusOK {
		t.Fatalf("select theme %s: status %d body %s", themeID, status, raw)
	}
}

// patchSettingsName renames the site through the settings API with the
// current ETag.
func patchSettingsName(t *testing.T, client *tclient, siteID uuid.UUID, name string) {
	t.Helper()
	sStatus, header, sRaw := client.do(http.MethodGet, "/api/v1/sites/"+siteID.String()+"/settings", "", nil)
	if sStatus != http.StatusOK {
		t.Fatalf("get settings: %d %s", sStatus, sRaw)
	}
	etag := header.Get("ETag")
	if etag == "" {
		t.Fatal("settings response carries no ETag")
	}
	status, _, raw := client.do(http.MethodPatch, "/api/v1/sites/"+siteID.String()+"/settings",
		map[string]any{"name": name}, map[string]string{"If-Match": etag})
	if status != http.StatusOK {
		t.Fatalf("patch settings: status %d body %s", status, raw)
	}
}

// writePublishFixtureTheme materializes a theme that satisfies the runner's
// requirements and builds with plain node (no dependencies), but declares a
// required artifact its build never produces.
func writePublishFixtureTheme(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"theme.json":   `{"contract":"fuyao.theme.manifest/v1","id":"pub-fixture-theme","version":"1.0.0","name":"Publish Fixture","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["blog"],"outputs":{"spaFallback":"200.html","urlConventions":["posts","blog-rss","sitemap"],"requiredArtifacts":["nope.txt"]}}`,
		"package.json": `{"name":"pub-fixture-theme","version":"1.0.0","private":true,"scripts":{"build":"node build.js"}}`,
		"build.js":     "const fs=require('fs');\nfs.mkdirSync('build',{recursive:true});\nfs.writeFileSync('build/index.html','<html>fixture</html>');\nfs.writeFileSync('build/200.html','<html>fallback</html>');\n",
		"scripts/prepare-build-inputs.js": "const fs=require('fs');" +
			"fs.mkdirSync('.fuyao/build-inputs/current/public',{recursive:true});" +
			"fs.mkdirSync('.fuyao/build-inputs/current/private',{recursive:true});" +
			"fs.writeFileSync('.fuyao/build-inputs/current/public/index.html','<html>s</html>');" +
			"fs.writeFileSync('.fuyao/build-inputs/current/private/input.json',JSON.stringify({" +
			"contract:'fuyao.theme.build-input/v1',theme:{id:'pub-fixture-theme',version:'1.0.0'}," +
			"inputs:{contentDigest:'sha256:" + strings.Repeat("0", 64) + "'," +
			"siteConfigDigest:'sha256:" + strings.Repeat("1", 64) + "'," +
			"themeConfigDigest:'sha256:" + strings.Repeat("2", 64) + "'}}));\n",
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
