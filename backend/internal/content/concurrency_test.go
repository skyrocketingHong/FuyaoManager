package content

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func concurrencyWorld(t *testing.T) (*Store, uuid.UUID) {
	t.Helper()
	root := t.TempDir()
	id := uuid.New()
	if err := os.MkdirAll(filepath.Join(root, id.String(), "content", "posts"), 0o750); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

// TestConcurrentIfMatchSingleWinner proves the ETag comparison and the write
// form one atomic step: many writers presenting the same old digest
// serialize through the file lock and exactly one wins.
func TestConcurrentIfMatchSingleWinner(t *testing.T) {
	s, id := concurrencyWorld(t)
	initial, err := s.CreatePost(id, Post{Category: "tech", Slug: "race", Title: "base", Date: "2026-11-12", Body: "base"})
	if err != nil {
		t.Fatal(err)
	}
	const n = 16
	start := make(chan struct{})
	results := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			p := initial
			p.Title = fmt.Sprint(i)
			p.Body = strings.Repeat("payload", 4096)
			_, err := s.SavePost(id, p, initial.Digest)
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("If-Match lost updates: expected exactly one winner, got %d (conflicts %d)", success, conflicts)
	}
}

// TestConcurrentCreateSingleWinner proves the duplicate check and the create
// are one atomic step: racing creators of the same path cannot both succeed
// or silently overwrite each other.
func TestConcurrentCreateSingleWinner(t *testing.T) {
	s, id := concurrencyWorld(t)
	const n = 8
	start := make(chan struct{})
	results := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := s.CreatePost(id, Post{Category: "tech", Slug: "same-slug", Title: fmt.Sprint(i), Date: "2026-11-12", Body: "x"})
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	created, exists := 0, 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrExists):
			exists++
		default:
			t.Fatal(err)
		}
	}
	if created != 1 {
		t.Fatalf("concurrent create: expected exactly one creator, got %d (exists %d)", created, exists)
	}
}

// TestSavedResponseMatchesWrittenBytes proves a successful save returns the
// digest of the bytes it wrote, never a concurrent writer's content re-read
// after the write.
func TestSavedResponseMatchesWrittenBytes(t *testing.T) {
	s, id := concurrencyWorld(t)
	initial, err := s.CreatePost(id, Post{Category: "tech", Slug: "resp", Title: "base", Date: "2026-11-12", Body: "base"})
	if err != nil {
		t.Fatal(err)
	}
	next := initial
	next.Title = "mine"
	saved, err := s.SavePost(id, next, initial.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != "mine" || saved.Digest == initial.Digest {
		t.Fatalf("saved response must describe this call's bytes: %+v", saved)
	}
	// The stored file agrees with the returned digest.
	raw, digest, err := s.ReadFile(id, RootContent, "posts/"+saved.Path)
	if err != nil || digest != saved.Digest || !strings.Contains(string(raw), "title: mine") {
		t.Fatalf("stored bytes disagree with the returned digest: %v %s", err, digest)
	}
}

// TestConcurrentDeleteVsSave proves a delete racing a save cannot remove a
// newer version without the saver learning about it: one of the two wins
// and the loser observes the conflict, never a silent overwrite.
func TestConcurrentDeleteVsSave(t *testing.T) {
	s, id := concurrencyWorld(t)
	initial, err := s.CreatePost(id, Post{Category: "tech", Slug: "del", Title: "base", Date: "2026-11-12", Body: "base"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	saveErr := make(chan error, 1)
	delErr := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		next := initial
		next.Title = "concurrent edit"
		_, err := s.SavePost(id, next, initial.Digest)
		saveErr <- err
	}()
	go func() {
		defer wg.Done()
		delErr <- s.DeletePost(id, initial.Path, initial.Digest)
	}()
	wg.Wait()
	close(saveErr)
	close(delErr)
	sErr, dErr := <-saveErr, <-delErr
	switch {
	case sErr == nil && dErr == nil:
		t.Fatal("save and delete both succeeded with the same old digest")
	case sErr != nil && !errors.Is(sErr, ErrConflict) && sErr != nil:
		t.Fatalf("save error: %v", sErr)
	case dErr != nil && !errors.Is(dErr, ErrConflict) && !errors.Is(dErr, ErrNotFound) && dErr != nil:
		t.Fatalf("delete error: %v", dErr)
	}
	// Terminal state is consistent: the file either exists with the edited
	// title or does not exist at all.
	_, _, err = s.ReadFile(id, RootContent, "posts/"+initial.Path)
	if err == nil {
		post, err := s.GetPost(id, initial.Path)
		if err != nil || post.Title != "concurrent edit" {
			t.Fatalf("stored post must be the saved version: %+v %v", post, err)
		}
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
