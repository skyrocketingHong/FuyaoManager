package content

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// PostTaxonomy aggregates the blog directory structure, registered terms,
// and frontmatter category/tag usages of one content scope. Physical
// directories and categories remain separate views.
type PostTaxonomy struct {
	Directories []PostDirectory `json:"directories"`
	Categories  []TaxonomyTerm  `json:"categories"`
	Tags        []TaxonomyTerm  `json:"tags"`
	PostCount   int             `json:"postCount"`
}

// PostDirectory is one valid first-level physical directory of the posts
// tree. The display name prefers the non-empty title of the directory's
// _index.md and falls back to the slug.
type PostDirectory struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	PostCount int    `json:"postCount"`
}

// TaxonomyTerm is one category or tag name with the number of posts
// referencing it, counted once per post and preserving the submitted case.
type TaxonomyTerm struct {
	Name      string `json:"name"`
	PostCount int    `json:"postCount"`
}

// GetPostTaxonomy reads the complete blog taxonomy of one content scope.
// It scans the whole valid post set — drafts and posts with invalid dates
// count, _index.md, lock artifacts, and temp files do not — so the
// aggregate never depends on the current page, search results, or a valid
// date. Unlike ListPosts, unreadable or unparseable files fail the call
// with the site-relative path instead of producing a seemingly complete
// success. The current store and scope are the read boundary: nothing
// outside the jailed posts tree is read, symlinked entries are skipped, and
// the contentbind binding decides which scope a caller may see.
func (s *Store) GetPostTaxonomy(scopeID uuid.UUID) (PostTaxonomy, error) {
	tax := PostTaxonomy{
		Directories: []PostDirectory{},
		Categories:  []TaxonomyTerm{},
		Tags:        []TaxonomyTerm{},
	}
	vocabulary, _, err := s.GetPostVocabulary(scopeID)
	if err != nil {
		return PostTaxonomy{}, err
	}
	root, err := resolve(s.ContentRoot(scopeID), "posts")
	if errors.Is(err, ErrNotFound) {
		// No posts tree yet: an empty vocabulary is a real state.
		mergePostVocabulary(&tax, vocabulary)
		return tax, nil
	}
	if err != nil {
		return PostTaxonomy{}, err
	}
	var dirSlugs []string
	dirPosts := map[string]int{}
	catCounts := map[string]int{}
	tagCounts := map[string]int{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root && errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		// Symlinks never cross the read boundary: linked files are skipped
		// and linked directories are not walked into.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			// Only first-level directories aggregate; deeper nesting stays
			// out of the taxonomy along with its files.
			rel = filepath.ToSlash(rel)
			if strings.Contains(rel, "/") {
				return fs.SkipDir
			}
			// A directory whose slug is not a valid category name (and its
			// whole subtree) never enters the candidates.
			if !categoryPattern.MatchString(rel) {
				return fs.SkipDir
			}
			dirSlugs = append(dirSlugs, rel)
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".md") || strings.HasPrefix(d.Name(), "_") {
			return nil // _index.md and lock/temp artifacts are not posts
		}
		if !ValidPostPath(rel) {
			return nil // section files and multi-level shapes stay out
		}
		data, _, readErr := s.ReadFile(scopeID, RootContent, "posts/"+rel)
		if readErr != nil {
			return fmt.Errorf("read post %s: %w", rel, readErr)
		}
		post, parseErr := ParsePost(rel, data)
		if parseErr != nil {
			return fmt.Errorf("parse post %s: %w", rel, parseErr)
		}
		tax.PostCount++
		if dir := filepath.Dir(rel); dir != "." {
			dirPosts[dir]++
		}
		countTaxonomyTerms(post.Categories, catCounts)
		countTaxonomyTerms(post.Tags, tagCounts)
		return nil
	})
	if err != nil {
		return PostTaxonomy{}, err
	}
	for _, slug := range dirSlugs {
		title, err := s.indexTitle(scopeID, slug)
		if err != nil {
			return PostTaxonomy{}, fmt.Errorf("read index for %s: %w", slug, err)
		}
		title = strings.TrimSpace(title)
		if title == "" {
			title = slug
		}
		tax.Directories = append(tax.Directories, PostDirectory{
			Slug:      slug,
			Title:     title,
			PostCount: dirPosts[slug],
		})
	}
	sort.Slice(tax.Directories, func(i, j int) bool {
		return tax.Directories[i].Slug < tax.Directories[j].Slug
	})
	for _, name := range sortedTaxonomyNames(catCounts) {
		tax.Categories = append(tax.Categories, TaxonomyTerm{Name: name, PostCount: catCounts[name]})
	}
	for _, name := range sortedTaxonomyNames(tagCounts) {
		tax.Tags = append(tax.Tags, TaxonomyTerm{Name: name, PostCount: tagCounts[name]})
	}
	mergePostVocabulary(&tax, vocabulary)
	return tax, nil
}

func mergePostVocabulary(tax *PostTaxonomy, vocabulary PostVocabulary) {
	merge := func(current []TaxonomyTerm, names []string) []TaxonomyTerm {
		seen := make(map[string]bool, len(current))
		for _, term := range current {
			seen[term.Name] = true
		}
		for _, name := range names {
			if !seen[name] {
				current = append(current, TaxonomyTerm{Name: name})
				seen[name] = true
			}
		}
		sort.Slice(current, func(i, j int) bool { return current[i].Name < current[j].Name })
		return current
	}
	tax.Categories = merge(tax.Categories, vocabulary.Categories)
	tax.Tags = merge(tax.Tags, vocabulary.Tags)
}

// indexTitle reads only the title from a directory's _index.md — no post
// field types apply to an index. A missing index yields an empty title
// (slug fallback), a symlinked index that escapes the jail is skipped the
// same way as any other link, and real read or syntax failures surface as
// errors instead of being disguised as "no title".
func (s *Store) indexTitle(scopeID uuid.UUID, slug string) (string, error) {
	data, _, err := s.ReadFile(scopeID, RootContent, "posts/"+slug+"/_index.md")
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if errors.Is(err, ErrInvalidPath) {
		// A linked index outside the jail is skipped, consistent with the
		// symlink policy for the rest of the tree.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	fm, _, err := SplitFrontmatter(data)
	if err != nil {
		return "", err
	}
	if len(strings.TrimSpace(string(fm))) == 0 {
		return "", nil
	}
	var doc struct {
		Title string `yaml:"title"`
	}
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		return "", err
	}
	return doc.Title, nil
}

// countTaxonomyTerms adds every non-blank term once per post, preserving
// case and the submitted value verbatim.
func countTaxonomyTerms(terms []string, counts map[string]int) {
	seen := map[string]struct{}{}
	for _, term := range terms {
		if strings.TrimSpace(term) == "" {
			continue
		}
		if _, dup := seen[term]; dup {
			continue
		}
		seen[term] = struct{}{}
		counts[term]++
	}
}

// sortedTaxonomyNames orders the aggregated names for stable output.
func sortedTaxonomyNames(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
