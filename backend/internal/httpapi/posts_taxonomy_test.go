package httpapi

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

type taxonomyDirectoryDTO struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	PostCount int    `json:"postCount"`
}

type taxonomyTermDTO struct {
	Name      string `json:"name"`
	PostCount int    `json:"postCount"`
}

type taxonomyDTO struct {
	Directories []taxonomyDirectoryDTO `json:"directories"`
	Categories  []taxonomyTermDTO      `json:"categories"`
	Tags        []taxonomyTermDTO      `json:"tags"`
	PostCount   int                    `json:"postCount"`
	CanEdit     bool                   `json:"canEdit"`
}

func decodeTaxonomy(t *testing.T, raw []byte) taxonomyDTO {
	t.Helper()
	var value taxonomyDTO
	decodeJSONBody(t, raw, &value)
	return value
}

func (dto taxonomyDTO) directoryBySlug(slug string) (taxonomyDirectoryDTO, bool) {
	for _, dir := range dto.Directories {
		if dir.Slug == slug {
			return dir, true
		}
	}
	return taxonomyDirectoryDTO{}, false
}

func (dto taxonomyDTO) termCount(name string) (int, bool) {
	for _, term := range append(dto.Categories, dto.Tags...) {
		if term.Name == name {
			return term.PostCount, true
		}
	}
	return 0, false
}

// TestPostsTaxonomyFollowsSourceBindingAndAccess proves the taxonomy
// endpoints follow the posts source binding (independent reads the local
// tree, shared reads the platform library read-only), that non-members and
// restricted sessions cannot read a site's taxonomy, and that the platform
// route serves the library directly.
func TestPostsTaxonomyFollowsSourceBindingAndAccess(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "taxonomy-site")
	libraryPosts := filepath.Join(w.library.ContentRoot(content.PlatformScopeID), "posts")
	workspaceWrite(t, filepath.Join(libraryPosts, "tech/one.md"), []byte("---\ntitle: one\nslug: one\ndate: 2026-01-02\ndraft: false\ncategories: [go]\ntags: [perf]\n---\nbody\n"))
	workspaceWrite(t, filepath.Join(libraryPosts, "tech/two.md"), []byte("---\ntitle: two\nslug: two\ndate: 2026-01-02\ndraft: false\ncategories: [go, travel]\n---\nbody\n"))
	workspaceWrite(t, filepath.Join(libraryPosts, "events/_index.md"), []byte("---\ntitle: Events\n---\n"))
	workspaceWrite(t, filepath.Join(w.local.ContentRoot(site.Site.ID), "posts/tech/local.md"), []byte("---\ntitle: local\nslug: local\ndate: 2026-01-02\ndraft: false\ncategories: [localcat]\ntags: [localtag]\n---\nbody\n"))

	base := "/api/v1/sites/" + site.Site.ID.String()

	// Independent posts source: the local tree with edit access.
	_, raw := workspaceRequest(t, w.client, "GET", base+"/posts/taxonomy", nil, "", 200)
	independent := decodeTaxonomy(t, raw)
	if independent.PostCount != 1 || !independent.CanEdit {
		t.Fatalf("independent taxonomy: %+v", independent)
	}
	if len(independent.Directories) != 1 || independent.Directories[0].Slug != "tech" {
		t.Fatalf("independent directories: %+v", independent.Directories)
	}
	if len(independent.Categories) != 1 || independent.Categories[0].Name != "localcat" {
		t.Fatalf("independent categories: %+v", independent.Categories)
	}
	if len(independent.Tags) != 1 || independent.Tags[0].Name != "localtag" {
		t.Fatalf("independent tags: %+v", independent.Tags)
	}

	// Shared posts source: the platform library, read-only.
	record, etag := workspaceSources(t, w.client, site.Site.ID)
	record.Sources.Posts = "platform"
	workspaceRequest(t, w.client, "PUT", base+"/content-sources", record, etag, 200)
	_, raw = workspaceRequest(t, w.client, "GET", base+"/posts/taxonomy", nil, "", 200)
	shared := decodeTaxonomy(t, raw)
	if shared.CanEdit {
		t.Fatal("shared posts must be read-only")
	}
	if shared.PostCount != 2 {
		t.Fatalf("shared taxonomy must count platform posts: %+v", shared)
	}
	events, ok := shared.directoryBySlug("events")
	if !ok || events.Title != "Events" || events.PostCount != 0 {
		t.Fatalf("the events directory must carry its index title: %+v", shared.Directories)
	}
	if n, ok := shared.termCount("go"); !ok || n != 2 {
		t.Fatalf("platform categories must aggregate: %+v", shared.Categories)
	}
	if _, ok := shared.termCount("localcat"); ok {
		t.Fatalf("local categories must not leak into the shared read: %+v", shared.Categories)
	}

	// A user outside the site cannot read the taxonomy at all.
	outsider, err := w.env.store.Users.Create(t.Context(), "outsider", "Outsider")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.env.store.Users.Activate(t.Context(), outsider.ID); err != nil {
		t.Fatal(err)
	}
	outsiderToken, outsiderCSRF, _, err := w.env.store.Sessions.Create(t.Context(), outsider.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if status, _, _ := w.env.clientFor(outsiderToken, outsiderCSRF).do("GET", base+"/posts/taxonomy", nil, nil); status == 200 {
		t.Fatal("a non-member must not read the site taxonomy")
	}

	// A restricted session is confined by the full-session requirement.
	restrictedToken, restrictedCSRF, _, err := w.env.store.Sessions.CreateWithKind(t.Context(), w.owner.ID, model.SessionKindRestricted, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if status, _, _ := w.env.clientFor(restrictedToken, restrictedCSRF).do("GET", base+"/posts/taxonomy", nil, nil); status == 200 {
		t.Fatal("a restricted session must not read the site taxonomy")
	}

	// The platform route serves the library directly with platform edit
	// rights for a platform admin.
	_, raw = workspaceRequest(t, w.client, "GET", "/api/v1/content/posts/taxonomy", nil, "", 200)
	platform := decodeTaxonomy(t, raw)
	if platform.PostCount != 2 || !platform.CanEdit {
		t.Fatalf("platform taxonomy: %+v", platform)
	}
	if _, ok := platform.directoryBySlug("events"); !ok {
		t.Fatalf("platform directories must include the index-titled directory: %+v", platform.Directories)
	}
	if _, ok := platform.termCount("localtag"); ok {
		t.Fatal("site vocabulary must not leak into the platform route")
	}
}
