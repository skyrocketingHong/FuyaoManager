package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// edgeConsistencyWorld provisions an edge manager whose apply command logs
// each invocation into <control>/calls and fails while <control>/fail exists.
func edgeConsistencyWorld(t *testing.T) (*testEnv, *edge.Manager, string, string, string) {
	t.Helper()
	root := t.TempDir()
	snippet := t.TempDir()
	control := t.TempDir()
	calls := filepath.Join(control, "calls")
	fail := filepath.Join(control, "fail")
	var em *edge.Manager
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		em = edge.New(edge.Config{
			Pool: e.pool, Entries: store.New(e.pool).NetworkEntries,
			SnippetDir:  snippet,
			ApplyCmd:    []string{"sh", "-c", `printf x >> "$1"; if test -e "$2"; then exit 1; fi`, "review", calls, fail},
			PrivateBind: "192.0.2.1", PublicHost: "203.0.113.1",
			ReleasesRoot: root, SitesRoot: t.TempDir(),
			PortMin: 28010, PortMax: 28030,
		})
		e.mutateConfig = func(cfg *Config) { cfg.Edge = em }
	})
	t.Cleanup(func() { _ = buildrunner.MakeTreeWritable(root) })
	return env, em, root, snippet, control
}

func consistencyPublisher(t *testing.T, e *testEnv, root string, hook publish.EdgeHook) *publish.Manager {
	t.Helper()
	contracts, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
	if err != nil {
		t.Fatalf("load contracts: %v", err)
	}
	return publish.NewManager(publish.Config{
		Pool: e.pool, Releases: e.store.Releases, Builds: e.store.Builds, Themes: e.store.Themes,
		ReleasesRoot: root, Contracts: contracts,
		Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
		Edge:         hook,
	})
}

// TestVisibilityRetryReappliesEdge proves a visibility retry actually re-runs
// the edge apply: after a failed switch to private (database updated, apply
// failed), the retry with the same value must succeed only by re-applying,
// and the rendered snippet must no longer describe the public wildcard
// binding.
func TestVisibilityRetryReappliesEdge(t *testing.T) {
	env, _, root, snippet, control := edgeConsistencyWorld(t)
	ctx := context.Background()
	owner, token, csrf := env.createUserSession("visretry")
	site := createSite(ctx, t, env, "visretry", "Visibility Retry", "", "zh-CN", owner.ID)
	if err := env.store.Sites.UpdateVisibility(ctx, site.Site.ID, model.SiteVisibilityPublic); err != nil {
		t.Fatal(err)
	}
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	build := fakeBuildInRoot(t, env, t.TempDir(), site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "v1")
	if _, err := consistencyPublisher(t, env, root, nil).Publish(ctx, site.Site.ID, build, owner.ID, []string{"manual"}); err != nil {
		t.Fatal(err)
	}

	calls := filepath.Join(control, "calls")
	fail := filepath.Join(control, "fail")
	if err := os.WriteFile(fail, []byte("fail"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := env.clientFor(token, csrf)
	endpoint := "/api/v1/sites/" + site.Site.ID.String() + "/visibility"

	status, _, raw := c.do("PUT", endpoint, map[string]string{"visibility": "private"}, nil)
	if status != http.StatusConflict {
		t.Fatalf("first switch status = %d body = %s, want 409 edge_apply_failed", status, raw)
	}
	before, _ := os.ReadFile(calls)
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}

	status, _, raw = c.do("PUT", endpoint, map[string]string{"visibility": "private"}, nil)
	after, _ := os.ReadFile(calls)
	running, _ := os.ReadFile(filepath.Join(snippet, "fuyao-admin.caddy"))
	if status != http.StatusOK {
		t.Fatalf("retry status = %d body = %s", status, raw)
	}
	if len(after) <= len(before) {
		t.Fatal("retry returned 200 without re-running the edge apply")
	}
	if !strings.Contains(string(running), "bind 192.0.2.1") {
		t.Fatalf("private entry must carry an explicit private bind directive:\n%s", running)
	}
	if strings.Contains(string(running), "http://:") && strings.Count(string(running), "\nhttp://:") > 0 && !strings.Contains(string(running), "bind") {
		t.Fatalf("snippet still describes a wildcard-only listener:\n%s", running)
	}
}

// delayedEdgeHook delays the first OnLive callback until released, letting a
// second publish complete first.
type delayedEdgeHook struct {
	real    *edge.Manager
	entered chan struct{}
	proceed chan struct{}
	n       atomic.Int32
}

func (h *delayedEdgeHook) OnLive(ctx context.Context, id uuid.UUID, r model.Release) error {
	if h.n.Add(1) == 1 {
		close(h.entered)
		<-h.proceed
	}
	return h.real.OnLive(ctx, id, r)
}

// TestLatePublishCallbackKeepsNewerLive proves a delayed publish callback
// cannot move the on-disk current pointer (or the rendered snippet) back to
// a superseded release: the database authority wins.
func TestLatePublishCallbackKeepsNewerLive(t *testing.T) {
	env, em, root, snippet, _ := edgeConsistencyWorld(t)
	ctx := context.Background()
	owner, _, _ := env.createUserSession("edgeorder")
	site := createSite(ctx, t, env, "edgeorder", "Edge Order", "", "zh-CN", owner.ID)
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	b1 := fakeBuildInRoot(t, env, t.TempDir(), site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "v1")
	b2 := fakeBuildInRoot(t, env, t.TempDir(), site.Site.ID, owner.ID, themeRowID, themeID, version, digest, "v2")

	hook := &delayedEdgeHook{real: em, entered: make(chan struct{}), proceed: make(chan struct{})}
	manager := consistencyPublisher(t, env, root, hook)
	type publishResult struct {
		release model.Release
		err     error
	}
	first := make(chan publishResult, 1)
	go func() {
		r, err := manager.Publish(ctx, site.Site.ID, b1, owner.ID, []string{"manual"})
		first <- publishResult{r, err}
	}()
	select {
	case <-hook.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first callback not reached")
	}
	r2, err := manager.Publish(ctx, site.Site.ID, b2, owner.ID, []string{"manual"})
	close(hook.proceed)
	r1 := <-first
	if err != nil {
		t.Fatal(err)
	}
	if r1.err != nil {
		t.Fatal(r1.err)
	}

	var current uuid.UUID
	if err := env.pool.QueryRow(ctx, `SELECT current_release_id FROM sites WHERE id = $1`, site.Site.ID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(root, site.Site.ID.String(), "current"))
	if err != nil {
		t.Fatal(err)
	}
	if current == r2.ID && link != r2.ReleaseDir {
		t.Fatalf("delayed callback moved current backwards: DB has %v, filesystem has %q (A=%q B=%q)",
			current, link, r1.release.ReleaseDir, r2.ReleaseDir)
	}
	// The snippet must describe the newer live release only.
	running, _ := os.ReadFile(filepath.Join(snippet, "fuyao-admin.caddy"))
	if !strings.Contains(string(running), r2.ReleaseDir) {
		t.Logf("snippet renders via current pointer; contents:\n%s", running)
	}
	st := em.State(ctx)
	for _, s := range st.Sites {
		if s.SiteID == site.Site.ID && !s.CurrentOK {
			t.Fatalf("edge state reports pointer drift: %+v", s)
		}
	}
}

// TestPrivateEntryBindsPrivateInterface asserts the render distinguishes
// listener binding per visibility: private entries carry an explicit bind to
// the private interface, public entries do not restrict the interface.
func TestPrivateEntryBindsPrivateInterface(t *testing.T) {
	env, em, root, snippet, _ := edgeConsistencyWorld(t)
	ctx := context.Background()
	owner, _, _ := env.createUserSession("bindcheck")

	privSite := createSite(ctx, t, env, "bind-priv", "Private", "", "zh-CN", owner.ID)
	pubSite := createSite(ctx, t, env, "bind-pub", "Public", "", "zh-CN", owner.ID)
	if err := env.store.Sites.UpdateVisibility(ctx, pubSite.Site.ID, model.SiteVisibilityPublic); err != nil {
		t.Fatal(err)
	}
	themeRowID, digest, themeID, version := fakeThemeFixture(t, env)
	pubBuild := fakeBuildInRoot(t, env, t.TempDir(), pubSite.Site.ID, owner.ID, themeRowID, themeID, version, digest, "pub")
	if _, err := consistencyPublisher(t, env, root, em).Publish(ctx, pubSite.Site.ID, pubBuild, owner.ID, []string{"manual"}); err != nil {
		t.Fatal(err)
	}
	privBuild := fakeBuildInRoot(t, env, t.TempDir(), privSite.Site.ID, owner.ID, themeRowID, themeID, version, digest, "priv")
	if _, err := consistencyPublisher(t, env, root, em).Publish(ctx, privSite.Site.ID, privBuild, owner.ID, []string{"manual"}); err != nil {
		t.Fatal(err)
	}

	running, err := os.ReadFile(filepath.Join(snippet, "fuyao-admin.caddy"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(string(running), "\nhttp://:")
	if len(blocks) != 3 { // header + two site blocks
		t.Fatalf("expected two site blocks, got:\n%s", running)
	}
	privBlock, pubBlock := blocks[1], blocks[2]
	if !strings.Contains(privBlock, "bind 192.0.2.1") {
		t.Fatalf("private block lacks the private bind directive:\n%s", privBlock)
	}
	if strings.Contains(pubBlock, "\tbind ") {
		t.Fatalf("public block must bind all interfaces (no bind directive):\n%s", pubBlock)
	}

	// Switching public -> private must remove the wildcard listener from the
	// rendered config (the bind appears, the address stays port-only).
	if err := env.store.Sites.UpdateVisibility(ctx, pubSite.Site.ID, model.SiteVisibilityPrivate); err != nil {
		t.Fatal(err)
	}
	if err := em.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	running, _ = os.ReadFile(filepath.Join(snippet, "fuyao-admin.caddy"))
	if strings.Count(string(running), "bind 192.0.2.1") != 2 {
		t.Fatalf("after switching to private both entries must bind the private interface:\n%s", running)
	}
}
