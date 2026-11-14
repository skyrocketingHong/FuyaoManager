package content

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// rawPostWithUnknown writes a post file whose frontmatter mixes the modeled
// fields with unknown keys of every scalar and composite YAML type.
func rawPostWithUnknown() []byte {
	return []byte(`---
title: base
slug: extra
date: 2026-11-12
draft: true
lastmod: 2026-11-13
categories: [old]
tags: [oldtag]
description: old desc
cover: /old.png
summary: kept summary
rating: 4.5
custom:
  nested:
    count: 3
    flag: true
    note: null
  items:
    - a
    - b
---
original body
`)
}

func findKey(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	if mapping.Kind != yaml.MappingNode {
		t.Fatalf("expected a mapping node, got kind %v", mapping.Kind)
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	t.Fatalf("frontmatter key %q not found", key)
	return nil
}

// TestSavePostPreservesUnknownFrontmatter proves a save keeps the unmodeled
// frontmatter of the locked server copy — names, nesting, values, and YAML
// types — while the modeled fields follow the request, including explicit
// clearing of optional values.
func TestSavePostPreservesUnknownFrontmatter(t *testing.T) {
	s, id := concurrencyWorld(t)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", rawPostWithUnknown()); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetPost(id, "tech/extra.md")
	if err != nil {
		t.Fatal(err)
	}

	next := Post{
		Category: "tech",
		Slug:     "extra",
		Title:    "updated",
		Date:     "2026-11-12",
		Draft:    false,
		Body:     "new body",
	}
	saved, err := s.SavePost(id, next, current.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != "updated" || saved.Draft || saved.Lastmod != "" || saved.Description != "" {
		t.Fatalf("modeled fields must follow the request: %+v", saved)
	}

	raw, digest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil || digest != saved.Digest {
		t.Fatalf("stored bytes disagree with the returned digest: %v %s %s", err, digest, saved.Digest)
	}

	// Clearing optional modeled fields must delete their old entries, not
	// resurrect them through the merge.
	for _, gone := range []string{"lastmod:", "categories:", "tags:", "description:", "cover:"} {
		if strings.Contains(string(raw), gone) {
			t.Fatalf("cleared field %q must not survive the save:\n%s", gone, raw)
		}
	}
	if !strings.Contains(string(raw), "draft: false") {
		t.Fatalf("draft=false must win over the server copy:\n%s", raw)
	}
	if !strings.Contains(string(raw), "new body") {
		t.Fatalf("body must come from this request:\n%s", raw)
	}

	// Unknown data keeps its names, values, and YAML types.
	fm, _, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		t.Fatal(err)
	}
	mapping := doc.Content[0]
	summary := findKey(t, mapping, "summary")
	if summary.Tag != "!!str" || summary.Value != "kept summary" {
		t.Fatalf("summary must survive verbatim: %s %q", summary.Tag, summary.Value)
	}
	rating := findKey(t, mapping, "rating")
	if rating.Tag != "!!float" || rating.Value != "4.5" {
		t.Fatalf("rating must keep its float type: %s %q", rating.Tag, rating.Value)
	}
	custom := findKey(t, mapping, "custom")
	nested := findKey(t, custom, "nested")
	count := findKey(t, nested, "count")
	if count.Tag != "!!int" || count.Value != "3" {
		t.Fatalf("nested count must keep its int type: %s %q", count.Tag, count.Value)
	}
	flag := findKey(t, nested, "flag")
	if flag.Tag != "!!bool" || flag.Value != "true" {
		t.Fatalf("nested flag must keep its bool type: %s %q", flag.Tag, flag.Value)
	}
	note := findKey(t, nested, "note")
	if note.Tag != "!!null" {
		t.Fatalf("explicit null must keep its null type: %s %q", note.Tag, note.Value)
	}
	items := findKey(t, custom, "items")
	if items.Kind != yaml.SequenceNode || len(items.Content) != 2 || items.Content[0].Value != "a" {
		t.Fatalf("nested sequence must survive: %+v", items)
	}

	// Re-reading the post through the projection still works.
	reread, err := s.GetPost(id, "tech/extra.md")
	if err != nil || reread.Title != "updated" || reread.Digest != saved.Digest {
		t.Fatalf("projection must re-parse the merged file: %+v %v", reread, err)
	}
}

// TestSavePostWithoutIfMatchKeepsUnknownData proves the empty-digest call
// keeps its existing non-conditional semantics while still preserving the
// unknown frontmatter of the server copy.
func TestSavePostWithoutIfMatchKeepsUnknownData(t *testing.T) {
	s, id := concurrencyWorld(t)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", rawPostWithUnknown()); err != nil {
		t.Fatal(err)
	}
	next := Post{Category: "tech", Slug: "extra", Title: "overwritten", Date: "2026-11-12", Draft: true, Body: "b"}
	saved, err := s.SavePost(id, next, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "summary: kept summary") || saved.Title != "overwritten" {
		t.Fatalf("empty-digest save must overwrite and preserve unknown data:\n%s", raw)
	}
}

// TestSavePostConflictLeavesBytesUntouched proves an outdated digest fails
// with ErrConflict and the stored file is byte-for-byte unchanged.
func TestSavePostConflictLeavesBytesUntouched(t *testing.T) {
	s, id := concurrencyWorld(t)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", rawPostWithUnknown()); err != nil {
		t.Fatal(err)
	}
	before, digest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil {
		t.Fatal(err)
	}
	next := Post{Category: "tech", Slug: "extra", Title: "should not land", Date: "2026-11-12", Draft: false, Body: "x"}
	if _, err := s.SavePost(id, next, "sha256:"+strings.Repeat("0", 64)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale digest must conflict, got %v", err)
	}
	after, afterDigest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil || afterDigest != digest || string(after) != string(before) {
		t.Fatalf("conflict must not touch the stored file: %v %s", err, afterDigest)
	}
}

// TestSavePostDigestMatchesStoredFile proves the digest returned by a save
// that preserved unknown data describes exactly the bytes on disk.
func TestSavePostDigestMatchesStoredFile(t *testing.T) {
	s, id := concurrencyWorld(t)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", rawPostWithUnknown()); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetPost(id, "tech/extra.md")
	if err != nil {
		t.Fatal(err)
	}
	next := Post{Category: "tech", Slug: "extra", Title: "with data", Date: "2026-11-12", Draft: true, Body: "b"}
	saved, err := s.SavePost(id, next, current.Digest)
	if err != nil {
		t.Fatal(err)
	}
	raw, digest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil || digest != saved.Digest || Digest(raw) != saved.Digest {
		t.Fatalf("returned digest must match the stored bytes: %v %s %s", err, digest, saved.Digest)
	}
}

// TestSavePostRejectsDanglingAlias proves an unknown alias into a replaced
// known field fails before the write, leaving the original file untouched,
// while aliases inside the preserved unknown data survive.
func TestSavePostRejectsDanglingAlias(t *testing.T) {
	s, id := concurrencyWorld(t)
	dangling := []byte(`---
title: &t base
slug: extra
date: 2026-11-12
draft: true
summary: *t
---
body
`)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", dangling); err != nil {
		t.Fatal(err)
	}
	before, digest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil {
		t.Fatal(err)
	}
	next := Post{Category: "tech", Slug: "extra", Title: "updated", Date: "2026-11-12", Draft: false, Body: "x"}
	if _, err := s.SavePost(id, next, digest); err == nil {
		t.Fatal("an alias into a replaced known field must fail the save")
	}
	after, afterDigest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil || afterDigest != digest || string(after) != string(before) {
		t.Fatalf("rejected save must not touch the stored file: %v %s", err, afterDigest)
	}

	// Aliases inside the preserved unknown data keep working after a save.
	anchored := []byte(`---
title: base
slug: extra
date: 2026-11-12
draft: true
custom: &c
  a: 1
extra: *c
---
body
`)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", anchored); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetPost(id, "tech/extra.md")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.SavePost(id, next, current.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved.Body, "x") {
		t.Fatalf("unexpected body: %q", saved.Body)
	}
}

// TestSavePostRejectsNonMappingFrontmatter proves a non-mapping frontmatter
// fails explicitly before the write instead of being silently flattened.
func TestSavePostRejectsNonMappingFrontmatter(t *testing.T) {
	s, id := concurrencyWorld(t)
	listFm := []byte(`---
- just
- a list
---
body
`)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", listFm); err != nil {
		t.Fatal(err)
	}
	before, digest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil {
		t.Fatal(err)
	}
	next := Post{Category: "tech", Slug: "extra", Title: "updated", Date: "2026-11-12", Draft: false, Body: "x"}
	if _, err := s.SavePost(id, next, digest); err == nil {
		t.Fatal("a non-mapping frontmatter must fail the save")
	}
	after, afterDigest, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil || afterDigest != digest || string(after) != string(before) {
		t.Fatalf("rejected save must not touch the stored file: %v %s", err, afterDigest)
	}
}

// TestCreatePostStillRefusesOverwrite pins the create semantics: creating
// over an existing file (even one with unknown frontmatter) stays refused.
func TestCreatePostStillRefusesOverwrite(t *testing.T) {
	s, id := concurrencyWorld(t)
	if _, err := s.WriteFile(id, RootContent, "posts/tech/extra.md", rawPostWithUnknown()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(id, Post{Category: "tech", Slug: "extra", Title: "n", Date: "2026-11-12", Body: "b"}); !errors.Is(err, ErrExists) {
		t.Fatalf("create over an existing post must refuse, got %v", err)
	}
	// The refused create must not have touched the existing file.
	raw, _, err := s.ReadFile(id, RootContent, "posts/tech/extra.md")
	if err != nil || !strings.Contains(string(raw), "summary: kept summary") {
		t.Fatalf("refused create must leave the file intact: %v %s", err, raw)
	}
}
