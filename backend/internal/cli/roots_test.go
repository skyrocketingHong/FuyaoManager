package cli

// Deterministic root resolution: discovery from a nested working directory,
// the verified explicit override, the loud failure when nothing is
// discoverable, and the mutual-independence check for the runtime roots.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepoTree creates a directory carrying this repository's markers and
// returns its path.
func makeRepoTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "contracts", "openapi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "contracts", "openapi", "openapi.yaml"), []byte("openapi: 3.0.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func TestResolveRepoRootDiscoversUpward(t *testing.T) {
	repo := makeRepoTree(t)
	// Starting from backend/ (as `go -C backend` does) must resolve the
	// repository root, not the backend directory. Both sides are normalized
	// through their real paths (macOS reports /var via /private/var).
	chdir(t, filepath.Join(repo, "backend"))
	resolved, err := resolveRepoRoot("")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("resolved %q, want %q", resolved, want)
	}
}

func TestResolveRepoRootExplicitOverride(t *testing.T) {
	repo := makeRepoTree(t)
	installation := t.TempDir() // a binary-only installation: no markers
	chdir(t, t.TempDir())

	// An explicit value is operator-owned: both the repository checkout and
	// a plain installation directory are accepted.
	for _, want := range []string{repo, installation} {
		resolved, err := resolveRepoRoot(want)
		if err != nil {
			t.Fatalf("explicit override %q: %v", want, err)
		}
		if resolved != want {
			t.Fatalf("explicit root = %q, want %q", resolved, want)
		}
	}
	// A missing path stays a configuration error.
	if _, err := resolveRepoRoot(filepath.Join(installation, "missing")); err == nil {
		t.Fatal("an explicit nonexistent path must be rejected")
	}
	// Automatic discovery is the only path where the markers are required.
	chdir(t, installation)
	if _, err := resolveRepoRoot(""); err == nil {
		t.Fatal("discovery without markers must fail loudly")
	}
}

func TestResolveRepoRootFailsLoudlyWithoutDiscovery(t *testing.T) {
	chdir(t, t.TempDir()) // no markers anywhere up the chain
	_, err := resolveRepoRoot("")
	if err == nil {
		t.Fatal("an undiscoverable root must fail loudly")
	}
	if !strings.Contains(err.Error(), "FUYAO_ADMIN_REPO_ROOT") {
		t.Fatalf("error must point at the configuration: %v", err)
	}
}

func TestEnsureDistinctRootsRejectsAliasesAndNesting(t *testing.T) {
	base := t.TempDir()
	builds := filepath.Join(base, "builds")
	sites := filepath.Join(base, "sites")
	releases := filepath.Join(base, "releases")
	for _, dir := range []string{builds, sites, releases} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	labels := []string{"sites", "builds", "releases"}
	if err := ensureDistinctRoots(labels, []string{sites, builds, releases}); err != nil {
		t.Fatalf("independent roots must pass: %v", err)
	}

	// Nested root: one subsystem inside another.
	nested := filepath.Join(sites, "builds")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureDistinctRoots(labels, []string{sites, nested, releases}); err == nil {
		t.Fatal("a nested root must be rejected")
	}

	// Symlink alias: two labels resolving to one real directory.
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(builds, alias); err != nil {
		t.Fatal(err)
	}
	if err := ensureDistinctRoots(labels, []string{builds, alias, releases}); err == nil {
		t.Fatal("two labels resolving to one directory must be rejected")
	}
}

// TestEnsureDistinctRootsResolvesMissingTailsBeforeCreation proves the
// overlap check runs on paths that do not exist yet (resolving each missing
// tail against its deepest existing ancestor) and never creates anything:
// a rejected configuration leaves the disk untouched.
func TestEnsureDistinctRootsResolvesMissingTailsBeforeCreation(t *testing.T) {
	data := t.TempDir()
	// Same missing tail: both labels resolve to the same future directory.
	same := filepath.Join(data, "shared")
	if err := ensureDistinctRoots([]string{"sites", "builds"}, []string{same, same}); err == nil {
		t.Fatal("identical missing roots must be rejected before creation")
	}
	// A missing root nested under another's missing tail.
	if err := ensureDistinctRoots(
		[]string{"sites", "builds"},
		[]string{filepath.Join(data, "sites"), filepath.Join(data, "sites", "builds")},
	); err == nil {
		t.Fatal("a nested missing root must be rejected before creation")
	}
	// Nothing was created by the failing checks.
	if _, err := os.Stat(same); !os.IsNotExist(err) {
		t.Fatal("the overlap check must not create directories")
	}
	if _, err := os.Stat(filepath.Join(data, "sites")); !os.IsNotExist(err) {
		t.Fatal("the overlap check must not create directories")
	}
}

// TestEnsureDistinctRootsMixesRelativeAndAbsolute proves a relative root and
// an absolute root pointing at the same directory are detected: resolveRealPath
// normalizes to absolute first, so a relative EvalSymlinks answer cannot
// hide the overlap.
func TestEnsureDistinctRootsMixesRelativeAndAbsolute(t *testing.T) {
	base := t.TempDir()
	sites := filepath.Join(base, "sites")
	if err := os.MkdirAll(sites, 0o755); err != nil {
		t.Fatal(err)
	}
	// The relative form exists (EvalSymlinks would answer it in relative
	// form, which never equals the absolute twin).
	chdir(t, base)
	if err := ensureDistinctRoots([]string{"sites", "builds"}, []string{"sites", sites}); err == nil {
		t.Fatal("a relative root aliasing an absolute one must be rejected")
	}
	// Same through redundant segments that Clean collapses.
	if err := ensureDistinctRoots(
		[]string{"sites", "builds"},
		[]string{filepath.Join(base, "other", "..", "sites"), sites},
	); err == nil {
		t.Fatal("a cleaned path aliasing an absolute root must be rejected")
	}
}

// TestEnsureDistinctRootsAllowsDotDotNamedSiblings proves an upward rel is
// only ".." or "../"-prefixed: a literal sibling directory named "..foo"
// inside the parent is an independent root, not an escape.
func TestEnsureDistinctRootsAllowsDotDotNamedSiblings(t *testing.T) {
	base := t.TempDir()
	dotFoo := filepath.Join(base, "..foo")
	builds := filepath.Join(base, "builds")
	releases := filepath.Join(base, "releases")
	for _, dir := range []string{dotFoo, builds, releases} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureDistinctRoots(
		[]string{"sites", "builds", "releases"},
		[]string{dotFoo, builds, releases},
	); err != nil {
		t.Fatalf("a sibling named ..foo must pass as an independent root: %v", err)
	}
	// As a missing tail too: the resolution keeps the literal name.
	if err := ensureDistinctRoots(
		[]string{"sites", "builds"},
		[]string{filepath.Join(base, "..foo", "nested"), builds},
	); err != nil {
		t.Fatalf("a missing tail under ..foo must not be misjudged: %v", err)
	}
}
