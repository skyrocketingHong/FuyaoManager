package buildrunner

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fixedTarget serves a static map of build id → (artifact dir, SPA fallback).
type fixedTarget struct {
	entries map[uuid.UUID][2]string
}

func (f fixedTarget) GetPreviewTarget(ctx context.Context, buildID uuid.UUID) (string, string, error) {
	entry, ok := f.entries[buildID]
	if !ok {
		return "", "", os.ErrNotExist
	}
	return entry[0], entry[1], nil
}

// TestPreviewCookieIsolation reproduces the real-browser sharing that broke
// the single fixed cookie name: one cookie jar, one http.Client, two preview
// origins on different ports of the same host. Both entry URLs are redeemed
// in sequence (default redirect following stores each origin's cookie), then
// both roots and data files are re-visited through the same client — both
// must stay authorized with their own content. No manual AddCookie, no
// per-preview jars.
func TestPreviewCookieIsolation(t *testing.T) {
	buildsRoot := t.TempDir()
	write := func(marker string) (uuid.UUID, string) {
		id := uuid.New()
		dir := filepath.Join(buildsRoot, id.String(), "artifacts")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>"+marker+"</html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"marker":"`+marker+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return id, dir
	}
	id1, art1 := write("build-one")
	id2, art2 := write("build-two")

	server := NewPreviewServer(PreviewConfig{
		Target:     fixedTarget{entries: map[uuid.UUID][2]string{id1: {art1, ""}, id2: {art2, ""}}},
		BuildsRoot: buildsRoot,
		TTL:        time.Hour,
	})
	defer server.Close()

	url1, _, err := server.Issue(uuid.New(), id1)
	if err != nil {
		t.Fatalf("issue first preview: %v", err)
	}
	url2, _, err := server.Issue(uuid.New(), id2)
	if err != nil {
		t.Fatalf("issue second preview: %v", err)
	}
	origin1 := url1[:strings.Index(url1, "/__preview__/")]
	origin2 := url2[:strings.Index(url2, "/__preview__/")]
	if origin1 == origin2 {
		t.Fatalf("previews must get distinct origins: %s", origin1)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	// Redeem both entry URLs through the same client, in sequence. Each
	// 303 sets that origin's uniquely named cookie; nothing is overwritten.
	for _, tc := range []struct{ url, marker string }{
		{url1, "build-one"},
		{url2, "build-two"},
	} {
		resp, err := client.Get(tc.url)
		if err != nil {
			t.Fatalf("redeem %s: %v", tc.url, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), tc.marker) {
			t.Fatalf("redeem %s: status %d body %s", tc.url, resp.StatusCode, body)
		}
	}

	// Re-visit both origins through the same client: both stay authorized
	// and serve their own root page and data file.
	for _, tc := range []struct{ origin, marker string }{
		{origin1, "build-one"},
		{origin2, "build-two"},
	} {
		for _, path := range []string{"/", "/data.json"} {
			resp, err := client.Get(tc.origin + path)
			if err != nil {
				t.Fatalf("GET %s%s: %v", tc.origin, path, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), tc.marker) {
				t.Fatalf("GET %s%s: status %d body %s", tc.origin, path, resp.StatusCode, body)
			}
		}
	}

	// Distinct cookie names must actually be present in the shared jar.
	names := map[string]bool{}
	for _, origin := range []string{origin1, origin2} {
		for _, c := range jar.Cookies(mustURL(t, origin)) {
			if strings.HasPrefix(c.Name, previewCookiePrefix) {
				names[c.Name] = true
			}
		}
	}
	if len(names) != 2 {
		t.Fatalf("expected two distinct preview cookies in the shared jar, got %v", names)
	}
}

// Anonymous access (no cookie at all) stays rejected on both origins.
func TestPreviewCookieIsolationAnonymous(t *testing.T) {
	buildsRoot := t.TempDir()
	id := uuid.New()
	dir := filepath.Join(buildsRoot, id.String(), "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewPreviewServer(PreviewConfig{
		Target:     fixedTarget{entries: map[uuid.UUID][2]string{id: {dir, ""}}},
		BuildsRoot: buildsRoot,
		TTL:        time.Hour,
	})
	defer server.Close()
	url, _, err := server.Issue(uuid.New(), id)
	if err != nil {
		t.Fatal(err)
	}
	origin := url[:strings.Index(url, "/__preview__/")]
	// A client without a jar sends no cookies.
	bare := &http.Client{Timeout: 10 * time.Second}
	resp, err := bare.Get(origin + "/")
	if err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous root access: err=%v status=%v", err, resp)
	}
	resp.Body.Close()
}

// mustURL parses a URL for jar inspection.
func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
