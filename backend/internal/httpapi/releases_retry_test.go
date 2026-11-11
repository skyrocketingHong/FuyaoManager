package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/themecontract"
)

// flakyContracts injects exactly one ValidateRelease failure, then defers to
// the real contract set. It reproduces "a transient publish failure on an
// otherwise valid build" without touching real infrastructure.
type flakyContracts struct {
	real     *themecontract.Set
	failNext atomic.Bool
	injected atomic.Int64
}

func (f *flakyContracts) ValidateRelease(data []byte) error {
	if f.failNext.CompareAndSwap(true, false) {
		f.injected.Add(1)
		return errors.New("injected transient validation failure")
	}
	return f.real.ValidateRelease(data)
}

// insertFakeSucceededBuild stages a succeeded build row with a real artifact
// directory (no Node build needed): publish only consumes the row plus the
// frozen artifacts on disk.
func insertFakeSucceededBuild(t *testing.T, env *testEnv, siteID, userID, themeRowID uuid.UUID, themeID, version string, manifestDigest []byte, marker string) uuid.UUID {
	t.Helper()
	buildID := uuid.New()
	artifactDir := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"index.html": "<html>fake:" + marker + "</html>",
		"200.html":   "<html>fallback</html>",
	} {
		if err := os.WriteFile(filepath.Join(artifactDir, name), []byte(body), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	digests, err := json.Marshal(map[string]string{
		"contentDigest":     "sha256:" + strings.Repeat("1", 64),
		"siteConfigDigest":  "sha256:" + strings.Repeat("2", 64),
		"themeConfigDigest": "sha256:" + strings.Repeat("3", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.pool.Exec(context.Background(),
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

// fakeThemeFixture registers a contract-valid theme whose required artifact
// our fake build actually produces, and returns its row id + digest.
func fakeThemeFixture(t *testing.T, env *testEnv) (uuid.UUID, []byte, string, string) {
	t.Helper()
	root := t.TempDir()
	manifest := `{"contract":"fuyao.theme.manifest/v1","id":"retry-fixture-theme","version":"1.0.0","name":"Retry Fixture","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["blog"],"outputs":{"spaFallback":"200.html","urlConventions":["posts","blog-rss","sitemap"],"requiredArtifacts":["index.html"]}}`
	if err := os.WriteFile(filepath.Join(root, "theme.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	registerThemeDir(t, env, root)
	var rowID uuid.UUID
	var digest []byte
	if err := env.pool.QueryRow(context.Background(),
		`SELECT id, manifest_digest FROM themes WHERE theme_id='retry-fixture-theme'`).Scan(&rowID, &digest); err != nil {
		t.Fatalf("load fixture theme row: %v", err)
	}
	return rowID, digest, "retry-fixture-theme", "1.0.0"
}

func newTestManager(t *testing.T, env *testEnv, contract publish.ReleaseContract, releasesRoot string) *publish.Manager {
	t.Helper()
	if contract == nil {
		set, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
		if err != nil {
			t.Fatalf("load contracts: %v", err)
		}
		contract = set
	}
	return publish.NewManager(publish.Config{
		Pool:         env.pool,
		Releases:     env.store.Releases,
		Builds:       env.store.Builds,
		Themes:       env.store.Themes,
		ReleasesRoot: releasesRoot,
		Contracts:    contract,
		Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
	})
}

// TestPublishRetryAfterInjectedFailure covers the failed-retry regression:
// one transient publish failure records history but must not lock the build
// out; retrying the same build succeeds; staging and final directories are
// coordinated correctly throughout.
func TestPublishRetryAfterInjectedFailure(t *testing.T) {
	real, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
	if err != nil {
		t.Fatalf("load contracts: %v", err)
	}
	flaky := &flakyContracts{real: real}
	env, _, releasesRoot := setupPublishWithContract(t, flaky)
	ctx := context.Background()

	owner, ownerToken, ownerCSRF := env.createUserSession("retryowner")
	site, err := env.store.Sites.Create(ctx, "retry-site", "Retry Site", "", "zh-CN", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	buildID := insertFakeSucceededBuild(t, env, site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "retry")

	manager := newTestManager(t, env, flaky, releasesRoot)
	siteRoot := filepath.Join(releasesRoot, site.Site.ID.String())

	// First attempt: the contract validation fails once.
	flaky.failNext.Store(true)
	failed, err := manager.Publish(ctx, site.Site.ID, buildID, owner.ID, []string{"manual"})
	if err == nil {
		t.Fatal("injected failure did not fail the publish")
	}
	if failed.Failure == nil || failed.Failure.Stage != publish.StageReleaseMetadata {
		t.Fatalf("failure record wrong: %+v", failed.Failure)
	}
	if flaky.injected.Load() != 1 {
		t.Fatal("injection did not fire")
	}
	// The failed attempt is queryable history...
	rows, err := env.store.Releases.ListForUser(ctx, site.Site.ID, owner.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "failed" {
		t.Fatalf("failed attempt not in history: %v %+v", err, rows)
	}
	// ...and left no directories behind: no final dir, no staging leftovers.
	if _, err := os.Stat(filepath.Join(siteRoot, buildID.String())); !os.IsNotExist(err) {
		t.Fatalf("failed attempt left a final directory: %v", err)
	}
	assertNoStagingLeftovers(t, siteRoot)

	// Retry the same build after the transient fault cleared: it must
	// succeed despite the earlier failed row (partial unique index).
	live, err := manager.Publish(ctx, site.Site.ID, buildID, owner.ID, []string{"manual"})
	if err != nil {
		t.Fatalf("retry after failure did not succeed: %v", err)
	}
	if live.Status != "live" {
		t.Fatalf("retried release not live: %+v", live)
	}
	finalDir := filepath.Join(siteRoot, buildID.String())
	if _, err := os.Stat(filepath.Join(finalDir, "release.json")); err != nil {
		t.Fatalf("retried release missing release.json: %v", err)
	}
	rows, err = env.store.Releases.ListForUser(ctx, site.Site.ID, owner.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("history after retry wrong: %v %+v", err, rows)
	}
	assertNoStagingLeftovers(t, siteRoot)

	// Republishing the now-released build is a conflict, not a new row.
	if _, err := manager.Publish(ctx, site.Site.ID, buildID, owner.ID, []string{"manual"}); !errors.Is(err, publish.ErrBuildAlreadyReleased) {
		t.Fatalf("republish conflict not reported: %v", err)
	}
	rows, err = env.store.Releases.ListForUser(ctx, site.Site.ID, owner.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("conflict added a row: %v %+v", err, rows)
	}

	// The GET release route serves both the live record and the failed one.
	client := env.clientFor(ownerToken, ownerCSRF)
	if status, _, raw := client.do(http.MethodGet,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+live.ID.String(), "", nil); status != http.StatusOK {
		t.Fatalf("get live release: status %d body %s", status, raw)
	}
	failedID := uuid.Nil
	for _, row := range rows {
		if row.Status == "failed" {
			failedID = row.ID
		}
	}
	if status, _, raw := client.do(http.MethodGet,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+failedID.String(), "", nil); status != http.StatusOK {
		t.Fatalf("get failed release: status %d body %s", status, raw)
	}
	if status, _, _ := client.do(http.MethodGet,
		"/api/v1/sites/"+site.Site.ID.String()+"/releases/"+uuid.NewString(), "", nil); status != http.StatusNotFound {
		t.Fatalf("unknown release: want 404, got %d", status)
	}
}

// TestPublishConcurrentSameBuildAndCleanup covers directory coordination:
// concurrent publishes of one build leave exactly one release (the loser
// gets a conflict and deletes nothing), and concurrent publishes of two
// different builds on one site never delete each other's sealed artifacts.
func TestPublishConcurrentSameBuildAndCleanup(t *testing.T) {
	env, _, releasesRoot := setupPublish(t)
	ctx := context.Background()

	owner, _, _ := env.createUserSession("concowner")
	site, err := env.store.Sites.Create(ctx, "conc-site", "Conc Site", "", "zh-CN", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	buildA := insertFakeSucceededBuild(t, env, site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "A")
	buildB := insertFakeSucceededBuild(t, env, site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "B")
	manager := newTestManager(t, env, nil, releasesRoot)
	siteRoot := filepath.Join(releasesRoot, site.Site.ID.String())

	// Same build, two concurrent attempts: exactly one wins.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.Publish(ctx, site.Site.ID, buildA, owner.ID, []string{"manual"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var conflicts, successes int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, publish.ErrBuildAlreadyReleased):
			conflicts++
		default:
			t.Fatalf("unexpected publish error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("same-build concurrency: successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
	if _, err := os.Stat(filepath.Join(siteRoot, buildA.String(), "release.json")); err != nil {
		t.Fatalf("winning release directory missing: %v", err)
	}
	assertNoStagingLeftovers(t, siteRoot)

	// Two different builds on the same site, concurrently: both seal, one
	// goes live and the other supersedes; both directories stay intact.
	resultsB := make(chan error, 2)
	for _, build := range []uuid.UUID{buildA, buildB} {
		build := build
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.Publish(ctx, site.Site.ID, build, owner.ID, []string{"manual"})
			resultsB <- err
		}()
	}
	wg.Wait()
	close(resultsB)
	for err := range resultsB {
		if err != nil && !errors.Is(err, publish.ErrBuildAlreadyReleased) {
			t.Fatalf("cross-build publish error: %v", err)
		}
	}
	for _, build := range []uuid.UUID{buildA, buildB} {
		if _, err := os.Stat(filepath.Join(siteRoot, build.String(), "release.json")); err != nil {
			t.Fatalf("release directory for %s damaged by concurrent publish: %v", build, err)
		}
	}
	liveCount := 0
	rows, err := env.store.Releases.ListForUser(ctx, site.Site.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Status == "live" {
			liveCount++
		}
	}
	if liveCount != 1 {
		t.Fatalf("exactly one live release expected, got %d (%+v)", liveCount, rows)
	}
	assertNoStagingLeftovers(t, siteRoot)
}

// assertNoStagingLeftovers fails when any staging directory survived: every
// attempt must clean up its own staging tree on both success and failure.
func assertNoStagingLeftovers(t *testing.T, siteRoot string) {
	t.Helper()
	staging := filepath.Join(siteRoot, ".staging")
	entries, err := os.ReadDir(staging)
	if err == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("staging leftovers: %v", names)
	}
}
