package edge

// Run-isolation regression at the manager level: every mutating entry point
// rejects with ErrDeploymentDisabled before touching disk, commands, or the
// database — even when a snippet directory is fully configured. Direct calls
// that bypass the CLI/API must not be able to write pointers or snippets.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// TestIsolatedManagerRejectsEveryMutatingEntry proves an isolated manager
// with a configured snippet directory performs zero side effects: OnLive,
// Reconcile, and ApplyAll return ErrDeploymentDisabled, the apply command
// never runs, no snippet file appears, and the release root stays untouched.
func TestIsolatedManagerRejectsEveryMutatingEntry(t *testing.T) {
	tmp := t.TempDir()
	snippetDir := filepath.Join(tmp, "snippets")
	releasesRoot := filepath.Join(tmp, "releases")
	for _, dir := range []string{snippetDir, releasesRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := filepath.Join(tmp, "apply-ran")
	m := New(Config{
		// Fully configured: snippet management reports enabled while the
		// deployment switch keeps the manager isolated.
		SnippetDir:        snippetDir,
		ApplyCmd:          []string{"touch", sentinel},
		ReleasesRoot:      releasesRoot,
		DeploymentEnabled: false,
	})
	if !m.Enabled() {
		t.Fatal("the snippet directory must still report snippet management as configured")
	}
	if !m.Isolated() {
		t.Fatal("the off switch must report run isolation")
	}

	ctx := context.Background()
	release := model.Release{ID: uuid.New(), ReleaseDir: "release-1"}

	if err := m.OnLive(ctx, uuid.New(), release); !errors.Is(err, ErrDeploymentDisabled) {
		t.Fatalf("OnLive: err=%v, want ErrDeploymentDisabled", err)
	}
	if err := m.Reconcile(ctx); !errors.Is(err, ErrDeploymentDisabled) {
		t.Fatalf("Reconcile: err=%v, want ErrDeploymentDisabled", err)
	}
	if err := m.ApplyAll(ctx); !errors.Is(err, ErrDeploymentDisabled) {
		t.Fatalf("ApplyAll: err=%v, want ErrDeploymentDisabled", err)
	}

	// Zero side effects: the apply command never ran, the snippet directory
	// holds no file, and no pointer structure was created under the release
	// root (switchCurrentLocked would mkdir the site root).
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("the apply command must never run in isolation")
	}
	if entries, err := os.ReadDir(snippetDir); err != nil || len(entries) != 0 {
		t.Fatalf("snippet directory must stay empty: entries=%d err=%v", len(entries), err)
	}
	if entries, err := os.ReadDir(releasesRoot); err != nil || len(entries) != 0 {
		t.Fatalf("release root must stay untouched: entries=%d err=%v", len(entries), err)
	}
}
