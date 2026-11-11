package buildrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
)

// memBuildStore is an in-memory BuildStore for workspace tests: it records
// source digests and terminal states without a database, so two Runners can
// run truly concurrently — the isolation under test is the per-task working
// copy, not a shared advisory lock.
type memBuildStore struct {
	mu      sync.Mutex
	rows    map[uuid.UUID]*model.Build
	digests map[uuid.UUID][2]any // aggregate, fileCount
}

func newMemBuildStore(builds ...model.Build) *memBuildStore {
	s := &memBuildStore{rows: map[uuid.UUID]*model.Build{}, digests: map[uuid.UUID][2]any{}}
	for i := range builds {
		b := builds[i]
		s.rows[b.ID] = &b
	}
	return s
}

func (s *memBuildStore) Get(ctx context.Context, id uuid.UUID) (model.Build, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.rows[id]; ok {
		return *b, nil
	}
	return model.Build{}, os.ErrNotExist
}

func (s *memBuildStore) MarkRunning(ctx context.Context, id uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.rows[id]
	if !ok || b.Status != model.BuildStatusQueued {
		return false, nil
	}
	b.Status = model.BuildStatusRunning
	return true, nil
}

func (s *memBuildStore) MarkSucceeded(ctx context.Context, id uuid.UUID, artifactPath string, inputDigests json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[id].Status = model.BuildStatusSucceeded
	s.rows[id].ArtifactPath = artifactPath
	return nil
}

func (s *memBuildStore) MarkFailed(ctx context.Context, id uuid.UUID, failure model.BuildFailure) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[id].Status = model.BuildStatusFailed
	s.rows[id].Failure = &failure
	return nil
}

func (s *memBuildStore) RecoverInterrupted(ctx context.Context) (int64, error) { return 0, nil }
func (s *memBuildStore) ListQueued(ctx context.Context) ([]uuid.UUID, error)   { return nil, nil }

func (s *memBuildStore) UpdateSourceDigest(ctx context.Context, id uuid.UUID, aggregateDigest string, fileCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.digests[id] = [2]any{aggregateDigest, fileCount}
	return nil
}

// memThemeStore serves fixed rows by id.
type memThemeStore struct{ row model.Theme }

func (s memThemeStore) GetByRowID(ctx context.Context, rowID uuid.UUID) (model.Theme, error) {
	if rowID == s.row.ID {
		return s.row, nil
	}
	return model.Theme{}, os.ErrNotExist
}

// writeFixtureTheme materializes a minimal contract-valid theme whose
// prepare and build scripts are instant Node scripts: prepare writes the
// snapshot staging tree (public + private/input.json), build writes
// build/index.html carrying a marker so cross-task contamination is visible.
func writeFixtureTheme(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"theme.json":   `{"contract":"fuyao.theme.manifest/v1","id":"fixture-theme","version":"1.0.0","name":"Fixture","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["blog","favicon"],"outputs":{"spaFallback":"200.html","urlConventions":["posts","blog-rss"],"requiredArtifacts":[]}}`,
		"package.json": `{"name":"fixture-theme","version":"1.0.0","private":true,"scripts":{"build":"node build.js"}}`,
		"build.js": `const fs=require('fs');
// The marker comes from this task's own staged input (one level above the
// theme copy), so two tasks over one shared source must produce different
// outputs — contamination would swap them.
const overrides=JSON.parse(fs.readFileSync('../site-overrides.json','utf8'));
fs.mkdirSync('build',{recursive:true});
fs.writeFileSync('build/index.html','<html>built:'+overrides.site.name+'</html>');
fs.writeFileSync('build/200.html','<html>fallback</html>');
`,
		"scripts/prepare-build-inputs.js": `const fs=require('fs');
for (const p of process.argv.slice(2)) if (!p.startsWith('--')) throw new Error('unexpected arg '+p);
fs.mkdirSync('.fuyao/build-inputs/current/public/posts',{recursive:true});
fs.mkdirSync('.fuyao/build-inputs/current/private',{recursive:true});
fs.writeFileSync('.fuyao/build-inputs/current/public/index.html','<html>snapshot</html>');
fs.writeFileSync('.fuyao/build-inputs/current/private/input.json',JSON.stringify({contract:'fuyao.theme.build-input/v1',theme:{id:'fixture-theme',version:'1.0.0'},inputs:{contentDigest:'sha256:x',siteConfigDigest:'sha256:y',themeConfigDigest:'sha256:z'}}));
`,
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
	// Pre-existing shared working state in the registered source: a task
	// must leave it exactly as found.
	if err := os.MkdirAll(filepath.Join(root, ".fuyao", "build-inputs", "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".fuyao", "build-context.json"), []byte(`{"sentinel":"pre-existing"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fixtureRunner wires a Runner over the in-memory stores and a fixture
// theme; marker rides the (scrubbed-plus-explicit) environment into build.
func fixtureRunner(t *testing.T, buildsRoot string, store BuildStore, theme model.Theme) *Runner {
	t.Helper()
	return NewRunner(Config{
		BuildStore: store,
		ThemeStore: memThemeStore{row: theme},
		RepoRoot:   t.TempDir(),
		BuildsRoot: buildsRoot,
		Mode:       "ci",
		Requirements: SiteRequirements{
			Capabilities:   []string{"blog"},
			URLConventions: []string{"posts", "blog-rss"},
		},
		BuildTimeout: 30 * time.Second,
	})
}

// TestWorkspaceIsolationConcurrent runs two tasks for the same registered
// theme on two independent Runners (no shared advisory lock — the in-memory
// stores bypass the pool) and proves: (1) both produce their own artifacts
// with no cross-contamination, (2) the registered source — including its
// pre-existing shared .fuyao state and build-context — is byte-identical
// afterwards, (3) each task records a full source-copy digest distinct from
// the manifest digest.
func TestWorkspaceIsolationConcurrent(t *testing.T) {
	themeRoot := t.TempDir()
	writeFixtureTheme(t, themeRoot)
	buildsRoot := t.TempDir()
	t.Cleanup(func() { _ = MakeTreeWritable(buildsRoot) }) // frozen artifacts block TempDir cleanup

	manifestBytes, err := os.ReadFile(filepath.Join(themeRoot, "theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	theme := model.Theme{
		ID:             uuid.New(),
		ThemeID:        "fixture-theme",
		Version:        "1.0.0",
		Adapter:        "node22-sveltekit-static",
		Manifest:       manifestBytes,
		ManifestDigest: sha256FixtureDigest(manifestBytes),
		SourcePath:     themeRoot,
	}

	baselineContext, err := os.ReadFile(filepath.Join(themeRoot, ".fuyao", "build-context.json"))
	if err != nil {
		t.Fatal(err)
	}
	baselineSource, err := TreeDigest(themeRoot)
	if err != nil {
		t.Fatal(err)
	}

	buildA := model.Build{ID: uuid.New(), SiteID: uuid.New(), RequestedBy: uuid.New(),
		ThemeRowID: theme.ID, ThemeID: theme.ThemeID, ThemeVersion: theme.Version,
		ManifestDigest: theme.ManifestDigest, ThemeConfig: json.RawMessage(`{}`),
		SiteSettings: json.RawMessage(`{"site":{"name":"A","description":"","defaultLocale":"zh-CN"}}`),
		Status:       model.BuildStatusQueued}
	buildB := buildA
	buildB.ID = uuid.New()
	buildB.SiteSettings = json.RawMessage(`{"site":{"name":"B","description":"","defaultLocale":"zh-CN"}}`)

	storeA := newMemBuildStore(buildA)
	storeB := newMemBuildStore(buildB)

	// Two Runners with no pool: no advisory lock, no cross-runner
	// serialization — only the per-task working copies keep them apart.
	runnerA := fixtureRunner(t, filepath.Join(buildsRoot, "a"), storeA, theme)
	runnerB := fixtureRunner(t, filepath.Join(buildsRoot, "b"), storeB, theme)

	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results[0] = runForTest(runnerA, buildA) }()
	go func() { defer wg.Done(); results[1] = runForTest(runnerB, buildB) }()
	wg.Wait()
	for i, err := range results {
		if err != nil {
			t.Fatalf("task %d failed: %v", i, err)
		}
	}

	// Both artifacts exist and were produced by their own build steps
	// inside their own task directories.
	for _, task := range []struct {
		id     uuid.UUID
		root   string
		marker string
	}{
		{buildA.ID, filepath.Join(buildsRoot, "a"), "A"},
		{buildB.ID, filepath.Join(buildsRoot, "b"), "B"},
	} {
		page, err := os.ReadFile(filepath.Join(task.root, task.id.String(), "artifacts", "index.html"))
		if err != nil || string(page) != "<html>built:"+task.marker+"</html>" {
			t.Fatalf("task %s artifacts wrong (want marker %s): err=%v page=%q", task.id, task.marker, err, page)
		}
		if _, err := os.Stat(filepath.Join(task.root, task.id.String(), "snapshot", "private", "input.json")); err != nil {
			t.Fatalf("task %s snapshot missing: %v", task.id, err)
		}
		manifestDoc, err := os.ReadFile(filepath.Join(task.root, task.id.String(), "source-manifest.json"))
		if err != nil {
			t.Fatalf("task %s source manifest missing: %v", task.id, err)
		}
		var sm sourceManifest
		if err := json.Unmarshal(manifestDoc, &sm); err != nil {
			t.Fatal(err)
		}
		if sm.Aggregate == "" || sm.FileCount < 4 || sm.Aggregate == hex.EncodeToString(theme.ManifestDigest) {
			t.Fatalf("task %s source manifest is not a full-tree identity", task.id)
		}
		if sm.Files["package.json"] == "" || sm.Files["scripts/prepare-build-inputs.js"] == "" {
			t.Fatalf("task %s source manifest missing per-file entries", task.id)
		}
	}

	// The registered source is untouched, including the pre-existing shared
	// .fuyao state: no task wrote, moved, or restored anything there.
	afterContext, err := os.ReadFile(filepath.Join(themeRoot, ".fuyao", "build-context.json"))
	if err != nil || string(afterContext) != string(baselineContext) {
		t.Fatalf("shared build-context.json changed: %q vs %q", afterContext, baselineContext)
	}
	afterSource, err := TreeDigest(themeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if afterSource != baselineSource {
		t.Fatal("registered theme source tree changed across concurrent tasks")
	}

	// Both stores saw the full source digest recorded.
	for _, store := range []*memBuildStore{storeA, storeB} {
		if len(store.digests) != 1 {
			t.Fatalf("source digest not recorded: %+v", store.digests)
		}
	}
}

// runForTest drives one build through the runner's internal execution path
// (claim + run) without the worker loop.
func runForTest(r *Runner, build model.Build) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.BuildTimeout)
	defer cancel()
	claimed, err := r.cfg.BuildStore.MarkRunning(ctx, build.ID)
	if err != nil || !claimed {
		return err
	}
	return r.runBuild(ctx, build)
}

// sha256FixtureDigest returns raw digest bytes for model.Theme fields.
func sha256FixtureDigest(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
