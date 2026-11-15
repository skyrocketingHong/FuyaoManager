package content

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func postTestPNG(t *testing.T, pixel color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, pixel)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPostMediaImportsLegacyPathWithoutOverwriting(t *testing.T) {
	s, id := concurrencyWorld(t)
	first := postTestPNG(t, color.Black)
	other := postTestPNG(t, color.White)
	name := "/wp-content/uploads/2020/06/image-1.png"
	item, err := s.StorePostMedia(id, name, bytes.NewReader(first), int64(len(first)))
	if err != nil || item.Path != name || item.Size != int64(len(first)) {
		t.Fatalf("import: %+v %v", item, err)
	}
	if _, err := s.StorePostMedia(id, name, bytes.NewReader(first), int64(len(first))); err != nil {
		t.Fatalf("idempotent import: %v", err)
	}
	if _, err := s.StorePostMedia(id, name, bytes.NewReader(other), int64(len(other))); !errors.Is(err, ErrExists) {
		t.Fatalf("conflict: %v", err)
	}
	data, kind, err := s.ReadPostMedia(id, name)
	if err != nil || kind != "image/png" || !bytes.Equal(data, first) {
		t.Fatalf("read: %s %v", kind, err)
	}
	items, err := s.ListPostMedia(id)
	if err != nil || len(items) != 1 || items[0].Path != name {
		t.Fatalf("list: %+v %v", items, err)
	}
	for _, invalid := range []string{"/wp-content/uploads/../secret.png", "/wp-content/uploads/2020/../../secret.png", "/images/new.png", "/wp-content/uploads/a.jpg", "/wp-content/uploads/.hidden.png"} {
		if _, err := s.StorePostMedia(id, invalid, bytes.NewReader(first), int64(len(first))); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	if _, err := s.StorePostMedia(id, "", bytes.NewReader(first), maxUploadBytes+1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size limit: %v", err)
	}
}

func TestPostMediaRejectsSymlinks(t *testing.T) {
	s, id := concurrencyWorld(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(s.ContentRoot(id), "wp-content"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.ContentRoot(id), "wp-content", "uploads")); err != nil {
		t.Fatal(err)
	}
	image := postTestPNG(t, color.Black)
	name := "/wp-content/uploads/a.png"
	if _, err := s.StorePostMedia(id, name, bytes.NewReader(image), int64(len(image))); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("write symlink: %v", err)
	}
	if _, _, err := s.ReadPostMedia(id, name); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("read symlink: %v", err)
	}
	if _, err := s.ListPostMedia(id); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("list symlink: %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatal("wrote outside content scope")
	}
}

func TestPostMediaConcurrentImportsHaveOneWinner(t *testing.T) {
	s, id := concurrencyWorld(t)
	images := [][]byte{postTestPNG(t, color.Black), postTestPNG(t, color.White)}
	results := make(chan error, len(images))
	var workers sync.WaitGroup
	for _, image := range images {
		workers.Add(1)
		go func(image []byte) {
			defer workers.Done()
			_, err := s.StorePostMedia(id, "/wp-content/uploads/race.png", bytes.NewReader(image), int64(len(image)))
			results <- err
		}(image)
	}
	workers.Wait()
	close(results)
	accepted, conflicts := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrExists) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
	}
}
