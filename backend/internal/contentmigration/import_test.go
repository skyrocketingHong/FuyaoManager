package contentmigration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

func migrationFixture(t *testing.T) Options {
	t.Helper()
	base := t.TempDir()
	options := Options{SourceConfig: filepath.Join(base, "source", "config"), SourceContent: filepath.Join(base, "source", "content"), SourceFavicon: filepath.Join(base, "source", "favicon"), PlatformRoot: filepath.Join(base, "platform"), SitesRoot: filepath.Join(base, "sites"), SiteID: uuid.New()}
	files := map[string]string{
		filepath.Join(options.SourceConfig, "site.yaml"):                         "schemaVersion: 1\nextension:\n  futureKey: preserve\n",
		filepath.Join(options.SourceConfig, "content.yaml"):                      "media:\n  mode: external\n  albumPublicBase: /media/albums\n",
		filepath.Join(options.SourceConfig, "deployment.env"):                    "private-env-data",
		filepath.Join(options.SourceContent, "posts", "tech", "real.md"):         "---\ntitle: real\n---\narticle",
		filepath.Join(options.SourceContent, "data", "friends.yaml"):             "online: []\noffline: []\n",
		filepath.Join(options.SourceContent, "albums", "photos", "original.jpg"): "real-media-bytes",
		filepath.Join(options.SourceContent, "state", "pending-deploy.json"):     "private-watcher-state",
		filepath.Join(options.SourceContent, "images", "legacy.png"):             "real-legacy-image",
		filepath.Join(options.SourceContent, "wp-content", "legacy.jpg"):         "real-wordpress-image",
		filepath.Join(options.SourceFavicon, "icon.svg"):                         "<svg/>",
	}
	for path, value := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return options
}

func TestImportCreatesIndependentAtomicScopesWithoutOperationalFiles(t *testing.T) {
	options := migrationFixture(t)
	report, err := Import(t.Context(), options)
	if err != nil || len(report.Targets) != 2 {
		t.Fatalf("import: %v", err)
	}
	if report.Targets[0].ScopeID != content.PlatformScopeID || report.Targets[1].ScopeID != options.SiteID || report.Targets[0].Digest != report.Targets[1].Digest {
		t.Fatal("scope identity or frozen copies differ")
	}
	original := filepath.Join(options.SourceContent, "posts", "tech", "real.md")
	sourceInfo, _ := os.Stat(original)
	for _, target := range report.Targets {
		copied := filepath.Join(target.Path, "content", "posts", "tech", "real.md")
		info, err := os.Stat(copied)
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(sourceInfo, info) {
			t.Fatal("import shares a mutable source inode")
		}
		for _, excluded := range []string{"content/state/pending-deploy.json", "config/deployment.env"} {
			if _, err := os.Stat(filepath.Join(target.Path, excluded)); !os.IsNotExist(err) {
				t.Fatal("operational source escaped import allowlist")
			}
		}
		cfg, err := os.ReadFile(filepath.Join(target.Path, "config", "site.yaml"))
		if err != nil || string(cfg) != "schemaVersion: 1\nextension:\n  futureKey: preserve\n" {
			t.Fatal("unknown site.yaml fields or original bytes changed")
		}
	}
	if err := os.WriteFile(filepath.Join(report.Targets[0].Path, "content/posts/tech/real.md"), []byte("edited platform copy"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(original)
	if string(data) == "edited platform copy" {
		t.Fatal("platform edit mutated live source")
	}
	data, _ = os.ReadFile(filepath.Join(report.Targets[1].Path, "content/posts/tech/real.md"))
	if string(data) == "edited platform copy" {
		t.Fatal("platform edit mutated independent site")
	}
	if _, err := Import(t.Context(), options); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("existing content was overwritten: %v", err)
	}
}

func TestImportRejectsLinksOverlapAndExistingEmptyDestination(t *testing.T) {
	t.Run("source-link", func(t *testing.T) {
		options := migrationFixture(t)
		if err := os.Symlink(options.SourceConfig, filepath.Join(options.SourceContent, "posts", "escape")); err != nil {
			t.Fatal(err)
		}
		if _, err := Import(t.Context(), options); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("symlink accepted: %v", err)
		}
		if _, err := os.Stat(filepath.Join(options.PlatformRoot, content.PlatformScopeID.String())); !os.IsNotExist(err) {
			t.Fatal("failed import left a visible scope")
		}
	})
	t.Run("overlap", func(t *testing.T) {
		options := migrationFixture(t)
		options.PlatformRoot = filepath.Join(options.SourceContent, "copy")
		if _, err := Import(t.Context(), options); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("overlap accepted: %v", err)
		}
	})
	t.Run("empty-destination", func(t *testing.T) {
		options := migrationFixture(t)
		if err := os.MkdirAll(filepath.Join(options.PlatformRoot, content.PlatformScopeID.String()), 0750); err != nil {
			t.Fatal(err)
		}
		if _, err := Import(t.Context(), options); !errors.Is(err, ErrDestinationExists) {
			t.Fatalf("empty existing destination replaced: %v", err)
		}
	})
}

func TestImportCancellationLeavesSourceAndDestinationsIntact(t *testing.T) {
	options := migrationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Import(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, path := range []string{filepath.Join(options.PlatformRoot, content.PlatformScopeID.String()), filepath.Join(options.SitesRoot, options.SiteID.String())} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("cancelled import left visible content")
		}
	}
}

func TestInitializePlatformRetainsExistingLibrary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "platform")
	if err := InitializePlatform(root); err != nil {
		t.Fatal(err)
	}
	library, err := content.New(root)
	if err != nil {
		t.Fatal(err)
	}
	posts, total, err := library.ListPosts(content.PlatformScopeID, "", 1, 20)
	if err != nil || len(posts) != 0 || total != 0 {
		t.Fatalf("empty library: %v", err)
	}
	if _, _, err := library.GetAlbumIndex(content.PlatformScopeID); err != nil {
		t.Fatal(err)
	}
	post, err := library.CreatePost(content.PlatformScopeID, content.Post{Category: "tech", Slug: "keep", Title: "Keep", Date: "2026-11-13", Body: "edited"})
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializePlatform(root); err != nil {
		t.Fatal(err)
	}
	saved, err := library.GetPost(content.PlatformScopeID, post.Path)
	if err != nil || saved.Digest != post.Digest {
		t.Fatal("initialization overwrote an existing library")
	}
}
