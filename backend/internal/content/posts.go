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

// postDateLayouts enumerates the accepted post date shapes; the shape is
// pinned by dateTimePattern first and the real calendar/time/offset values
// are validated by the standard parser afterwards.
var postDateLayouts = []string{
	"2006-01-02",
	"2006-01-02T15:04",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05.999999999Z07:00",
}

// parsePostDate validates and interprets one post date/lastmod value. It
// rejects impossible calendar dates, hours, minutes, seconds, and offsets
// instead of letting the parser roll them over. Values without an offset
// (date-only and naive datetimes) and date-only values are interpreted as
// UTC so every comparison happens on one clock; values with an offset keep
// their actual point in time. Reading does not rewrite historical data. An
// explicit numeric offset is range-checked on the input string because the
// generic-layout parse accepts an offset hour of 24 and an offset minute of
// 60; Z, date-only, and naive values carry no offset and skip the check.
func parsePostDate(value string) (time.Time, error) {
	if !dateTimePattern.MatchString(value) {
		return time.Time{}, errors.New("must be an ISO date or datetime")
	}
	// The pattern pins an optional offset to the tail as ±HH:mm, so the last
	// six bytes identify an explicit numeric offset; hour and minute ranges
	// (00–23 / 00–59) hold for both signs and every time-of-day shape.
	if len(value) >= 6 {
		tail := value[len(value)-6:]
		if (tail[0] == '+' || tail[0] == '-') && tail[3] == ':' {
			if hh := tail[1:3]; hh > "23" {
				return time.Time{}, fmt.Errorf("timezone offset %s out of range", tail)
			}
			if mm := tail[4:6]; mm > "59" {
				return time.Time{}, fmt.Errorf("timezone offset %s out of range", tail)
			}
		}
	}
	var parsed time.Time
	var lastErr error
	for _, layout := range postDateLayouts {
		parsed, lastErr = time.Parse(layout, value)
		if lastErr == nil {
			return parsed.UTC(), nil
		}
		// A value-range failure means the layout matched the shape but the
		// value is impossible — surface it immediately instead of trying a
		// coarser shape. Shape mismatches (cannot parse / extra text) move
		// on to the next, more specific layout.
		if strings.Contains(lastErr.Error(), "out of range") {
			return time.Time{}, lastErr
		}
	}
	return time.Time{}, lastErr
}

// utcPostDate preserves date-only values and fractional precision while
// expressing datetimes in UTC. Naive values have always been interpreted as UTC.
func utcPostDate(value string) (string, error) {
	parsed, err := parsePostDate(value)
	if err != nil {
		return "", err
	}
	if len(value) == len("2006-01-02") {
		return value, nil
	}
	if parsed.Year() < 0 || parsed.Year() > 9999 {
		return "", errors.New("UTC date is outside the four-digit year range")
	}
	layout := "2006-01-02T15:04"
	if len(value) > 16 && value[16] == ':' {
		layout += ":05"
	}
	fraction := ""
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		end := dot + 1
		for end < len(value) && value[end] >= '0' && value[end] <= '9' {
			end++
		}
		fraction = value[dot:end]
	}
	return parsed.UTC().Format(layout) + fraction + "Z", nil
}

func (p *Post) normalizeDates() error {
	date, err := utcPostDate(p.Date)
	if err != nil {
		return fieldError("date", err.Error())
	}
	lastmod := ""
	if p.Lastmod != "" {
		lastmod, err = utcPostDate(p.Lastmod)
		if err != nil {
			return fieldError("lastmod", err.Error())
		}
	}
	p.Date, p.Lastmod = date, lastmod
	return nil
}

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
	if _, err := parsePostDate(p.Date); err != nil {
		return fieldError("date", err.Error())
	}
	if p.Lastmod != "" {
		if _, err := parsePostDate(p.Lastmod); err != nil {
			return fieldError("lastmod", err.Error())
		}
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

// Render produces the full markdown file: frontmatter + body. Unknown
// frontmatter pairs (none on a fresh render) would be appended after the
// modeled keys; SavePost supplies them from the locked server copy.
func (p *Post) Render() ([]byte, error) {
	return renderPost(*p, nil)
}

// renderPost writes the modeled fields in frontmatterOrder, then any
// unknown frontmatter pairs (key, value nodes) taken verbatim from the
// server copy, then the fenced body with the existing newline convention.
func renderPost(p Post, unknown []*yaml.Node) ([]byte, error) {
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
	if len(unknown) > 0 {
		// Unknown keys keep their server-side nodes (names, nesting, values,
		// and YAML types) and their original order, rendered as one block
		// after the modeled keys. A dangling alias here (its anchor lived in
		// a replaced known field) is emitted verbatim — SavePost's pre-write
		// parse of the merged document is what rejects it, so nothing is
		// written.
		merge := &yaml.Node{Kind: yaml.MappingNode, Content: unknown}
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(merge); err != nil {
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
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

// knownFrontmatterKeys is the modeled projection set; everything else in a
// post's frontmatter is unknown data that SavePost must preserve.
var knownFrontmatterKeys = func() map[string]struct{} {
	set := make(map[string]struct{}, len(frontmatterOrder))
	for _, key := range frontmatterOrder {
		set[key] = struct{}{}
	}
	return set
}()

// unknownFrontmatter extracts the unmodeled key/value pairs from the raw
// frontmatter, preserving their order, nesting, values, and YAML types. A
// frontmatter that is not a mapping has no preservable pairs and fails
// explicitly instead of being silently flattened. The node-based decode
// skips the usual duplicate-key check, so the top level (modeled and
// unmodeled keys alike) is checked here: a duplicated key means an
// ambiguous original, never a silent cleanup. Top-level YAML merge keys
// (the `<<` merge tag, which a quoted "<<" string key does not carry) are
// rejected too: their expansion would resurrect cleared modeled fields, and
// a plain "<<" string key stays ordinary unknown data.
func unknownFrontmatter(raw []byte) ([]*yaml.Node, error) {
	fm, _, err := SplitFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(fm))) == 0 {
		return nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		return nil, fmt.Errorf("frontmatter is not valid YAML: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fieldError("frontmatter", "must be a YAML mapping")
	}
	mapping := doc.Content[0]
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	var unknown []*yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.Tag == "!!merge" {
			return nil, fieldError("frontmatter", "must not use YAML merge keys")
		}
		if _, dup := seen[key.Value]; dup {
			return nil, fieldError("frontmatter", fmt.Sprintf("duplicate key %q", key.Value))
		}
		seen[key.Value] = struct{}{}
		if _, known := knownFrontmatterKeys[key.Value]; known {
			continue
		}
		unknown = append(unknown, key, mapping.Content[i+1])
	}
	return unknown, nil
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
	// Sort by the shared date parser: valid dates descend by their actual
	// point in time (naive values on UTC), invalid historical dates land
	// after every valid one, and ties — or two invalid dates — break by
	// path. History is never filtered out for a bad date.
	dated := make([]datedPost, 0, len(all))
	for _, p := range all {
		at, err := parsePostDate(p.Date)
		if err != nil {
			dated = append(dated, datedPost{post: p})
			continue
		}
		dated = append(dated, datedPost{post: p, at: at, valid: true})
	}
	sort.SliceStable(dated, func(i, j int) bool {
		a, b := dated[i], dated[j]
		if a.valid && b.valid {
			if !a.at.Equal(b.at) {
				return a.at.After(b.at)
			}
			return a.post.Path < b.post.Path
		}
		if a.valid != b.valid {
			return a.valid
		}
		return a.post.Path < b.post.Path
	})
	total = len(dated)
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
	posts = make([]Post, 0, end-start)
	for _, entry := range dated[start:end] {
		posts = append(posts, entry.post)
	}
	return posts, total, nil
}

// datedPost pairs a listing entry with its parsed sort key; invalid dates
// sort behind every valid one.
type datedPost struct {
	post  Post
	at    time.Time
	valid bool
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

// SavePost validates and writes a post under read-modify-write semantics.
// The digest comparison and the write happen inside one exclusive file lock,
// so concurrent savers presenting the same old ETag serialize: exactly one
// wins and the rest observe the moved digest and return ErrConflict. The
// frontmatter keys outside the modeled projection survive from the locked
// server copy (names, nesting, values, and YAML types); the modeled fields
// come from this request alone, so clearing an optional value deletes its
// old entry. The merged document is parsed before anything is written, and
// the returned post and digest describe exactly the bytes this call wrote.
func (s *Store) SavePost(siteID uuid.UUID, post Post, expectedDigest string) (Post, error) {
	if err := post.Validate(); err != nil {
		return Post{}, err
	}
	if err := post.normalizeDates(); err != nil {
		return Post{}, err
	}
	rel := post.PostPath()
	var saved Post
	err := s.withFileLock(siteID, "posts", "posts/"+rel, func() error {
		var unknown []*yaml.Node
		raw, currentDigest, err := s.ReadFile(siteID, RootContent, "posts/"+rel)
		switch {
		case errors.Is(err, ErrNotFound):
			if expectedDigest != "" {
				return ErrNotFound
			}
		case err != nil:
			return err
		default:
			if expectedDigest != "" && currentDigest != expectedDigest {
				return ErrConflict
			}
			// The unknown keys only ever come from the server copy read under
			// this lock — never from the request payload.
			unknown, err = unknownFrontmatter(raw)
			if err != nil {
				return err
			}
		}
		data, err := renderPost(post, unknown)
		if err != nil {
			return err
		}
		// The merged document must parse cleanly (this also rejects dangling
		// aliases into the replaced known fields) before anything is written;
		// a failure here leaves the original file untouched.
		if _, err := ParsePost(rel, data); err != nil {
			return err
		}
		if _, err := s.WriteFile(siteID, RootContent, "posts/"+rel, data); err != nil {
			return err
		}
		parsed, err := ParsePost(rel, data)
		if err != nil {
			return err
		}
		parsed.Digest = Digest(data)
		saved = parsed
		return nil
	})
	return saved, err
}

// CreatePost saves a new post, refusing to overwrite an existing path. The
// existence check and the create run inside one lock, so racing creators of
// the same path cannot both succeed.
func (s *Store) CreatePost(siteID uuid.UUID, post Post) (Post, error) {
	if err := post.Validate(); err != nil {
		return Post{}, err
	}
	if err := post.normalizeDates(); err != nil {
		return Post{}, err
	}
	rel := post.PostPath()
	var saved Post
	err := s.withFileLock(siteID, "posts", "posts/"+rel, func() error {
		if _, _, err := s.ReadFile(siteID, RootContent, "posts/"+rel); err == nil {
			return ErrExists
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		data, err := post.Render()
		if err != nil {
			return err
		}
		if _, err := s.WriteFile(siteID, RootContent, "posts/"+rel, data); err != nil {
			return err
		}
		parsed, err := ParsePost(rel, data)
		if err != nil {
			return err
		}
		parsed.Digest = Digest(data)
		saved = parsed
		return nil
	})
	return saved, err
}

// DeletePost removes one post after digest verification; the check and the
// removal run inside one lock.
func (s *Store) DeletePost(siteID uuid.UUID, rel, expectedDigest string) error {
	if !ValidPostPath(rel) {
		return ErrInvalidPath
	}
	return s.withFileLock(siteID, "posts", "posts/"+rel, func() error {
		raw, currentDigest, err := s.ReadFile(siteID, RootContent, "posts/"+rel)
		if err != nil {
			return err
		}
		if expectedDigest != "" && currentDigest != expectedDigest {
			return ErrConflict
		}
		_ = raw
		return s.Remove(siteID, RootContent, "posts/"+rel)
	})
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
