package contentbind

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func writeFixture(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0640); err != nil {
		t.Fatal(err)
	}
}
func bindingFixture(t *testing.T, id uuid.UUID, label string, photos int) *content.Store {
	t.Helper()
	s, err := content.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	post := content.Post{Category: "tech", Slug: "source", Title: label, Date: "2026-11-13", Body: label}
	raw, err := post.Render()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"posts/tech/source.md":         string(raw),
		"data/friends.yaml":            "online:\n  - name: " + label + "\n    url: https://friend.example\noffline: []\n",
		"data/footprints.yaml":         "cities:\n  - id: " + label + "\n    name: {zh: 城市, en: City}\n    coordinates: {lng: 116.4, lat: 39.9}\n    type: city\n    visitDate: '20261113'\n",
		"albums/metadata/index.json":   `{"schemaVersion":1,"years":[2026],"devices":[],"totalPhotos":` + strconv.Itoa(photos) + `}`,
		"images/common.txt":            label,
		"images/" + label + ".txt":     label,
		"wp-content/" + label + ".txt": label,
	} {
		writeFixture(t, filepath.Join(s.ContentRoot(id), name), value)
	}
	writeFixture(t, filepath.Join(s.ConfigRoot(id), "site.yaml"), "site:\n  name: "+label+"\n")
	writeFixture(t, filepath.Join(s.FaviconRoot(id), "favicon.svg"), label)
	return s
}

// TestStoreGetPostTaxonomyFollowsPostsSource proves the taxonomy read is
// explicitly bound to the posts source: an independent site reads its own
// directories and a platform-sourced site reads the library — never the
// embedded store's own tree by method promotion.
func TestStoreGetPostTaxonomyFollowsPostsSource(t *testing.T) {
	id := uuid.New()
	local := bindingFixture(t, id, "local", 0)
	library := bindingFixture(t, content.PlatformScopeID, "library", 0)
	resolve := func(sources store.ContentSources) func(context.Context, uuid.UUID) (store.ContentSourceRecord, error) {
		return func(_ context.Context, got uuid.UUID) (store.ContentSourceRecord, error) {
			if got != id {
				t.Fatalf("resolved wrong site: %s", got)
			}
			return store.ContentSourceRecord{Sources: sources, Revision: 1}, nil
		}
	}

	bound := &Store{Store: local, Platform: library, Resolve: resolve(store.IndependentSources())}
	independent, err := bound.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if independent.PostCount != 1 {
		t.Fatalf("independent taxonomy must read the local tree: %+v", independent)
	}
	if len(independent.Directories) != 1 || independent.Directories[0].Slug != "tech" {
		t.Fatalf("independent directories must come from the local tree: %+v", independent.Directories)
	}

	bound.Resolve = resolve(store.ContentSources{Posts: "platform"})
	shared, err := bound.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if shared.PostCount != 1 {
		t.Fatalf("platform-sourced taxonomy must read the library: %+v", shared)
	}
	if len(shared.Directories) != 1 || shared.Directories[0].Slug != "tech" {
		t.Fatalf("platform directories must come from the library: %+v", shared.Directories)
	}
}

func TestStoreResolvesCategoriesWithoutSharingSiteConfiguration(t *testing.T) {
	id := uuid.New()
	local := bindingFixture(t, id, "local", 2)
	library := bindingFixture(t, content.PlatformScopeID, "library", 5)
	sources := store.IndependentSources()
	sources.Posts, sources.Albums = "platform", "platform"
	bound := &Store{Store: local, Platform: library, Resolve: func(_ context.Context, got uuid.UUID) (store.ContentSourceRecord, error) {
		if got != id {
			t.Fatalf("resolved wrong site: %s", got)
		}
		return store.ContentSourceRecord{Sources: sources, Revision: 3}, nil
	}}
	posts, total, err := bound.ListPosts(id, "", 1, 10)
	if err != nil || total != 1 || posts[0].Title != "library" {
		t.Fatalf("posts: %v %+v", err, posts)
	}
	album, _, err := bound.GetAlbumIndex(id)
	if err != nil || album.TotalPhotos != 5 {
		t.Fatalf("albums: %v %+v", err, album)
	}
	friends, _, err := bound.GetFriends(id)
	if err != nil || friends.Online[0].Name != "local" {
		t.Fatalf("friends: %v %+v", err, friends)
	}
	places, _, err := bound.GetFootprints(id)
	if err != nil || places.Cities[0].ID != "local" {
		t.Fatalf("places: %v %+v", err, places)
	}
	raw, _, err := bound.ReadFile(id, content.RootConfig, "site.yaml")
	if err != nil || !strings.Contains(string(raw), "local") {
		t.Fatalf("configuration must remain local: %s %v", raw, err)
	}
	// Changing the binding never copies or removes the independent document.
	sources.Posts = "independent"
	post, err := bound.GetPost(id, "tech/source.md")
	if err != nil {
		t.Fatal(err)
	}
	post.Title = "edited-local"
	if _, err = bound.SavePost(id, post, post.Digest); err != nil {
		t.Fatal(err)
	}
	sources.Posts = "platform"
	post, err = bound.GetPost(id, "tech/source.md")
	if err != nil || post.Title != "library" {
		t.Fatalf("shared post: %v %+v", err, post)
	}
	independent, err := local.GetPost(id, "tech/source.md")
	if err != nil || independent.Title != "edited-local" {
		t.Fatalf("independent content lost: %v %+v", err, independent)
	}
}

type unreadableUpload struct{ consumed bool }

func (r *unreadableUpload) Read([]byte) (int, error) { r.consumed = true; return 0, io.EOF }

func TestStoreRejectsEverySharedWriteBeforeTouchingContent(t *testing.T) {
	id := uuid.New()
	local := bindingFixture(t, id, "local", 2)
	library := bindingFixture(t, content.PlatformScopeID, "library", 5)
	bound := &Store{Store: local, Platform: library, Resolve: func(context.Context, uuid.UUID) (store.ContentSourceRecord, error) {
		return store.ContentSourceRecord{Sources: store.ContentSources{Posts: "platform", Albums: "platform", Friends: "platform", Footprints: "platform"}}, nil
	}}
	upload := &unreadableUpload{}
	editCalled := false
	cases := map[string]func() error{
		"create post": func() error { _, err := bound.CreatePost(id, content.Post{}); return err },
		"save post":   func() error { _, err := bound.SavePost(id, content.Post{}, "stale"); return err },
		"delete post": func() error { return bound.DeletePost(id, "tech/source.md", "stale") },
		"friends":     func() error { _, _, err := bound.SaveFriends(id, content.FriendsDoc{}, "stale"); return err },
		"places":      func() error { _, _, err := bound.SaveFootprints(id, content.FootprintsDoc{}, "stale"); return err },
		"album metadata": func() error {
			_, _, err := bound.SaveAlbumYear(id, "2026", "stale", func(*content.AlbumYear) error { editCalled = true; return nil })
			return err
		},
		"upload":       func() error { _, err := bound.StoreUpload(id, upload, 0); return err },
		"delete photo": func() error { return bound.DeleteAlbumPhoto(id, "2026", "photo.jpg", nil, "stale") },
		"detach photo": func() error { return bound.DetachAlbumPhoto(id, "2026", "photo.jpg", "stale") },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, content.ErrShared) {
				t.Fatalf("got %v, want shared-source rejection", err)
			}
		})
	}
	if upload.consumed {
		t.Fatal("shared upload consumed request bytes")
	}
	if editCalled {
		t.Fatal("shared album edit callback ran")
	}
	for _, category := range []string{"posts", "albums", "friends", "footprints"} {
		if err := bound.RequireContentWrite(t.Context(), id, category); !errors.Is(err, content.ErrShared) {
			t.Fatalf("%s write guard: %v", category, err)
		}
	}
	for _, pair := range []struct {
		s     *content.Store
		id    uuid.UUID
		title string
	}{{local, id, "local"}, {library, content.PlatformScopeID, "library"}} {
		post, err := pair.s.GetPost(pair.id, "tech/source.md")
		if err != nil || post.Title != pair.title {
			t.Fatalf("shared rejection changed content: %v %+v", err, post)
		}
	}
}

func TestStorePropagatesResolverErrorsAndMissingPlatform(t *testing.T) {
	id := uuid.New()
	local := bindingFixture(t, id, "local", 2)
	failure := errors.New("binding store unavailable")
	bound := &Store{Store: local, Resolve: func(context.Context, uuid.UUID) (store.ContentSourceRecord, error) {
		return store.ContentSourceRecord{}, failure
	}}
	if _, _, err := bound.ListPosts(id, "", 1, 10); !errors.Is(err, failure) {
		t.Fatalf("read error: %v", err)
	}
	if err := bound.RequireContentWrite(t.Context(), id, "posts"); !errors.Is(err, failure) {
		t.Fatalf("write error: %v", err)
	}
	bound.Resolve = func(context.Context, uuid.UUID) (store.ContentSourceRecord, error) {
		selected := store.IndependentSources()
		selected.Posts = "platform"
		return store.ContentSourceRecord{Sources: selected}, nil
	}
	if _, err := bound.GetPost(id, "tech/source.md"); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("missing platform must not fall back to local: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	bound.Resolve = func(ctx context.Context, _ uuid.UUID) (store.ContentSourceRecord, error) {
		return store.ContentSourceRecord{}, ctx.Err()
	}
	if err := bound.RequireContentWrite(ctx, id, "posts"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost caller cancellation: %v", err)
	}
}
