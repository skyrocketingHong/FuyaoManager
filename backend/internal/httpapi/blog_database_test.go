package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentbind"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func TestDatabaseBlogMigrationEditingAndFrozenBuild(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "database-blog")
	root := w.library.ContentRoot(content.PlatformScopeID)
	original := "---\ntitle: Legacy\nslug: old-identifier\ndate: '2026-01-02'\ndraft: false\ncategories: ['技术']\ntags: ['Beta 战士 👨‍🔬', 'Windows 10 Insider Preview 🐱‍👤', ' WWDC']\ncustom:\n  keep: [1, true]\n---\n\nBody stays.\n"
	workspaceWrite(t, filepath.Join(root, "posts/tech/legacy.md"), []byte(original))
	workspaceWrite(t, filepath.Join(root, "posts/tech/_index.md"), []byte("---\ntitle: 技术\ncustom: keep-index\n---\nindex body\n"))
	if _, _, err := w.library.SavePostVocabulary(content.PlatformScopeID, content.PostVocabulary{Version: 1, Categories: []string{}, Tags: []string{"Unused"}}, content.Digest(nil)); err != nil {
		t.Fatal(err)
	}
	if err := contentbind.InitializeDatabasePosts(context.Background(), w.env.pool, w.local, w.library, w.env.store.Blog, w.env.store.PostMedia); err != nil {
		t.Fatal(err)
	}

	posts, total, err := w.library.ListPosts(content.PlatformScopeID, "", 1, 20)
	if err != nil || total != 1 || len(posts) != 1 {
		t.Fatalf("imported posts: %d %+v %v", total, posts, err)
	}
	post := posts[0]
	if post.ID == "" || post.Path != post.ID || post.Slug != "old-identifier" || post.Digest != content.Digest([]byte(original)) {
		t.Fatalf("identity or digest not preserved: %+v", post)
	}
	if len(post.Categories) != 1 || post.Categories[0] != "tech" {
		t.Fatalf("category not unified: %+v", post.Categories)
	}
	taxonomy, err := w.library.GetPostTaxonomy(content.PlatformScopeID)
	if err != nil || len(taxonomy.CategoryGroups) != 1 || taxonomy.CategoryGroups[0].Title != "技术" || len(taxonomy.Tags) != 4 {
		t.Fatalf("taxonomy: %+v %v", taxonomy, err)
	}
	categoryHeader, _ := workspaceRequest(t, w.client, "GET", "/api/v1/content/posts/directories/tech", nil, "", 200)
	workspaceRequest(t, w.client, "DELETE", "/api/v1/content/posts/directories/tech", nil, categoryHeader.Get("ETag"), 409)
	duplicateHeader, _ := workspaceRequest(t, w.client, "GET", "/api/v1/content/posts/directories/duplicate", nil, "", 200)
	workspaceRequest(t, w.client, "PUT", "/api/v1/content/posts/directories/duplicate", map[string]string{"title": "技术"}, duplicateHeader.Get("ETag"), 409)
	emptyHeader, _ := workspaceRequest(t, w.client, "GET", "/api/v1/content/posts/directories/unused", nil, "", 200)
	createdCategory, _ := workspaceRequest(t, w.client, "PUT", "/api/v1/content/posts/directories/unused", map[string]string{"title": "Unused category"}, emptyHeader.Get("ETag"), 200)
	workspaceRequest(t, w.client, "DELETE", "/api/v1/content/posts/directories/unused", nil, emptyHeader.Get("ETag"), 412)
	workspaceRequest(t, w.client, "DELETE", "/api/v1/content/posts/directories/unused", nil, createdCategory.Get("ETag"), 204)

	// The database remains the source when every old article file is gone.
	if err := os.RemoveAll(filepath.Join(root, "posts")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".post-vocabulary.json")); err != nil {
		t.Fatal(err)
	}
	if got, err := w.library.GetPost(content.PlatformScopeID, "tech/legacy.md"); err != nil || got.ID != post.ID {
		t.Fatalf("legacy identifier: %+v %v", got, err)
	}

	getHeader, _ := workspaceRequest(t, w.client, "GET", "/api/v1/content/posts/"+post.ID, nil, "", 200)
	post.Slug = "new-identifier"
	post.Tags = []string{"Beta 战士 👨‍🔬", "Windows 10 Insider Preview 🐱‍👤", " WWDC", "新标签"}
	header, raw := workspaceRequest(t, w.client, "PUT", "/api/v1/content/posts/"+post.ID, post, getHeader.Get("ETag"), 200)
	var saved content.Post
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ID != post.ID || saved.Slug != "new-identifier" || len(saved.Tags) != 4 || saved.Tags[2] != " WWDC" || header.Get("ETag") == getHeader.Get("ETag") {
		t.Fatalf("updated post: %+v", saved)
	}
	workspaceRequest(t, w.client, "PUT", "/api/v1/content/posts/"+post.ID, post, getHeader.Get("ETag"), 412)
	other := post
	other.ID, other.Path, other.Slug = "", "", "new-identifier"
	workspaceRequest(t, w.client, "POST", "/api/v1/content/posts", other, "", 409)
	other.Slug = "second-post"
	other.Tags = []string{"Beta 战士 👨‍🔬"}
	workspaceRequest(t, w.client, "POST", "/api/v1/content/posts", other, "", 201)
	vocabularyHeader, vocabularyRaw := workspaceRequest(t, w.client, "GET", "/api/v1/content/posts/vocabulary", nil, "", 200)
	var vocabulary content.PostVocabulary
	if err := json.Unmarshal(vocabularyRaw, &vocabulary); err != nil {
		t.Fatal(err)
	}
	if !containsString(vocabulary.Tags, "新标签") {
		t.Fatal("new post tag was not registered atomically")
	}
	vocabularyETag := vocabularyHeader.Get("ETag")
	workspaceRequest(t, w.client, "POST", "/api/v1/content/posts/tags/rename", map[string]any{"from": "Beta 战士 👨‍🔬", "to": "Windows 10 Insider Preview 🐱‍👤"}, vocabularyETag, 409)
	workspaceRequest(t, w.client, "POST", "/api/v1/content/posts/tags/rename", map[string]any{"from": "Beta 战士 👨‍🔬"}, vocabularyETag, 400)
	unchangedHeader, _ := workspaceRequest(t, w.client, "GET", "/api/v1/content/posts/"+post.ID, nil, "", 200)
	if unchangedHeader.Get("ETag") != header.Get("ETag") {
		t.Fatal("rejected tag update changed an article")
	}
	for index, tag := range []string{"Beta 战士 👨‍🔬", "Windows 10 Insider Preview 🐱‍👤", " WWDC"} {
		next := tag + " revised"
		renamedHeader, result := workspaceRequest(t, w.client, "POST", "/api/v1/content/posts/tags/rename", map[string]any{"from": tag, "to": next}, vocabularyETag, 200)
		var response struct {
			Changed int `json:"changed"`
		}
		if err := json.Unmarshal(result, &response); err != nil {
			t.Fatal(err)
		}
		want := 1
		if index == 0 {
			want = 2
		}
		if response.Changed != want {
			t.Fatalf("tag update count: %d want %d", response.Changed, want)
		}
		if index == 0 {
			workspaceRequest(t, w.client, "POST", "/api/v1/content/posts/tags/rename", map[string]any{"from": next, "to": tag}, vocabularyETag, 412)
		}
		vocabularyETag = renamedHeader.Get("ETag")
	}
	var updatedSource []byte
	if err := w.env.pool.QueryRow(context.Background(), `SELECT original FROM blog_posts WHERE scope_id=$1 AND id=$2`, content.PlatformScopeID, post.ID).Scan(&updatedSource); err != nil {
		t.Fatal(err)
	}
	projected, err := content.ParsePost("tech/new-identifier.md", updatedSource)
	if err != nil || projected.Slug != "new-identifier" || len(projected.Tags) != 4 || projected.Tags[0] != "Beta 战士 👨‍🔬 revised" || !strings.Contains(string(updatedSource), "custom:") {
		t.Fatalf("database source was not updated consistently: %+v %v", projected.Tags, err)
	}
	siteBase := "/api/v1/sites/" + site.Site.ID.String()
	sourcesDoc, sourceETag := workspaceSources(t, w.client, site.Site.ID)
	sourcesDoc.Sources.Posts = "platform"
	workspaceRequest(t, w.client, "PUT", siteBase+"/content-sources", sourcesDoc, sourceETag, 200)
	_, shared := workspaceRequest(t, w.client, "GET", siteBase+"/posts/taxonomy", nil, "", 200)
	var sharedTaxonomy struct {
		CanEdit bool `json:"canEdit"`
	}
	if err := json.Unmarshal(shared, &sharedTaxonomy); err != nil || sharedTaxonomy.CanEdit {
		t.Fatalf("shared scope write access: %+v %v", sharedTaxonomy, err)
	}
	workspaceRequest(t, w.client, "POST", siteBase+"/posts/tags/rename", map[string]any{"from": "新标签", "to": "blocked"}, vocabularyETag, 409)

	// Re-running the import never revives stale files or changes the database.
	workspaceWrite(t, filepath.Join(root, "posts/tech/legacy.md"), []byte(original))
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); err != nil {
		t.Fatal(err)
	}
	if got, err := w.library.GetPost(content.PlatformScopeID, post.ID); err != nil || got.Slug != "new-identifier" {
		t.Fatalf("re-import changed database: %+v %v", got, err)
	}

	sources := store.IndependentSources()
	sources.Posts = "platform"
	contentRoot, _, _, err := contentbind.MaterializeWithMedia(context.Background(), w.local, w.library, w.env.store.PostMedia, site.Site.ID, sources, filepath.Join(t.TempDir(), "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join(contentRoot, "posts/tech/new-identifier.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(file), "keep: [1, true]") && !strings.Contains(string(file), "keep:\n") {
		t.Fatalf("unknown frontmatter not preserved: %s", file)
	}
	if !strings.Contains(string(file), "Body stays.") || !strings.Contains(string(file), "新标签") {
		t.Fatalf("snapshot content incomplete: %s", file)
	}
	index, err := os.ReadFile(filepath.Join(contentRoot, "posts/tech/_index.md"))
	if err != nil || !strings.Contains(string(index), "keep-index") || !strings.Contains(string(index), "index body") {
		t.Fatalf("category index not preserved: %s %v", index, err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestDatabaseBlogBuildIgnoresChangedLegacyMedia(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "database-blog-media")
	localRoot := w.local.ContentRoot(site.Site.ID)
	platformRoot := w.library.ContentRoot(content.PlatformScopeID)
	paths := []struct {
		root string
		path string
	}{
		{localRoot, "wp-content/uploads/local.jpg"},
		{localRoot, "images/local.jpg"},
		{platformRoot, "wp-content/uploads/platform.jpg"},
		{platformRoot, "images/platform.jpg"},
	}
	for _, item := range paths {
		workspaceWrite(t, filepath.Join(item.root, item.path), tinyJPEGBytes)
	}
	workspaceWrite(t, filepath.Join(localRoot, "wp-content/uploads/notes.pdf"), []byte("other content"))
	workspaceWrite(t, filepath.Join(localRoot, "images/other.svg"), []byte("other content"))
	if err := contentbind.InitializeDatabasePosts(context.Background(), w.env.pool, w.local, w.library, w.env.store.Blog, w.env.store.PostMedia); err != nil {
		t.Fatal(err)
	}
	changed := append(append([]byte{}, tinyJPEGBytes...), 1)
	for _, item := range paths {
		workspaceWrite(t, filepath.Join(item.root, item.path), changed)
	}
	for _, usePlatform := range []bool{false, true} {
		sources := store.IndependentSources()
		selected := paths[:2]
		if usePlatform {
			sources.Posts = "platform"
			selected = paths[2:]
		}
		root, _, _, err := contentbind.MaterializeWithMedia(t.Context(), w.local, w.library, w.env.store.PostMedia, site.Site.ID, sources, filepath.Join(t.TempDir(), "snapshot"))
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range selected {
			got, err := os.ReadFile(filepath.Join(root, item.path))
			if err != nil || !bytes.Equal(got, tinyJPEGBytes) {
				t.Fatalf("selected database media was not exported: %s: %v", item.path, err)
			}
		}
		for _, path := range []string{"wp-content/uploads/notes.pdf", "images/other.svg"} {
			if got, err := os.ReadFile(filepath.Join(root, path)); err != nil || string(got) != "other content" {
				t.Fatalf("unrelated content was not preserved: %s: %v", path, err)
			}
		}
	}
}

func TestDatabaseBlogImportRejectsDuplicatesWithoutPartialRows(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := w.library.ContentRoot(content.PlatformScopeID)
	first := "---\ntitle: First\nslug: duplicate\ndate: '2026-01-02'\n---\nfirst\n"
	second := "---\ntitle: Second\nslug: duplicate\ndate: '2026-01-03'\n---\nsecond\n"
	workspaceWrite(t, filepath.Join(root, "posts/a/first.md"), []byte(first))
	workspaceWrite(t, filepath.Join(root, "posts/b/second.md"), []byte(second))
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); err == nil || !strings.Contains(err.Error(), "duplicate post slug") {
		t.Fatalf("duplicate was not rejected: %v", err)
	}
	if imported, err := w.env.store.Blog.Imported(context.Background(), content.PlatformScopeID); err != nil || imported {
		t.Fatalf("partial import marker: %v %v", imported, err)
	}
	var count int
	if err := w.env.pool.QueryRow(context.Background(), `SELECT count(*) FROM blog_posts WHERE scope_id=$1`, content.PlatformScopeID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial rows: %d %v", count, err)
	}
	actual, err := os.ReadFile(filepath.Join(root, "posts/b/second.md"))
	if err != nil || string(actual) != second {
		t.Fatalf("source changed: %v", err)
	}
}

func TestDatabaseBlogImportRejectsUnsupportedResources(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := w.library.ContentRoot(content.PlatformScopeID)
	workspaceWrite(t, filepath.Join(root, "posts/tech/attachment.pdf"), []byte("legacy attachment"))
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); err == nil || !strings.Contains(err.Error(), "unsupported legacy blog resource") {
		t.Fatalf("unsupported resource was silently omitted: %v", err)
	}
	if imported, err := w.env.store.Blog.Imported(context.Background(), content.PlatformScopeID); err != nil || imported {
		t.Fatalf("partial import marker: %v %v", imported, err)
	}
}

func TestDatabaseBlogImportRejectsHiddenDirectories(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := w.library.ContentRoot(content.PlatformScopeID)
	workspaceWrite(t, filepath.Join(root, "posts/.drafts/hidden.md"), []byte("hidden article"))
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); err == nil || !strings.Contains(err.Error(), "unsupported hidden blog directory") {
		t.Fatalf("hidden directory was silently omitted: %v", err)
	}
	if imported, err := w.env.store.Blog.Imported(context.Background(), content.PlatformScopeID); err != nil || imported {
		t.Fatalf("partial import marker: %v %v", imported, err)
	}
}

func TestDatabaseBlogImportRejectsSymlinkedRoot(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := filepath.Join(w.library.ContentRoot(content.PlatformScopeID), "posts")
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); !errors.Is(err, content.ErrInvalidPath) {
		t.Fatalf("symlinked source was not rejected: %v", err)
	}
}

func TestDatabaseBlogImportRejectsConflictingMedia(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	path := "/wp-content/uploads/2020/image.jpg"
	if _, err := w.library.StorePostMedia(content.PlatformScopeID, path, bytes.NewReader(tinyJPEGBytes), int64(len(tinyJPEGBytes))); err != nil {
		t.Fatal(err)
	}
	different := append(append([]byte{}, tinyJPEGBytes...), 1)
	if err := w.env.store.PostMedia.Put(context.Background(), content.PlatformScopeID, path, different); err != nil {
		t.Fatal(err)
	}
	err := contentbind.InitializeDatabasePosts(context.Background(), w.env.pool, w.local, w.library, w.env.store.Blog, w.env.store.PostMedia)
	if err == nil || !strings.Contains(err.Error(), "conflicts with database") {
		t.Fatalf("conflicting media was not rejected: %v", err)
	}
	stored, _, err := w.env.store.PostMedia.Get(context.Background(), content.PlatformScopeID, path)
	if err != nil || !bytes.Equal(stored, different) {
		t.Fatalf("existing database media changed: %v", err)
	}
}

func TestDatabaseBlogConcurrentEditsUseETags(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := w.library.ContentRoot(content.PlatformScopeID)
	workspaceWrite(t, filepath.Join(root, "posts/tech/a.md"), []byte("---\ntitle: A\nslug: a\ndate: '2026-01-02'\n---\nbody\n"))
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); err != nil {
		t.Fatal(err)
	}
	w.library.Posts = w.env.store.Blog
	posts, _, err := w.library.ListPosts(content.PlatformScopeID, "", 1, 20)
	if err != nil || len(posts) != 1 {
		t.Fatalf("posts: %+v %v", posts, err)
	}
	original := posts[0]
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, tag := range []string{"Beta 战士 👨‍🔬", " WWDC"} {
		go func(tag string) {
			post := original
			post.Tags = []string{tag}
			<-start
			_, err := w.library.SavePost(content.PlatformScopeID, post, original.Digest)
			results <- err
		}(tag)
	}
	close(start)
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, content.ErrConflict)) || (second == nil && errors.Is(first, content.ErrConflict))) {
		t.Fatalf("concurrent updates: %v %v", first, second)
	}
	if err := w.library.DeletePost(content.PlatformScopeID, original.ID, original.Digest); !errors.Is(err, content.ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
}

func TestDatabaseBlogInterruptedImportRebuildsStaging(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "broken-blog")
	platformRoot := w.library.ContentRoot(content.PlatformScopeID)
	siteRoot := w.local.ContentRoot(site.Site.ID)
	initial := "---\ntitle: First\nslug: first\ndate: '2026-01-02'\n---\nbody\n"
	workspaceWrite(t, filepath.Join(platformRoot, "posts/tech/first.md"), []byte(initial))
	duplicate := "---\ntitle: Duplicate\nslug: duplicate\ndate: '2026-01-02'\n---\nbody\n"
	workspaceWrite(t, filepath.Join(siteRoot, "posts/a/one.md"), []byte(duplicate))
	workspaceWrite(t, filepath.Join(siteRoot, "posts/b/two.md"), []byte(duplicate))
	initialize := func() error {
		return contentbind.InitializeDatabasePosts(context.Background(), w.env.pool, w.local, w.library, w.env.store.Blog, w.env.store.PostMedia)
	}
	if err := initialize(); err == nil {
		t.Fatal("incomplete import unexpectedly succeeded")
	}
	var complete bool
	if err := w.env.pool.QueryRow(context.Background(), `SELECT complete FROM blog_migration_state WHERE singleton=true`).Scan(&complete); err != nil || complete {
		t.Fatalf("migration completion: %v %v", complete, err)
	}
	changed := strings.Replace(initial, "title: First", "title: Changed", 1)
	workspaceWrite(t, filepath.Join(platformRoot, "posts/tech/first.md"), []byte(changed))
	workspaceWrite(t, filepath.Join(siteRoot, "posts/b/two.md"), []byte(strings.Replace(duplicate, "slug: duplicate", "slug: second", 1)))
	if err := initialize(); err != nil {
		t.Fatal(err)
	}
	posts, count, err := w.library.ListPosts(content.PlatformScopeID, "", 1, 20)
	if err != nil || count != 1 || posts[0].Title != "Changed" {
		t.Fatalf("stale staged data survived: %+v %v", posts, err)
	}
	if err := w.env.pool.QueryRow(context.Background(), `SELECT complete FROM blog_migration_state WHERE singleton=true`).Scan(&complete); err != nil || !complete {
		t.Fatalf("migration did not complete: %v %v", complete, err)
	}
}

func TestDatabaseBlogImportsNestedCategoriesWithoutChangingPublicationPaths(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := w.library.ContentRoot(content.PlatformScopeID)
	workspaceWrite(t, filepath.Join(root, "posts/tech/mobile/_index.md"), []byte("---\ntitle: Mobile\n---\n"))
	workspaceWrite(t, filepath.Join(root, "posts/tech/mobile/old-filename.md"), []byte("---\ntitle: Nested\nslug: nested-slug\ndate: '2026-01-02'\ntags: [' WWDC']\n---\nbody\n"))
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); err != nil {
		t.Fatal(err)
	}
	w.library.Posts = w.env.store.Blog
	posts, count, err := w.library.ListPosts(content.PlatformScopeID, "", 1, 20)
	if err != nil || count != 1 || posts[0].Category == "tech/mobile" || !content.ValidPostCategory(posts[0].Category) {
		t.Fatalf("nested category was not normalized: %+v %v", posts, err)
	}
	if _, err := w.library.GetPost(content.PlatformScopeID, "tech/mobile/old-filename.md"); err != nil {
		t.Fatalf("legacy identity: %v", err)
	}
	output := filepath.Join(t.TempDir(), "posts")
	if err := w.env.store.Blog.ExportPosts(context.Background(), content.PlatformScopeID, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "tech/mobile/nested-slug.md"))
	if err != nil {
		t.Fatal(err)
	}
	post, err := content.ParsePost("tech/mobile/nested-slug.md", data)
	if err != nil || post.Category != "tech/mobile" || post.Slug != "nested-slug" || len(post.Tags) != 1 || post.Tags[0] != " WWDC" {
		t.Fatalf("exported post: %+v %v", post, err)
	}
}

func TestDatabaseBlogImportRejectsSourceChanges(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	path := filepath.Join(w.library.ContentRoot(content.PlatformScopeID), "posts/tech/a.md")
	before := "---\ntitle: Before\nslug: a\ndate: '2026-01-02'\n---\nbody\n"
	after := strings.Replace(before, "Before", "After", 1)
	workspaceWrite(t, path, []byte(before))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := w.env.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	const key int64 = 721904518
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
	_, err = w.env.pool.Exec(ctx, `CREATE FUNCTION block_blog_import_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(721904518); RETURN NEW; END $$;
		CREATE TRIGGER block_blog_import_test BEFORE INSERT ON blog_posts FOR EACH ROW EXECUTE FUNCTION block_blog_import_test()`)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- w.env.store.Blog.ImportLegacyScope(ctx, w.library, content.PlatformScopeID) }()
	for {
		var waiting bool
		err := w.env.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted AND objid=$1)`, key).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("import did not reach the database lock")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	workspaceWrite(t, path, []byte(after))
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil || !strings.Contains(err.Error(), "changed during database import") {
		t.Fatalf("changed input was not rejected: %v", err)
	}
	if imported, err := w.env.store.Blog.Imported(ctx, content.PlatformScopeID); err != nil || imported {
		t.Fatalf("partial import marker: %v %v", imported, err)
	}
}

func TestDatabaseBlogPreservesRootArticlePublicationPath(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := w.library.ContentRoot(content.PlatformScopeID)
	workspaceWrite(t, filepath.Join(root, "posts/root.md"), []byte("---\ntitle: Root\nslug: root\ndate: '2026-01-02'\n---\nroot body\n"))
	if err := w.env.store.Blog.ImportLegacyScope(context.Background(), w.library, content.PlatformScopeID); err != nil {
		t.Fatal(err)
	}
	w.library.Posts = w.env.store.Blog
	posts, count, err := w.library.ListPosts(content.PlatformScopeID, "", 1, 20)
	if err != nil || count != 1 || !content.ValidPostCategory(posts[0].Category) {
		t.Fatalf("root category: %+v %v", posts, err)
	}
	output := filepath.Join(t.TempDir(), "posts")
	if err := w.env.store.Blog.ExportPosts(context.Background(), content.PlatformScopeID, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "root.md"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := content.ParsePost("root.md", data)
	if err != nil || parsed.Category != "" || len(parsed.Categories) != 0 || !strings.Contains(parsed.Body, "root body") {
		t.Fatalf("root post moved: %+v %v", parsed, err)
	}
}
