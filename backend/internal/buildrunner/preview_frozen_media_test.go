package buildrunner

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
)

func TestPreviewFrozenMediaServesFourNamespacesWithoutLiveReferences(t *testing.T) {
	p, _ := documentServer(t, map[string]string{"index.html": "<html><body>preview</body></html>"})
	site, build := uuid.New(), uuid.New()
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"images/logo.svg": "<svg>frozen-logo</svg>", "wp-content/uploads/old.png": "frozen-wordpress", "albums/photos/a.jpg": "frozen-original", "albums/thumbnails/a.webp": "frozen-thumbnail"}
	for name, data := range files {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	frozen := filepath.Join(p.cfg.BuildsRoot, build.String(), "publication-media")
	if err := os.MkdirAll(filepath.Dir(frozen), 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := publicationmedia.Freeze(source, frozen, site, build); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publicationmedia.Remove(frozen) })
	var legacyCalls atomic.Int32
	p.cfg.MediaRoots = func(uuid.UUID) (string, string, error) {
		legacyCalls.Add(1)
		return filepath.Join(source, "albums/photos"), filepath.Join(source, "albums/thumbnails"), nil
	}
	p.cfg.FrozenMediaRoot = func(actualSite, actualBuild uuid.UUID) (string, error) {
		if actualSite != site || actualBuild != build {
			return "", errors.New("wrong scope")
		}
		return frozen, nil
	}
	client, base := previewEntry(t, p, site, build)
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"/images/logo.svg": files["images/logo.svg"], "/wp-content/uploads/old.png": files["wp-content/uploads/old.png"], "/media/albums/photos/a.jpg": files["albums/photos/a.jpg"], "/media/albums/thumbnails/a.webp": files["albums/thumbnails/a.webp"]}
	for path, want := range paths {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != 200 || string(body) != want {
			t.Fatalf("frozen %s: status=%d read=%v body=%s", path, response.StatusCode, readErr, body)
		}
		if response.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal("frozen preview media became browser-cacheable")
		}
	}
	for _, path := range []string{"/images", "/images/", "/images/missing.png", "/publication-media/manifest.json", "/manifest.json", "/media/albums/photos/missing.jpg"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("non-media path %s returned %d", path, response.StatusCode)
		}
	}
	outside := filepath.Join(t.TempDir(), "private.png")
	if err := os.WriteFile(outside, []byte("outside-private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(frozen, "images"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(frozen, "images", "escape.png")); err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(base + "/images/escape.png")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("frozen media symlink escaped its fixed directory")
	}
	if legacyCalls.Load() != 0 {
		t.Fatal("frozen preview consulted the live media resolver")
	}
}

func TestPreviewMissingFrozenMediaNeverFallsBackToArtifactOrLive(t *testing.T) {
	p, dir := documentServer(t, map[string]string{"index.html": "<html><body>preview</body></html>"})
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images", "not-frozen.png"), []byte("artifact fallback must not serve"), 0600); err != nil {
		t.Fatal(err)
	}
	var legacyCalls atomic.Int32
	p.cfg.MediaRoots = func(uuid.UUID) (string, string, error) { legacyCalls.Add(1); return dir, dir, nil }
	p.cfg.FrozenMediaRoot = func(uuid.UUID, uuid.UUID) (string, error) { return "", os.ErrNotExist }
	client, base := previewEntry(t, p, uuid.New(), uuid.New())
	for _, path := range []string{"/images/not-frozen.png", "/wp-content/p.png", "/media/albums/photos/p.jpg", "/media/albums/thumbnails/p.webp"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("missing frozen media %s fell back with %d", path, response.StatusCode)
		}
	}
	if legacyCalls.Load() != 0 {
		t.Fatal("missing frozen media fell back to live content")
	}
}

func TestPreviewArtifactImagesRemainAvailableWithoutFrozenResolver(t *testing.T) {
	p, dir := documentServer(t, map[string]string{"index.html": "<html>ci</html>"})
	for _, path := range []string{"images/ci.png", "wp-content/ci.svg"} {
		file := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(file), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("self-contained-ci-asset"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client, base := previewEntry(t, p, uuid.New(), uuid.New())
	for _, path := range []string{"/images/ci.png", "/wp-content/ci.svg"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || string(body) != "self-contained-ci-asset" {
			t.Fatal("CI artifact media was incorrectly routed through production frozen media")
		}
	}
}
