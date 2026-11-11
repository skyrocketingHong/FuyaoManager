package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Post is the editable projection of one markdown post. The frontmatter
// fields mirror exactly what the theme's index generator consumes (title,
// slug, date, lastmod, draft, categories, tags, description, cover and its
// variants); `draft` is the generator's public-output gate.
type Post struct {
	Path            string   `json:"path"` // category/slug.md
	Category        string   `json:"category"`
	Title           string   `json:"title"`
	Slug            string   `json:"slug"`
	Date            string   `json:"date"`
	Lastmod         string   `json:"lastmod,omitempty"`
	Draft           bool     `json:"draft"`
	Categories      []string `json:"categories"`
	Tags            []string `json:"tags"`
	Description     string   `json:"description,omitempty"`
	Cover           string   `json:"cover,omitempty"`
	CoverAlt        string   `json:"coverAlt,omitempty"`
	CoverSource     string   `json:"coverSource,omitempty"`
	CoverSourceLink string   `json:"coverSourceLink,omitempty"`
	Body            string   `json:"body"`
	Digest          string   `json:"digest"`
}

// frontmatterOrder keeps rendering stable and review-friendly.
var frontmatterOrder = []string{
	"title", "slug", "date", "lastmod", "draft", "categories", "tags",
	"description", "cover", "cover_alt", "cover_source", "cover_source_link",
}

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,120}$`)
	categoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	dateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})?)?$`)
)

// Validate checks the editable fields; date accepts the theme's formats.
func (p *Post) Validate() error {
	if !categoryPattern.MatchString(p.Category) {
		return fieldError("category", "must match "+categoryPattern.String())
	}
	if !slugPattern.MatchString(p.Slug) {
		return fieldError("slug", "must match "+slugPattern.String())
	}
	if strings.TrimSpace(p.Title) == "" {
		return fieldError("title", "must not be empty")
	}
	if !dateTimePattern.MatchString(p.Date) {
		return fieldError("date", "must be an ISO date or datetime")
	}
	if p.Lastmod != "" && !dateTimePattern.MatchString(p.Lastmod) {
		return fieldError("lastmod", "must be an ISO date or datetime")
	}
	for _, list := range []struct {
		name   string
		values []string
	}{{"categories", p.Categories}, {"tags", p.Tags}} {
		for _, v := range list.values {
			if strings.TrimSpace(v) == "" {
				return fieldError(list.name, "must not contain empty entries")
			}
		}
	}
	return nil
}

// PostPath is the on-disk relative path of the post.
func (p *Post) PostPath() string {
	return p.Category + "/" + p.Slug + ".md"
}

// Render produces the full markdown file: frontmatter + body.
func (p *Post) Render() ([]byte, error) {
	fields := map[string]any{
		"title": p.Title,
		"slug":  p.Slug,
		"date":  p.Date,
		"draft": p.Draft,
	}
	if p.Lastmod != "" {
		fields["lastmod"] = p.Lastmod
	}
	if len(p.Categories) > 0 {
		fields["categories"] = p.Categories
	}
	if len(p.Tags) > 0 {
		fields["tags"] = p.Tags
	}
	if p.Description != "" {
		fields["description"] = p.Description
	}
	if p.Cover != "" {
		fields["cover"] = p.Cover
	}
	if p.CoverAlt != "" {
		fields["cover_alt"] = p.CoverAlt
	}
	if p.CoverSource != "" {
		fields["cover_source"] = p.CoverSource
	}
	if p.CoverSourceLink != "" {
		fields["cover_source_link"] = p.CoverSourceLink
	}
	var buf strings.Builder
	buf.WriteString("---\n")
	for _, key := range frontmatterOrder {
		value, ok := fields[key]
		if !ok {
			continue
		}
		node := &yaml.Node{}
		if err := node.Encode(value); err != nil {
			return nil, err
		}
		// Lists render inline (["a", "b"]) like the existing posts; block
		// sequences cannot follow "key:" on the same line.
		if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode {
			node.Style = yaml.FlowStyle
		}
		raw, err := yaml.Marshal(node)
		if err != nil {
			return nil, err
		}
		buf.WriteString(key + ": " + strings.TrimRight(string(raw), "\n") + "\n")
		delete(fields, key)
	}
	buf.WriteString("---\n")
	body := strings.ReplaceAll(p.Body, "\r\n", "\n")
	if body != "" && !strings.HasPrefix(body, "\n") {
		buf.WriteString("\n")
	}
	buf.WriteString(body)
	return []byte(buf.String()), nil
}

// ParsePost decodes one markdown file into the editable projection.
func ParsePost(rel string, data []byte) (Post, error) {
	fm, body, err := SplitFrontmatter(data)
	if err != nil {
		return Post{}, err
	}
	var doc struct {
		Title           string   `yaml:"title"`
		Slug            string   `yaml:"slug"`
		Date            string   `yaml:"date"`
		Lastmod         string   `yaml:"lastmod"`
		Draft           bool     `yaml:"draft"`
		Categories      []string `yaml:"categories"`
		Tags            []string `yaml:"tags"`
		Description     string   `yaml:"description"`
		Cover           string   `yaml:"cover"`
		CoverAlt        string   `yaml:"cover_alt"`
		CoverSource     string   `yaml:"cover_source"`
		CoverSourceLink string   `yaml:"cover_source_link"`
	}
	if fm != nil {
		if err := yaml.Unmarshal(fm, &doc); err != nil {
			return Post{}, fmt.Errorf("frontmatter is not valid YAML: %w", err)
		}
	}
	category := filepath.Dir(rel)
	if category == "." {
		category = ""
	}
	post := Post{
		Path:            rel,
		Category:        category,
		Title:           doc.Title,
		Slug:            doc.Slug,
		Date:            doc.Date,
		Lastmod:         doc.Lastmod,
		Draft:           doc.Draft,
		Categories:      orEmpty(doc.Categories),
		Tags:            orEmpty(doc.Tags),
		Description:     doc.Description,
		Cover:           doc.Cover,
		CoverAlt:        doc.CoverAlt,
		CoverSource:     doc.CoverSource,
		CoverSourceLink: doc.CoverSourceLink,
		Body:            string(body),
		Digest:          Digest(data),
	}
	if post.Slug == "" {
		post.Slug = strings.TrimSuffix(filepath.Base(rel), ".md")
	}
	return post, nil
}

// ListPosts scans the site's posts tree (one category nesting level),
// sorted by date descending. query filters title/slug/category/tags
// case-insensitively; pagination is 1-based.
func (s *Store) ListPosts(siteID uuid.UUID, query string, page, pageSize int) (posts []Post, total int, err error) {
	root, err := resolve(s.ContentRoot(siteID), "posts")
	if err != nil {
		return nil, 0, err
	}
	var all []Post
	q := strings.ToLower(strings.TrimSpace(query))
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil // no posts yet
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") || strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !ValidPostPath(rel) {
			return nil // section files (_index.md) and odd shapes are not editable posts
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		post, pErr := ParsePost(rel, data)
		if pErr != nil {
			return nil // unreadable frontmatter: skip in listings, still editable by path
		}
		if q != "" {
			hay := strings.ToLower(post.Title + " " + post.Slug + " " + post.Category + " " + strings.Join(post.Tags, " "))
			if !strings.Contains(hay, q) {
				return nil
			}
		}
		all = append(all, post)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		di, _ := time.Parse(time.RFC3339, all[i].Date)
		dj, _ := time.Parse(time.RFC3339, all[j].Date)
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return all[i].Path < all[j].Path
	})
	total = len(all)
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= total {
		return []Post{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

// GetPost reads one post by its site-relative path.
func (s *Store) GetPost(siteID uuid.UUID, rel string) (Post, error) {
	if !ValidPostPath(rel) {
		return Post{}, ErrInvalidPath
	}
	data, _, err := s.ReadFile(siteID, RootContent, "posts/"+rel)
	if err != nil {
		return Post{}, err
	}
	return ParsePost(rel, data)
}

// SavePost validates and atomically writes a post; expectedDigest ("")
// enforces read-modify-write consistency through the caller.
func (s *Store) SavePost(siteID uuid.UUID, post Post, expectedDigest string) (Post, error) {
	if err := post.Validate(); err != nil {
		return Post{}, err
	}
	rel := post.PostPath()
	if expectedDigest != "" {
		current, err := s.GetPost(siteID, rel)
		if errors.Is(err, ErrNotFound) {
			return Post{}, ErrNotFound
		}
		if err != nil {
			return Post{}, err
		}
		if current.Digest != expectedDigest {
			return Post{}, ErrConflict
		}
	}
	data, err := post.Render()
	if err != nil {
		return Post{}, err
	}
	if _, err := s.WriteFile(siteID, RootContent, "posts/"+rel, data); err != nil {
		return Post{}, err
	}
	return s.GetPost(siteID, rel)
}

// CreatePost saves a new post, refusing to overwrite an existing path.
func (s *Store) CreatePost(siteID uuid.UUID, post Post) (Post, error) {
	if err := post.Validate(); err != nil {
		return Post{}, err
	}
	if _, err := s.GetPost(siteID, post.PostPath()); err == nil {
		return Post{}, ErrExists
	} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrInvalidPath) {
		return Post{}, err
	}
	return s.SavePost(siteID, post, "")
}

// DeletePost removes one post after digest verification.
func (s *Store) DeletePost(siteID uuid.UUID, rel, expectedDigest string) error {
	if !ValidPostPath(rel) {
		return ErrInvalidPath
	}
	post, err := s.GetPost(siteID, rel)
	if err != nil {
		return err
	}
	if expectedDigest != "" && post.Digest != expectedDigest {
		return ErrConflict
	}
	return s.Remove(siteID, RootContent, "posts/"+rel)
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
