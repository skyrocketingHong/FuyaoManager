package contentbind

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func selectPlatform(s *store.ContentSources, category string) {
	switch category {
	case "posts":
		s.Posts = "platform"
	case "albums":
		s.Albums = "platform"
	case "friends":
		s.Friends = "platform"
	case "footprints":
		s.Footprints = "platform"
	}
}
func fixtureBytes(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}

func TestMaterializeFreezesEachSelectedCategoryAndKeepsSiteConfiguration(t *testing.T) {
	paths := map[string]string{"posts": "posts/tech/source.md", "albums": "albums/metadata/index.json", "friends": "data/friends.yaml", "footprints": "data/footprints.yaml"}
	for _, selected := range []string{"none", "posts", "albums", "friends", "footprints", "all"} {
		t.Run(selected, func(t *testing.T) {
			id := uuid.New()
			local := bindingFixture(t, id, "local", 2)
			library := bindingFixture(t, content.PlatformScopeID, "library", 5)
			sources := store.IndependentSources()
			for category := range paths {
				if selected == category || selected == "all" {
					selectPlatform(&sources, category)
				}
			}
			target := filepath.Join(t.TempDir(), "private-task")
			contentRoot, configRoot, faviconRoot, err := Materialize(local, library, id, sources, target)
			if err != nil {
				t.Fatal(err)
			}
			if contentRoot != filepath.Join(target, "content") || configRoot != filepath.Join(target, "config") || faviconRoot != filepath.Join(configRoot, "favicon") {
				t.Fatal("materialization roots are not confined to the task")
			}
			for category, path := range paths {
				expected := filepath.Join(local.ContentRoot(id), path)
				if sources.Source(category) == "platform" {
					expected = filepath.Join(library.ContentRoot(content.PlatformScopeID), path)
				}
				if fixtureBytes(t, filepath.Join(contentRoot, path)) != fixtureBytes(t, expected) {
					t.Fatalf("%s came from the wrong source", category)
				}
			}
			if fixtureBytes(t, filepath.Join(configRoot, "site.yaml")) != fixtureBytes(t, filepath.Join(local.ConfigRoot(id), "site.yaml")) || fixtureBytes(t, filepath.Join(faviconRoot, "favicon.svg")) != "local" {
				t.Fatal("shared content replaced the site's configuration or favicon")
			}
			if fixtureBytes(t, filepath.Join(contentRoot, "images/common.txt")) != "local" {
				t.Fatal("platform asset overwrote a site-owned asset")
			}
			_, libraryAssetErr := os.Stat(filepath.Join(contentRoot, "images/library.txt"))
			if selected == "none" && !errors.Is(libraryAssetErr, os.ErrNotExist) {
				t.Fatal("independent snapshot imported library assets")
			}
			if selected != "none" && libraryAssetErr != nil {
				t.Fatal(libraryAssetErr)
			}
			post := filepath.Join(contentRoot, paths["posts"])
			frozen := fixtureBytes(t, post)
			writeFixture(t, filepath.Join(local.ContentRoot(id), paths["posts"]), "changed-local-after-snapshot")
			writeFixture(t, filepath.Join(library.ContentRoot(content.PlatformScopeID), paths["posts"]), "changed-platform-after-snapshot")
			if fixtureBytes(t, post) != frozen {
				t.Fatal("a source edit mutated the frozen input")
			}
			writeFixture(t, post, "theme-preparation-edit")
			if strings.Contains(fixtureBytes(t, filepath.Join(local.ContentRoot(id), paths["posts"])), "theme-preparation-edit") || strings.Contains(fixtureBytes(t, filepath.Join(library.ContentRoot(content.PlatformScopeID), paths["posts"])), "theme-preparation-edit") {
				t.Fatal("theme preparation wrote back to a source")
			}
		})
	}
}

func TestMaterializeExcludesPrivatePostVocabulary(t *testing.T) {
	id := uuid.New()
	local := bindingFixture(t, id, "local", 1)
	if _, _, err := local.SavePostVocabulary(id, content.PostVocabulary{Version: 1, Categories: []string{}, Tags: []string{"private-unused"}}, content.Digest(nil)); err != nil {
		t.Fatal(err)
	}
	root, _, _, err := Materialize(local, nil, id, store.IndependentSources(), filepath.Join(t.TempDir(), "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".post-vocabulary.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("editing vocabulary leaked into build input: %v", err)
	}
}

func TestMaterializeRejectsSymlinksAndRemovesPartialInput(t *testing.T) {
	for _, where := range []string{"local-content", "local-config", "platform-posts", "platform-assets", "content-root"} {
		t.Run(where, func(t *testing.T) {
			id := uuid.New()
			local := bindingFixture(t, id, "local", 2)
			library := bindingFixture(t, content.PlatformScopeID, "library", 5)
			outside := filepath.Join(t.TempDir(), "outside.txt")
			writeFixture(t, outside, "outside-data")
			sources := store.IndependentSources()
			sources.Posts = "platform"
			link := filepath.Join(local.ContentRoot(id), "escape.txt")
			switch where {
			case "local-config":
				link = filepath.Join(local.ConfigRoot(id), "escape.txt")
			case "platform-posts":
				link = filepath.Join(library.ContentRoot(content.PlatformScopeID), "posts", "escape.md")
			case "platform-assets":
				link = filepath.Join(library.ContentRoot(content.PlatformScopeID), "images", "escape.txt")
			case "content-root":
				link = local.ContentRoot(id)
				if err := os.RemoveAll(link); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "private-task")
			_, _, _, err := Materialize(local, library, id, sources, target)
			if !errors.Is(err, content.ErrInvalidPath) {
				t.Fatalf("got %v, want symlink rejection", err)
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("partial input survived: %v", err)
			}
			if fixtureBytes(t, outside) != "outside-data" {
				t.Fatal("copy touched data outside the input")
			}
		})
	}
}

func TestMaterializeRejectsInvalidSelectionMissingLibraryAndExistingTarget(t *testing.T) {
	id := uuid.New()
	local := bindingFixture(t, id, "local", 2)
	for _, mode := range []string{"invalid", "missing-platform", "missing-category", "existing-target"} {
		t.Run(mode, func(t *testing.T) {
			sources := store.IndependentSources()
			library := bindingFixture(t, content.PlatformScopeID, "library", 5)
			target := filepath.Join(t.TempDir(), "private-task")
			switch mode {
			case "invalid":
				sources.Posts = "unknown"
			case "missing-platform":
				sources.Posts = "platform"
				library = nil
			case "missing-category":
				sources.Posts = "platform"
				if err := os.RemoveAll(filepath.Join(library.ContentRoot(content.PlatformScopeID), "posts")); err != nil {
					t.Fatal(err)
				}
			case "existing-target":
				writeFixture(t, filepath.Join(target, "keep.txt"), "existing-task")
			}
			if _, _, _, err := Materialize(local, library, id, sources, target); err == nil {
				t.Fatal("invalid input was accepted")
			}
			if mode == "existing-target" {
				if fixtureBytes(t, filepath.Join(target, "keep.txt")) != "existing-task" {
					t.Fatal("existing task was overwritten")
				}
			} else if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed input was retained: %v", err)
			}
		})
	}
}
