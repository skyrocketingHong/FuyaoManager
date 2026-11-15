package content

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPostDirectoryPreservesIndexAndUsesETag(t *testing.T) {
	s, id := concurrencyWorld(t)
	original := []byte("---\ntitle: Old\nweight: 12\ncustom:\n  active: true\n---\n\nBody\n")
	_, oldDigest, err := s.ReadFile(id, RootContent, "posts/notes/_index.md")
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.WriteFile(id, RootContent, "posts/notes/_index.md", original); err != nil {
		t.Fatal(err)
	}
	old, oldDigest, err := s.GetPostDirectory(id, "notes")
	if err != nil || old.Title != "Old" {
		t.Fatalf("read: %+v %v", old, err)
	}
	saved, nextDigest, err := s.SavePostDirectory(id, "notes", "笔记", oldDigest)
	if err != nil || saved.Title != "笔记" || nextDigest != saved.Digest {
		t.Fatalf("save: %+v %v", saved, err)
	}
	data, actualDigest, err := s.ReadFile(id, RootContent, "posts/notes/_index.md")
	if err != nil || actualDigest != nextDigest {
		t.Fatal(err)
	}
	fm, body, err := SplitFrontmatter(data)
	if err != nil || string(body) != "\nBody\n" || !strings.Contains(string(fm), "weight: 12") || !strings.Contains(string(fm), "active: true") {
		t.Fatalf("index data lost: %q %q %v", fm, body, err)
	}
	if _, _, err = s.SavePostDirectory(id, "notes", "stale", oldDigest); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale ETag: %v", err)
	}
	after, afterDigest, err := s.ReadFile(id, RootContent, "posts/notes/_index.md")
	if err != nil || string(after) != string(data) || afterDigest != nextDigest {
		t.Fatal("stale write changed index")
	}
	tax, err := s.GetPostTaxonomy(id)
	if err != nil || len(tax.Directories) != 1 || tax.Directories[0].Title != "笔记" {
		t.Fatalf("taxonomy: %+v %v", tax, err)
	}
}

func TestPostDirectoryCreateAndRejectUnsafeIndex(t *testing.T) {
	s, id := concurrencyWorld(t)
	empty, digest, err := s.GetPostDirectory(id, "new")
	if err != nil || digest != Digest(nil) || empty.Title != "new" {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	if _, _, err := s.SavePostDirectory(id, "new", "New", digest); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetPostDirectory(id, "../bad"); err == nil {
		t.Fatal("accepted invalid slug")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.ContentRoot(id), "posts", "linked")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "_index.md")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetPostDirectory(id, "linked"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("linked read: %v", err)
	}
	if _, _, err := s.SavePostDirectory(id, "linked", "Bad", Digest(nil)); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("linked write: %v", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "private" {
		t.Fatal("linked destination changed")
	}
}

func TestPostTaxonomyWithMissingPostsTree(t *testing.T) {
	s, id := concurrencyWorld(t)
	if err := os.RemoveAll(filepath.Join(s.ContentRoot(id), "posts")); err != nil {
		t.Fatal(err)
	}
	tax, err := s.GetPostTaxonomy(id)
	if err != nil || len(tax.Directories) != 0 || tax.Directories == nil {
		t.Fatalf("empty posts tree: %+v %v", tax, err)
	}
}

func TestPostDirectoryConcurrentCreationHasOneWinner(t *testing.T) {
	s, id := concurrencyWorld(t)
	_, digest, err := s.GetPostDirectory(id, "notes")
	if err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	results := make(chan error, 2)
	for _, title := range []string{"One", "Two"} {
		writers.Add(1)
		go func(title string) {
			defer writers.Done()
			_, _, err := s.SavePostDirectory(id, "notes", title, digest)
			results <- err
		}(title)
	}
	writers.Wait()
	close(results)
	accepted, conflicts := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
	}
}
