package buildrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
)

// writeDependencyFixture materializes a theme source whose build output
// embeds what its OWN node_modules/depmarker resolves to — proving module
// lookup follows the registered source, not the task root's ancestry. A
// synthetic .env/.npmrc sentinel must never reach the copy.
func writeDependencyFixture(t *testing.T, root, marker string) {
	t.Helper()
	for rel, body := range map[string]string{
		"theme.json":                          `{"contract":"fuyao.theme.manifest/v1","id":"dep-theme","version":"1.0.0","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["blog"],"outputs":{"spaFallback":"200.html","urlConventions":["posts"],"requiredArtifacts":[]}}`,
		"package.json":                        `{"name":"dep-theme","version":"1.0.0","private":true,"scripts":{"build":"node build.js"}}`,
		"build.js":                            "const fs=require('fs');const m=require('depmarker');\nfs.mkdirSync('build',{recursive:true});\nfs.writeFileSync('build/index.html','<html>dep:'+m()+'</html>');\nfs.writeFileSync('build/200.html','x');\n",
		"scripts/prepare-build-inputs.js":     "const fs=require('fs');fs.mkdirSync('.fuyao/build-inputs/current/public',{recursive:true});fs.mkdirSync('.fuyao/build-inputs/current/private',{recursive:true});fs.writeFileSync('.fuyao/build-inputs/current/public/index.html','<html>s</html>');fs.writeFileSync('.fuyao/build-inputs/current/private/input.json',JSON.stringify({contract:'fuyao.theme.build-input/v1',theme:{id:'dep-theme',version:'1.0.0'},inputs:{contentDigest:'sha256:x',siteConfigDigest:'sha256:y',themeConfigDigest:'sha256:z'}}));\n",
		"node_modules/depmarker/index.js":     "module.exports=function(){return '" + marker + "';};\n",
		"node_modules/depmarker/package.json": `{"name":"depmarker","version":"1.0.0","main":"index.js"}`,
		".env":                                "SECRET=SENTINEL_ENV_" + marker + "\n",
		".npmrc":                              "//registry.npmjs.org/:_authToken=SENTINEL_NPMRC_" + marker + "\n",
		"nested/.env.local":                   "SECRET=SENTINEL_NESTED_ENV_" + marker + "\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDependencyResolutionFromSource runs two themes whose same-named
// depmarker module differs per source, with the BuildsRoot in a temp
// directory that is NOT an ancestor of either source (and contains a
// decoy depmarker of its own). Each build must embed its own source's
// marker, proving resolution is anchored at the registered source — and
// the sentinel private files must never appear in any copy, manifest, or
// artifact.
func TestDependencyResolutionFromSource(t *testing.T) {
	buildsRoot := t.TempDir() // not under either source; other volume than repo
	t.Cleanup(func() { _ = MakeTreeWritable(buildsRoot) })

	// A decoy marker that must never be picked up.
	if err := os.MkdirAll(filepath.Join(buildsRoot, "node_modules", "depmarker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildsRoot, "node_modules", "depmarker", "index.js"),
		[]byte("module.exports=function(){return 'DECOY';};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildsRoot, "package.json"),
		[]byte(`{"name":"decoy-root","workspaces":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	builds := runTwoDependencyBuilds(t, buildsRoot)

	for _, task := range builds {
		page, err := os.ReadFile(filepath.Join(task.taskDir, "artifacts", "index.html"))
		if err != nil || string(page) != "<html>dep:"+task.marker+"</html>" {
			t.Fatalf("task %s resolved the wrong dependency (want %s): err=%v page=%q",
				task.buildID, task.marker, err, page)
		}
		assertNoSentinels(t, task.taskDir, task.marker)
	}
}

type depTask struct {
	buildID uuid.UUID
	taskDir string
	marker  string
}

// runTwoDependencyBuilds drives both builds (sequentially — isolation of
// dependency resolution is the target here, concurrency is covered by the
// workspace test) and returns their task directories.
func runTwoDependencyBuilds(t *testing.T, buildsRoot string) []depTask {
	t.Helper()
	var out []depTask
	for _, marker := range []string{"alpha", "beta"} {
		sourceRoot := t.TempDir()
		writeDependencyFixture(t, sourceRoot, marker)
		manifestBytes, err := os.ReadFile(filepath.Join(sourceRoot, "theme.json"))
		if err != nil {
			t.Fatal(err)
		}
		theme := model.Theme{
			ID: uuid.New(), ThemeID: "dep-theme", Version: "1.0.0",
			Adapter: "node22-sveltekit-static", Manifest: manifestBytes,
			ManifestDigest: sha256FixtureDigest(manifestBytes), SourcePath: sourceRoot,
		}
		build := model.Build{
			ID: uuid.New(), ThemeRowID: theme.ID, ThemeID: theme.ThemeID,
			ThemeVersion: theme.Version, ManifestDigest: theme.ManifestDigest,
			ThemeConfig:  json.RawMessage(`{}`),
			SiteSettings: json.RawMessage(`{"site":{"name":"` + marker + `","description":"","defaultLocale":"zh-CN"}}`),
			Status:       model.BuildStatusQueued,
		}
		store := newMemBuildStore(build)
		runner := NewRunner(Config{
			BuildStore: store, ThemeStore: memThemeStore{row: theme},
			RepoRoot: t.TempDir(), BuildsRoot: buildsRoot, Mode: "ci",
			Requirements: SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts"}},
			BuildTimeout: 30 * time.Second,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = runner.runBuild(ctx, build)
		cancel()
		if err != nil {
			t.Fatalf("build %s (%s) failed: %v", build.ID, marker, err)
		}
		taskDir := filepath.Join(buildsRoot, build.ID.String())
		out = append(out, depTask{buildID: build.ID, taskDir: taskDir, marker: marker})
		// Source baseline: the sentinel files stay in the source untouched.
		for _, rel := range []string{".env", ".npmrc", "nested/.env.local"} {
			data, readErr := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(rel)))
			if readErr != nil || !strings.Contains(string(data), "SENTINEL_") {
				t.Fatalf("source sentinel %s missing or clobbered", rel)
			}
		}
		// source-manifest.json must not carry the sensitive files among its
		// recorded file entries (the scope string legitimately names them),
		// and its manifestSha256 must equal the theme.json digest.
		manifestDoc, _ := os.ReadFile(filepath.Join(taskDir, "source-manifest.json"))
		var sm sourceManifest
		if err := json.Unmarshal(manifestDoc, &sm); err != nil {
			t.Fatal(err)
		}
		for path := range sm.Files {
			base := filepath.Base(path)
			if sensitiveFileName(base) {
				t.Fatalf("source manifest lists sensitive file %s", path)
			}
		}
		if sm.ManifestSHA != sm.Files["theme.json"] {
			t.Fatalf("manifestSha256 %q does not match theme.json digest %q", sm.ManifestSHA, sm.Files["theme.json"])
		}
	}
	return out
}

// assertNoSentinels walks a task directory and fails on any sentinel value
// from this task's source (or any sentinel file name at all).
func assertNoSentinels(t *testing.T, taskDir, marker string) {
	t.Helper()
	err := filepath.Walk(taskDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := filepath.Base(path)
		if sensitiveFileName(name) && info.Mode().IsRegular() {
			return filepath.SkipDir
		}
		if info.Mode().IsRegular() {
			data, readErr := os.ReadFile(path)
			if readErr == nil && strings.Contains(string(data), "SENTINEL_") {
				t.Fatalf("sentinel leaked into task at %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
