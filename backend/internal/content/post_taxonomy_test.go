package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// seedPostFile writes an arbitrary post file directly so drafts, invalid
// dates, and unknown keys can exist in the scanned tree.
func seedPostFile(t *testing.T, s *Store, id uuid.UUID, rel, content string) {
	t.Helper()
	if _, err := s.WriteFile(id, RootContent, "posts/"+rel, []byte(content)); err != nil {
		t.Fatal(err)
	}
}

func taxonomyTermNames(terms []TaxonomyTerm) []string {
	names := make([]string, 0, len(terms))
	for _, term := range terms {
		names = append(names, term.Name)
	}
	return names
}

func taxonomyTermCount(terms []TaxonomyTerm, name string) (int, bool) {
	for _, term := range terms {
		if term.Name == name {
			return term.PostCount, true
		}
	}
	return 0, false
}

func directoryBySlug(dirs []PostDirectory, slug string) (PostDirectory, bool) {
	for _, dir := range dirs {
		if dir.Slug == slug {
			return dir, true
		}
	}
	return PostDirectory{}, false
}

// TestGetPostTaxonomyAggregatesBeyondOnePage proves the aggregation scans
// the complete set — drafts and invalid dates count — with every article's
// terms counted once per post, and posts with no directory stay out of the
// directory view.
func TestGetPostTaxonomyAggregatesBeyondOnePage(t *testing.T) {
	s, id := concurrencyWorld(t)
	const count = 25
	for i := 0; i < count; i++ {
		slug := fmt.Sprintf("post-%02d", i)
		draft := i%5 == 0
		tags := []string{"go"}
		if i%2 == 0 {
			tags = append(tags, "perf")
		}
		if _, err := s.CreatePost(id, Post{
			Category: "tech",
			Slug:     slug,
			Title:    "post " + slug,
			Date:     "2026-11-12",
			Draft:    draft,
			Tags:     tags,
			Body:     "body",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A draft and a post with an invalid date must still count.
	seedPostFile(t, s, id, "tech/extra-draft.md", "---\ntitle: extra draft\nslug: extra-draft\ndate: not a date\ndraft: true\ncategories: [Go, Go, \"  \", go]\ntags: [Perf, perf]\n---\nbody\n")

	tax, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if tax.PostCount != count+1 {
		t.Fatalf("every post must count, drafts and invalid dates included: %d", tax.PostCount)
	}
	tech, ok := directoryBySlug(tax.Directories, "tech")
	if !ok || tech.PostCount != count+1 || tech.Title != "tech" {
		t.Fatalf("tech directory must aggregate all posts: %+v", tech)
	}
	if n, ok := taxonomyTermCount(tax.Categories, "Go"); !ok || n != 1 {
		t.Fatalf("the seeded draft's categories must count once per post: %+v", tax.Categories)
	}
	if n, ok := taxonomyTermCount(tax.Categories, "go"); !ok || n != 1 {
		t.Fatalf("case must be preserved as its own term: %+v", tax.Categories)
	}
	if n, ok := taxonomyTermCount(tax.Tags, "Perf"); !ok || n != 1 {
		t.Fatalf("draft tags must count with case preserved: %+v", tax.Tags)
	}
	if n, _ := taxonomyTermCount(tax.Tags, "perf"); n != 14 {
		t.Fatalf("lowercase perf counts the seed plus even posts: %+v", tax.Tags)
	}
}

// TestGetPostTaxonomyDirectories proves directories cover valid existing
// empty ones, prefer the _index.md title, and fall back to the slug.
func TestGetPostTaxonomyDirectories(t *testing.T) {
	s, id := concurrencyWorld(t)
	seedPostFile(t, s, id, "events/one.md", "---\ntitle: one\nslug: one\ndate: 2026-01-02\ndraft: false\n---\nx\n")
	seedPostFile(t, s, id, "events/two.md", "---\ntitle: two\nslug: two\ndate: 2026-01-02\ndraft: false\n---\nx\n")
	seedPostFile(t, s, id, "events/_index.md", "---\ntitle: Events\n---\n")
	seedPostFile(t, s, id, "plain/only.md", "---\ntitle: only\nslug: only\ndate: 2026-01-02\ndraft: false\n---\nx\n")
	if err := os.MkdirAll(filepath.Join(s.ContentRoot(id), "posts", "empty"), 0o750); err != nil {
		t.Fatal(err)
	}

	tax, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(tax.Directories) != 3 {
		t.Fatalf("three first-level directories must list: %+v", tax.Directories)
	}
	events, _ := directoryBySlug(tax.Directories, "events")
	if events.Title != "Events" || events.PostCount != 2 {
		t.Fatalf("events must use the index title and count its posts: %+v", events)
	}
	plain, _ := directoryBySlug(tax.Directories, "plain")
	if plain.Title != "plain" || plain.PostCount != 1 {
		t.Fatalf("plain must fall back to the slug: %+v", plain)
	}
	empty, _ := directoryBySlug(tax.Directories, "empty")
	if empty.Title != "empty" || empty.PostCount != 0 {
		t.Fatalf("an existing empty directory must list with zero posts: %+v", empty)
	}
}

// TestGetPostTaxonomySeparatesDirectoriesAndCategories proves a directory
// and a frontmatter category sharing a name stay two independent views.
func TestGetPostTaxonomySeparatesDirectoriesAndCategories(t *testing.T) {
	s, id := concurrencyWorld(t)
	seedPostFile(t, s, id, "travel/one.md", "---\ntitle: one\nslug: one\ndate: 2026-01-02\ndraft: false\ncategories: [travel]\n---\nx\n")
	seedPostFile(t, s, id, "tech/two.md", "---\ntitle: two\nslug: two\ndate: 2026-01-02\ndraft: false\ncategories: [travel]\n---\nx\n")

	tax, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	travelDir, _ := directoryBySlug(tax.Directories, "travel")
	if travelDir.PostCount != 1 {
		t.Fatalf("the physical directory counts only its own posts: %+v", travelDir)
	}
	if n, ok := taxonomyTermCount(tax.Categories, "travel"); !ok || n != 2 {
		t.Fatalf("the frontmatter category counts both referencing posts: %+v", tax.Categories)
	}
}

// TestGetPostTaxonomyIsolatesScopes proves two scopes with the same terms
// never mix their counts.
func TestGetPostTaxonomyIsolatesScopes(t *testing.T) {
	s, id := concurrencyWorld(t)
	other := uuid.New()
	if err := os.MkdirAll(filepath.Join(s.ContentRoot(other), "posts"), 0o750); err != nil {
		t.Fatal(err)
	}
	seedPostFile(t, s, id, "tech/one.md", "---\ntitle: one\nslug: one\ndate: 2026-01-02\ndraft: false\ncategories: [go]\ntags: [perf]\n---\nx\n")
	seedPostFile(t, s, id, "tech/two.md", "---\ntitle: two\nslug: two\ndate: 2026-01-02\ndraft: false\ncategories: [go]\n---\nx\n")
	seedPostFile(t, s, other, "blog/one.md", "---\ntitle: one\nslug: one\ndate: 2026-01-02\ndraft: false\ncategories: [go]\n---\nx\n")

	first, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := taxonomyTermCount(first.Categories, "go"); n != 2 {
		t.Fatalf("scope A keeps its own count: %+v", first.Categories)
	}
	if first.PostCount != 2 {
		t.Fatalf("scope A counts only its posts: %d", first.PostCount)
	}
	second, err := s.GetPostTaxonomy(other)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := taxonomyTermCount(second.Categories, "go"); n != 1 {
		t.Fatalf("scope B keeps its own count: %+v", second.Categories)
	}
	if second.PostCount != 1 {
		t.Fatalf("scope B counts only its posts: %d", second.PostCount)
	}
}

// TestGetPostTaxonomyFailsOnBrokenYAML proves an unparseable file fails the
// whole call with the site-relative path and without leaking the content,
// unlike the listing that skips broken files.
func TestGetPostTaxonomyFailsOnBrokenYAML(t *testing.T) {
	s, id := concurrencyWorld(t)
	seedPostFile(t, s, id, "tech/broken.md", "---\ntitle: [unclosed\nslug: broken\ndraft: false\n---\nSECRET-BODY\n")
	tax, err := s.GetPostTaxonomy(id)
	if err == nil {
		t.Fatalf("broken YAML must fail the call: %+v", tax)
	}
	if !strings.Contains(err.Error(), "tech/broken.md") {
		t.Fatalf("the error must locate the relative path: %v", err)
	}
	if strings.Contains(err.Error(), "SECRET-BODY") {
		t.Fatalf("the error must not carry the file body: %v", err)
	}
}

// TestGetPostTaxonomyBlocksSymlinkLeaks proves symlinked posts, linked
// directories, and linked index files never leak data from outside the
// scope: linked posts do not count, linked directories do not list, and a
// linked index falls back to the slug.
func TestGetPostTaxonomyBlocksSymlinkLeaks(t *testing.T) {
	s, id := concurrencyWorld(t)
	externalRoot := t.TempDir()
	externalFile := filepath.Join(externalRoot, "leak.md")
	if err := os.WriteFile(externalFile, []byte("---\ntitle: EXTERNAL-POST\n---\nSECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalDir := filepath.Join(externalRoot, "leakdir")
	if err := os.MkdirAll(externalDir, 0o750); err != nil {
		t.Fatal(err)
	}
	externalIndex := filepath.Join(externalRoot, "leak-index.md")
	if err := os.WriteFile(externalIndex, []byte("---\ntitle: EXTERNAL-INDEX\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	seedPostFile(t, s, id, "tech/real.md", "---\ntitle: real\nslug: real\ndate: 2026-01-02\ndraft: false\n---\nx\n")
	if err := os.MkdirAll(filepath.Join(s.ContentRoot(id), "posts", "empty"), 0o750); err != nil {
		t.Fatal(err)
	}
	postsDir := filepath.Join(s.ContentRoot(id), "posts")
	if err := os.Symlink(externalFile, filepath.Join(postsDir, "tech", "leak.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(externalDir, filepath.Join(postsDir, "tech", "leakdir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(externalIndex, filepath.Join(postsDir, "empty", "_index.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	tax, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if tax.PostCount != 1 {
		t.Fatalf("a linked post must not count: %d", tax.PostCount)
	}
	if _, leak := directoryBySlug(tax.Directories, "leakdir"); leak {
		t.Fatalf("a linked directory must not list: %+v", tax.Directories)
	}
	empty, _ := directoryBySlug(tax.Directories, "empty")
	if empty.Title != "empty" {
		t.Fatalf("a linked index must fall back to the slug: %+v", empty)
	}
	for _, term := range append(taxonomyTermNames(tax.Categories), taxonomyTermNames(tax.Tags)...) {
		if strings.Contains(term, "EXTERNAL") {
			t.Fatalf("external data must not leak into the vocabulary: %v", term)
		}
	}
}

// TestGetPostTaxonomyEmptyScope proves a missing or empty posts tree
// returns empty arrays with zero counts.
func TestGetPostTaxonomyEmptyScope(t *testing.T) {
	s, id := concurrencyWorld(t)
	missing, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if missing.PostCount != 0 || len(missing.Directories) != 0 || len(missing.Categories) != 0 || len(missing.Tags) != 0 {
		t.Fatalf("a missing tree is an empty taxonomy: %+v", missing)
	}
	if err := os.MkdirAll(filepath.Join(s.ContentRoot(id), "posts"), 0o750); err != nil {
		t.Fatal(err)
	}
	empty, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if empty.PostCount != 0 || len(empty.Directories) != 0 || len(empty.Categories) != 0 || len(empty.Tags) != 0 {
		t.Fatalf("an empty tree is an empty taxonomy: %+v", empty)
	}
}
