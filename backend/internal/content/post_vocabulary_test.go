package content

import (
	"errors"
	"sync"
	"testing"
)

func TestPostVocabularyPersistsUnusedTermsAndCombinesArticleReferences(t *testing.T) {
	s, id := concurrencyWorld(t)
	initial, digest, err := s.GetPostVocabulary(id)
	if err != nil || digest != Digest(nil) || initial.Version != 1 || initial.Tags == nil || initial.Categories == nil {
		t.Fatalf("empty vocabulary: %+v %q %v", initial, digest, err)
	}
	input := PostVocabulary{Version: 1, Categories: []string{"notes", "技术"}, Tags: []string{"unused", "Go"}}
	_, savedDigest, err := s.SavePostVocabulary(id, input, digest)
	if err != nil || savedDigest == digest {
		t.Fatalf("save vocabulary: %q %v", savedDigest, err)
	}
	if _, err := s.CreatePost(id, Post{Category: "tech", Slug: "one", Title: "One", Date: "2026-01-01", Categories: []string{"notes"}, Tags: []string{"Go", "article-only"}}); err != nil {
		t.Fatal(err)
	}
	taxonomy, err := s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := taxonomyTermCount(taxonomy.Tags, "unused"); !ok || n != 0 {
		t.Fatalf("unused term missing: %+v", taxonomy.Tags)
	}
	if n, ok := taxonomyTermCount(taxonomy.Tags, "Go"); !ok || n != 1 {
		t.Fatalf("usage count changed: %+v", taxonomy.Tags)
	}
	if n, ok := taxonomyTermCount(taxonomy.Categories, "notes"); !ok || n != 1 {
		t.Fatalf("category count changed: %+v", taxonomy.Categories)
	}
	if n, ok := taxonomyTermCount(taxonomy.Tags, "article-only"); !ok || n != 1 {
		t.Fatalf("article term missing: %+v", taxonomy.Tags)
	}
	input.Tags = []string{"unused"}
	if _, _, err := s.SavePostVocabulary(id, input, digest); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale ETag: %v", err)
	}
	if _, _, err := s.SavePostVocabulary(id, input, savedDigest); err != nil {
		t.Fatal(err)
	}
	taxonomy, err = s.GetPostTaxonomy(id)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := taxonomyTermCount(taxonomy.Tags, "Go"); !ok || n != 1 {
		t.Fatalf("removing a registered term removed an article reference: %+v", taxonomy.Tags)
	}
}

func TestPostVocabularyConcurrentWritersAndValidation(t *testing.T) {
	s, id := concurrencyWorld(t)
	_, digest, err := s.GetPostVocabulary(id)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, tag := range []string{"one", "two"} {
		wg.Add(1)
		go func(tag string) {
			defer wg.Done()
			_, _, err := s.SavePostVocabulary(id, PostVocabulary{Version: 1, Categories: []string{}, Tags: []string{tag}}, digest)
			results <- err
		}(tag)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	current, currentDigest, err := s.GetPostVocabulary(id)
	if err != nil {
		t.Fatal(err)
	}
	invalid := PostVocabulary{Version: 1, Categories: []string{"  "}, Tags: []string{"valid"}}
	if _, _, err := s.SavePostVocabulary(id, invalid, currentDigest); err == nil {
		t.Fatal("empty category accepted")
	}
	after, afterDigest, err := s.GetPostVocabulary(id)
	if err != nil || currentDigest != afterDigest || len(after.Tags) != len(current.Tags) || after.Tags[0] != current.Tags[0] {
		t.Fatalf("invalid save changed vocabulary: %+v %v", after, err)
	}
}
