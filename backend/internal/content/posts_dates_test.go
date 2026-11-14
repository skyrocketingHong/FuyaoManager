package content

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// TestParsePostDate pins the accepted shapes and semantics: impossible
// calendar dates, hours, minutes, seconds, and offsets are rejected instead
// of rolled over, and every accepted value normalizes onto UTC.
func TestParsePostDate(t *testing.T) {
	valid := map[string]string{
		"2026-01-02":                    "2026-01-02T00:00:00Z",
		"2024-02-29":                    "2024-02-29T00:00:00Z",
		"2026-11-12T10:30":              "2026-11-12T10:30:00Z",
		"2026-11-12T10:30Z":             "2026-11-12T10:30:00Z",
		"2026-11-12T10:30+08:00":        "2026-11-12T02:30:00Z",
		"2026-11-12T10:30:45":           "2026-11-12T10:30:45Z",
		"2026-11-12T10:30:45Z":          "2026-11-12T10:30:45Z",
		"2026-11-12T10:30:45+08:00":     "2026-11-12T02:30:45Z",
		"2026-11-12T10:30:45-05:00":     "2026-11-12T15:30:45Z",
		"2026-11-12T10:30:45.25Z":       "2026-11-12T10:30:45.25Z",
		"2026-11-12T10:30:45.123456789": "2026-11-12T10:30:45.123456789Z",
	}
	for value, want := range valid {
		got, err := parsePostDate(value)
		if err != nil {
			t.Fatalf("%q must parse: %v", value, err)
		}
		if formatted := got.UTC().Format(time.RFC3339Nano); formatted != want {
			t.Fatalf("%q must normalize to %s, got %s", value, want, formatted)
		}
	}
	invalid := []string{
		"2023-02-29",             // not a leap year
		"2026-02-30",             // February has 28 days
		"2026-04-31",             // April has 30 days
		"2026-13-01",             // month 13
		"2026-00-10",             // month 0
		"2026-01-00",             // day 0
		"2026-01-32",             // day 32
		"2026-11-12T24:00",       // hour 24
		"2026-11-12T10:60",       // minute 60
		"2026-11-12T10:00:60",    // second 60
		"2026-11-12T10:00+25:00", // offset hour 25
		"2026-11-12T10:00+00:99", // offset minute 99
		"2026-1-2",               // single-digit fields
		"2026/01/02",             // wrong separator
		"",                       // empty
		"not a date",             // prose
	}
	for _, value := range invalid {
		if _, err := parsePostDate(value); err == nil {
			t.Fatalf("%q must be rejected", value)
		}
	}
}

// TestValidateDateFields proves date is required, lastmod is optional but
// validated with the same rule, and neither value is rewritten.
func TestValidateDateFields(t *testing.T) {
	base := func() Post {
		return Post{Category: "tech", Slug: "d", Title: "t", Date: "2026-01-02", Draft: true, Body: "b"}
	}
	post := base()
	post.Lastmod = ""
	if err := post.Validate(); err != nil {
		t.Fatalf("empty lastmod must pass: %v", err)
	}
	post.Lastmod = "2026-01-03T09:00Z"
	if err := post.Validate(); err != nil {
		t.Fatalf("valid lastmod must pass: %v", err)
	}
	post.Lastmod = "2026-02-30"
	if err := post.Validate(); err == nil {
		t.Fatal("an impossible lastmod must fail")
	}
	post.Lastmod = "2026-01-03T09:00Z"
	post.Date = "2026-02-30"
	if err := post.Validate(); err == nil {
		t.Fatal("an impossible date must fail")
	}
	post.Date = ""
	if err := post.Validate(); err == nil {
		t.Fatal("an empty date must fail")
	}
}

// TestSavePostKeepsOriginalDateStrings proves a save writes the submitted
// date strings verbatim — no seconds padding, no offset normalization, no Z
// rewrite — and an impossible date fails with the stored bytes untouched.
func TestSavePostKeepsOriginalDateStrings(t *testing.T) {
	s, id := concurrencyWorld(t)
	original := []byte("---\ntitle: base\nslug: d\ndate: 2026-01-02\nlastmod: 2026-01-03T09:00Z\ndraft: true\n---\nbody\n")
	if _, err := s.WriteFile(id, RootContent, "posts/tech/d.md", original); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetPost(id, "tech/d.md")
	if err != nil {
		t.Fatal(err)
	}
	next := Post{
		Category: "tech",
		Slug:     "d",
		Title:    "updated",
		Date:     "2026-01-02",
		Lastmod:  "2026-01-03T09:00Z",
		Draft:    false,
		Body:     "new body",
	}
	saved, err := s.SavePost(id, next, current.Digest)
	if err != nil {
		t.Fatal(err)
	}
	raw, digest, err := s.ReadFile(id, RootContent, "posts/tech/d.md")
	if err != nil || digest != saved.Digest {
		t.Fatalf("stored bytes disagree with the returned digest: %v", err)
	}
	// The date strings survive verbatim at the value level; the YAML
	// representation may quote a digit-leading scalar, but the parsed key
	// values are exactly what the request submitted.
	fm, _, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		t.Fatal(err)
	}
	dateNode := findKey(t, doc.Content[0], "date")
	if dateNode.Value != "2026-01-02" || dateNode.Tag != "!!str" {
		t.Fatalf("date must stay the submitted string: %s %q", dateNode.Tag, dateNode.Value)
	}
	lastmodNode := findKey(t, doc.Content[0], "lastmod")
	if lastmodNode.Value != "2026-01-03T09:00Z" || lastmodNode.Tag != "!!str" {
		t.Fatalf("lastmod must stay the submitted string: %s %q", lastmodNode.Tag, lastmodNode.Value)
	}

	// An impossible date fails the save with the stored bytes untouched.
	before, beforeDigest, err := s.ReadFile(id, RootContent, "posts/tech/d.md")
	if err != nil {
		t.Fatal(err)
	}
	bad := next
	bad.Date = "2026-02-30"
	if _, err := s.SavePost(id, bad, saved.Digest); err == nil {
		t.Fatal("an impossible date must fail the save")
	}
	after, afterDigest, err := s.ReadFile(id, RootContent, "posts/tech/d.md")
	if err != nil || afterDigest != beforeDigest || string(after) != string(before) {
		t.Fatalf("rejected save must not touch the stored file: %v %s", err, afterDigest)
	}
}

// seedPost writes a post file directly so invalid dates can exist in
// history without going through validation.
func seedPost(t *testing.T, s *Store, id uuid.UUID, slug, date string) {
	t.Helper()
	fm := "---\ntitle: " + strings.ToUpper(slug) + "\nslug: " + slug + "\ndate: " + date + "\ndraft: false\n---\nbody " + slug
	if _, err := s.WriteFile(id, RootContent, "posts/tech/"+slug+".md", []byte(fm)); err != nil {
		t.Fatal(err)
	}
}

// TestListPostsOrdersValidBeforeInvalidAndPaginates proves the listing
// sorts valid dates by their actual point in time (offsets compare across
// day boundaries), lands invalid historical dates after every valid one,
// breaks ties by path, and paginates with a correct total — history is
// never filtered out for a bad date.
func TestListPostsOrdersValidBeforeInvalidAndPaginates(t *testing.T) {
	s, id := concurrencyWorld(t)
	seedPost(t, s, id, "a", "not a date")
	seedPost(t, s, id, "b", "2026-11-10")
	seedPost(t, s, id, "c", "2026-11-12T08:00Z")
	seedPost(t, s, id, "d", "2026-11-12T10:00+02:00") // same instant as c
	seedPost(t, s, id, "e", "2026-11-13T00:00+08:00") // 2026-11-12T16:00Z — latest
	seedPost(t, s, id, "f", "2026-02-30")

	posts, total, err := s.ListPosts(id, "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 6 {
		t.Fatalf("invalid history must not be filtered: total %d", total)
	}
	got := make([]string, 0, len(posts))
	for _, p := range posts {
		got = append(got, strings.TrimSuffix(strings.ToLower(filepath.Base(p.Path)), ".md"))
	}
	want := []string{"e", "c", "d", "b", "a", "f"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order must be %v, got %v", want, got)
		}
	}

	// The second page carries the remaining entries with the same total.
	page2, total2, err := s.ListPosts(id, "", 2, 4)
	if err != nil || total2 != 6 || len(page2) != 2 {
		t.Fatalf("pagination must keep the total and slice: %d %d %v", total2, len(page2), err)
	}
	if strings.TrimSuffix(strings.ToLower(filepath.Base(page2[0].Path)), ".md") != "a" || strings.TrimSuffix(strings.ToLower(filepath.Base(page2[1].Path)), ".md") != "f" {
		t.Fatalf("second page must hold the trailing invalid entries: %+v", page2)
	}
}

// TestListPostsSortingIsTimezoneIndependent proves the sort keys never
// depend on the process-local timezone: naive values sort as UTC in every
// zone. This test mutates time.Local and therefore does not run in
// parallel; the previous zone is restored afterwards.
func TestListPostsSortingIsTimezoneIndependent(t *testing.T) {
	oldLocal := time.Local
	defer func() { time.Local = oldLocal }()
	s, id := concurrencyWorld(t)
	seedPost(t, s, id, "a", "not a date")
	seedPost(t, s, id, "b", "2026-11-10")
	seedPost(t, s, id, "c", "2026-11-12T08:00Z")
	for _, offset := range []int{-8 * 3600, 0, 8 * 3600} {
		time.Local = time.FixedZone(fmt.Sprintf("WP%d", offset), offset)
		posts, total, err := s.ListPosts(id, "", 1, 20)
		if err != nil || total != 3 {
			t.Fatalf("listing must be stable across zones: %d %v", total, err)
		}
		got := make([]string, 0, len(posts))
		for _, p := range posts {
			got = append(got, strings.TrimSuffix(strings.ToLower(filepath.Base(p.Path)), ".md"))
		}
		if strings.Join(got, ",") != "c,b,a" {
			t.Fatalf("zone offset %d changed the order: %v", offset, got)
		}
	}
}
