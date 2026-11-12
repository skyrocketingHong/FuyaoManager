package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/siteprovision"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
)

// tinyJPEGBytes is a real 1x1 JPEG.
var tinyJPEGBytes, _ = base64.StdEncoding.DecodeString("/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCAABAAEDASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwD3+iiigD//2Q==")

// TestAlbumPipelineRealMedia drives the whole album loop with a real photo
// and a real production build: upload → indexing (twice, proving managed
// fields and order survive regeneration) → API ordering edit → build → the
// frozen snapshot carries the managed fields → private preview serves the
// actual image bytes → publish → hard delete refused against the real
// release layout → detach removes it from the next build instead.
func TestAlbumPipelineRealMedia(t *testing.T) {
	if testing.Short() {
		t.Skip("real Node/SvelteKit build")
	}
	themeRoot := testThemeRoot(t)
	buildsRoot := filepath.Join(t.TempDir(), "admin-builds")
	t.Cleanup(func() { _ = buildrunner.MakeTreeWritable(buildsRoot) })

	enq := &lateEnqueuer{}
	prev := &latePreviews{}
	var prodRunner *buildrunner.Runner
	var contentStore *content.Store
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		cs, err := content.New(e.provisionRoot)
		if err != nil {
			t.Fatalf("content store: %v", err)
		}
		contentStore = cs
		e.mutateConfig = func(cfg *Config) {
			cfg.BuildEnqueuer = enq
			cfg.Previews = prev
			cfg.Content = cs
		}
		prodSt := store.New(e.pool)
		prodRunner = buildrunner.NewRunner(buildrunner.Config{
			BuildStore: prodSt.Builds, ThemeStore: prodSt.Themes, Pool: e.pool,
			RepoRoot: repoRoot(t), BuildsRoot: buildsRoot, Mode: "production",
			ContentRoots: func(siteID uuid.UUID) (string, string, string, error) {
				return cs.ContentRoot(siteID), cs.ConfigRoot(siteID), cs.FaviconRoot(siteID), nil
			},
			Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
		})
	})
	if err := prodRunner.Start(context.Background()); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(prodRunner.Stop)
	t.Cleanup(func() {
		if t.Failed() {
			_ = os.MkdirAll("/tmp/p3-realmedia-failed", 0o755)
			_ = buildrunner.MakeTreeWritable(buildsRoot)
			entries, _ := os.ReadDir(buildsRoot)
			for _, entry := range entries {
				_ = exec.Command("cp", "-R", filepath.Join(buildsRoot, entry.Name()), filepath.Join("/tmp/p3-realmedia-failed", entry.Name())).Run()
			}
		}
		_ = buildrunner.MakeTreeWritable(buildsRoot)
	})
	previews := buildrunner.NewPreviewServer(buildrunner.PreviewConfig{
		Target: env.store.Builds, BuildsRoot: buildsRoot, TTL: time.Hour,
		MediaRoots: func(siteID uuid.UUID) (string, string, error) {
			contentRoot := contentStore.ContentRoot(siteID)
			return filepath.Join(contentRoot, "albums", "photos"),
				filepath.Join(contentRoot, "albums", "thumbnails"), nil
		},
	})
	t.Cleanup(previews.Close)
	enq.set(prodRunner)
	prev.set(previews)

	ctx := context.Background()
	owner, token, csrf := env.createUserSession("realmedia")
	c := env.clientFor(token, csrf)
	site := createSite(ctx, t, env, "real-media", "Real Media", "", "zh-CN", owner.ID)
	siteID := site.Site.ID.String()
	provisioner, err := siteprovision.New(env.provisionRoot)
	if err != nil {
		t.Fatal(err)
	}
	provisioner.SeedFaviconDir = filepath.Join(themeRoot, "static", "favicon")
	if err := provisioner.Provision(site.Site, site.Settings, ""); err != nil {
		t.Fatal(err)
	}
	registerThemeDir(t, env, themeRoot)
	selectThemeForSite(t, c, mustUUID(siteID), themeIDAt(t, themeRoot), themeVersionAt(t, themeRoot))

	// Two real uploads.
	p1, err := contentStore.StoreUpload(site.Site.ID, bytes.NewReader(tinyJPEGBytes), int64(len(tinyJPEGBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentStore.StoreUpload(site.Site.ID, bytes.NewReader(tinyJPEGBytes), int64(len(tinyJPEGBytes))); err != nil {
		t.Fatal(err)
	}
	year := strings.Split(p1, "/")[0]

	// First indexing pass through the real theme script.
	runIndexer := func(extraArgs ...string) {
		t.Helper()
		albumRoot := filepath.Join(contentStore.ContentRoot(site.Site.ID), "albums")
		args := append([]string{filepath.Join(themeRoot, "scripts", "generate-album-index.js"), "--no-rename"}, extraArgs...)
		indexer := exec.Command("node", args...)
		indexer.Env = append([]string{
			"PATH=" + os.Getenv("PATH"),
			"FUYAO_ALBUM_PHOTOS_DIR=" + filepath.Join(albumRoot, "photos"),
			"FUYAO_ALBUM_THUMBNAILS_DIR=" + filepath.Join(albumRoot, "thumbnails"),
			"FUYAO_ALBUM_METADATA_DIR=" + filepath.Join(albumRoot, "metadata"),
		}, os.Environ()...)
		if out, err := indexer.CombinedOutput(); err != nil {
			t.Fatalf("indexer: %v %s", err, out)
		}
	}
	runIndexer()

	doc, digest, err := contentStore.GetAlbumYear(site.Site.ID, year)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Photos) != 2 {
		t.Fatalf("expected two photos after indexing, got %+v", doc.Photos)
	}

	// Managed fields + custom order through the API: submit in reversed
	// order relative to the stored sequence.
	stored := make([]string, 0, len(doc.Photos))
	for _, photo := range doc.Photos {
		stored = append(stored, photo.Filename)
	}
	reversed := []string{stored[1], stored[0]}
	bodyPhotos := make([]map[string]string, 0, len(reversed))
	for _, name := range reversed {
		bodyPhotos = append(bodyPhotos, map[string]string{"filename": name, "title": name + " 标题", "alt": name + " alt"})
	}
	status, _, raw := c.do("PUT", "/api/v1/sites/"+siteID+"/albums/"+year,
		map[string]any{"photos": bodyPhotos}, map[string]string{"If-Match": `"` + digest + `"`})
	if status != http.StatusOK {
		t.Fatalf("ordering PUT status = %d body = %s", status, raw)
	}
	var savedEnv struct {
		Year struct {
			Photos []content.AlbumPhoto `json:"photos"`
		} `json:"year"`
		Digest string `json:"digest"`
	}
	decodeJSONBody(t, raw, &savedEnv)
	if len(savedEnv.Year.Photos) != 2 || savedEnv.Year.Photos[0].Filename != reversed[0] {
		t.Fatalf("ordering lost: submitted %v, got %+v", reversed, savedEnv.Year.Photos)
	}
	digest = savedEnv.Digest

	// A second indexing pass (full) plus a metadata-only pass must keep the
	// managed fields and the saved order.
	runIndexer()
	runIndexer("--metadata-only")
	doc, digest, err = contentStore.GetAlbumYear(site.Site.ID, year)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Photos) != 2 || doc.Photos[0].Filename != reversed[0] {
		t.Fatalf("indexer destroyed saved order: %+v", doc.Photos)
	}
	for _, photo := range doc.Photos {
		if photo.Title != photo.Filename+" 标题" || photo.Alt != photo.Filename+" alt" {
			t.Fatalf("indexer destroyed managed fields: %+v", photo)
		}
		if len(photo.Variants) == 0 {
			t.Fatalf("photo %s lost its thumbnail variants", photo.Filename)
		}
	}

	// Real production build.
	status, _, raw = c.do("POST", "/api/v1/sites/"+siteID+"/builds", map[string]string{}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("build create = %d %s", status, raw)
	}
	var build buildResp
	decodeJSONBody(t, raw, &build)
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		_, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/builds/"+build.ID, nil, nil)
		decodeJSONBody(t, raw, &build)
		if build.Status == "succeeded" || build.Status == "failed" {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if build.Status != "succeeded" {
		t.Fatalf("production build = %s failure = %+v", build.Status, build.Failure)
	}

	// The frozen snapshot carries the managed fields and the saved order.
	snapshotPublic := filepath.Join(buildsRoot, build.ID, "snapshot", "public")
	yearBytes, err := os.ReadFile(filepath.Join(snapshotPublic, "albums", year+".json"))
	if err != nil {
		t.Fatalf("snapshot year json missing: %v", err)
	}
	var snapYear content.AlbumYear
	if err := json.Unmarshal(yearBytes, &snapYear); err != nil {
		t.Fatal(err)
	}
	if len(snapYear.Photos) != 2 || snapYear.Photos[0].Filename != reversed[0] {
		t.Fatalf("snapshot order wrong: %+v", snapYear.Photos)
	}
	if !strings.Contains(string(yearBytes), "标题") || !strings.Contains(string(yearBytes), " alt") {
		t.Fatalf("managed fields missing from the real production snapshot:\n%s", yearBytes)
	}

	// Private preview: entry token, then the real image bytes with an image
	// content type (never the SPA shell).
	previewURL, _, err := previews.Issue(site.Site.ID, mustUUID(build.ID))
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	pc := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	resp, err := pc.Get(previewURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
		t.Fatalf("preview entry status = %d", resp.StatusCode)
	}
	parsed, _ := url.Parse(previewURL)
	mediaResp, err := pc.Get(parsed.Scheme + "://" + parsed.Host + "/media/albums/photos/" + p1)
	if err != nil {
		t.Fatal(err)
	}
	// A single Read may legally return a short count; drain the body fully.
	body, err := io.ReadAll(mediaResp.Body)
	mediaResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(mediaResp.Header.Get("Content-Type"), "image/") {
		t.Fatalf("preview photo returned %d %s", mediaResp.StatusCode, mediaResp.Header.Get("Content-Type"))
	}
	if !bytes.Equal(body, tinyJPEGBytes) {
		t.Fatalf("preview photo bytes mismatch: got %d bytes, want %d", len(body), len(tinyJPEGBytes))
	}
	// Missing media is a real 404, not the SPA shell.
	missResp, err := pc.Get(parsed.Scheme + "://" + parsed.Host + "/media/albums/photos/" + year + "/01/01/missing.jpeg")
	if err != nil {
		t.Fatal(err)
	}
	missResp.Body.Close()
	if missResp.Header.Get("Content-Type") == "text/html" || missResp.StatusCode == http.StatusOK {
		t.Fatalf("missing media answered with the SPA shell: %d %s", missResp.StatusCode, missResp.Header.Get("Content-Type"))
	}
	// Traversal must not escape the media root.
	travResp, err := pc.Get(parsed.Scheme + "://" + parsed.Host + "/media/albums/photos/..%2f..%2f..%2fetc%2fpasswd")
	if err != nil {
		t.Fatal(err)
	}
	travResp.Body.Close()
	if travResp.StatusCode == http.StatusOK && travResp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("traversal reached a non-image file: %d %s", travResp.StatusCode, travResp.Header.Get("Content-Type"))
	}

	// Publish into a real release root; the reference scan must find the
	// frozen albums/<year>.json layout and block the hard delete.
	releaseRoot := filepath.Join(t.TempDir(), "releases")
	t.Cleanup(func() { _ = buildrunner.MakeTreeWritable(releaseRoot) })
	contracts, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
	if err != nil {
		t.Fatal(err)
	}
	pm := publish.NewManager(publish.Config{
		Pool: env.pool, Releases: env.store.Releases, Builds: env.store.Builds, Themes: env.store.Themes,
		ReleasesRoot: releaseRoot, Contracts: contracts,
		Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
	})
	if _, err := pm.Publish(ctx, site.Site.ID, mustUUID(build.ID), owner.ID, []string{"manual"}); err != nil {
		t.Fatal(err)
	}
	refs, err := content.ReleaseMetadataForSite(releaseRoot, site.Site.ID)
	if err != nil {
		t.Fatalf("reference scan: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("reference scan found no documents for the real release layout")
	}
	target := doc.Photos[0]
	err = contentStore.DeleteAlbumPhoto(site.Site.ID, year, target.Filename, refs, digest)
	if err == nil {
		t.Fatal("hard delete accepted for a photo referenced by a live release")
	}

	// Detach removes it from the metadata (the next build excludes it)
	// while files stay on disk for the live release.
	detachPath := "/api/v1/sites/" + siteID + "/albums/" + year + "/" + url.PathEscape(target.Filename) + "?mode=detach"
	status, _, raw = c.do("DELETE", detachPath, nil, map[string]string{"If-Match": `"` + digest + `"`})
	if status != http.StatusNoContent {
		t.Fatalf("detach status = %d body = %s", status, raw)
	}
	doc, _, err = contentStore.GetAlbumYear(site.Site.ID, year)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Photos) != 1 {
		t.Fatalf("detach did not remove the metadata entry: %+v", doc.Photos)
	}
	if _, err := os.Stat(filepath.Join(contentStore.ContentRoot(site.Site.ID), "albums", "photos", target.Path)); err != nil {
		t.Fatalf("detach must keep the original file for live releases: %v", err)
	}
}
