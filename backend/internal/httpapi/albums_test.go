package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/mediatask"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/siteprovision"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// tinyJPEG is a valid 1x1 JPEG used for upload testing.
var tinyJPEG, _ = base64.StdEncoding.DecodeString(
	"/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCAABAAEDASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwD3+iiigD//2Q==")

// footprintsWorld is the content world plus a valid base document.
func footprintsDoc() content.FootprintsDoc {
	return content.FootprintsDoc{Cities: []content.FootprintCity{{
		ID:          "110000",
		Name:        content.FootprintName{Zh: "北京", En: "Beijing"},
		Coordinates: content.FootprintCoordinates{Lng: 116.407526, Lat: 39.90403},
		Type:        "city", Adcode: "110000", VisitDate: "20240315",
		Places: []content.FootprintPlace{{
			ID: "110000-001", Name: content.FootprintName{Zh: "故宫", En: "Forbidden City"},
			Coordinates: content.FootprintCoordinates{Lng: 116.397026, Lat: 39.918058},
			Type:        "spot", VisitDate: "20240315",
		}},
	}}}
}

// TestFootprintsCRUD covers the footprint document lifecycle: read-empty,
// validation failures, persistence, and digest conflicts.
func TestFootprintsCRUD(t *testing.T) {
	_, c, siteID := contentWorld(t)

	status, _, raw := c.do("GET", "/api/v1/sites/"+siteID+"/content/footprints", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get footprints status = %d body = %s", status, raw)
	}
	var env struct {
		Digest string `json:"digest"`
	}
	decodeJSONBody(t, raw, &env)

	// Validation failures.
	bad := footprintsDoc()
	bad.Cities[0].VisitDate = "2024-03-15"
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/content/footprints",
		map[string]any{"footprints": bad}, map[string]string{"If-Match": `"` + env.Digest + `"`})
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "validation_error") {
		t.Fatalf("bad visitDate status = %d body = %s", status, raw)
	}
	bad = footprintsDoc()
	bad.Cities[0].Coordinates.Lng = 200
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/content/footprints",
		map[string]any{"footprints": bad}, map[string]string{"If-Match": `"` + env.Digest + `"`})
	if status != http.StatusBadRequest {
		t.Fatalf("out-of-bounds coords status = %d", status)
	}
	bad = footprintsDoc()
	bad.Cities[0].Places = append(bad.Cities[0].Places, content.FootprintPlace{
		ID: "110000", Name: content.FootprintName{Zh: "x", En: "x"},
		Coordinates: content.FootprintCoordinates{Lng: 116.4, Lat: 39.9},
		Type:        "spot", VisitDate: "20240315",
	})
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/content/footprints",
		map[string]any{"footprints": bad}, map[string]string{"If-Match": `"` + env.Digest + `"`})
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "duplicate id") {
		t.Fatalf("duplicate id status = %d body = %s", status, raw)
	}

	// Valid save + read back + conflict.
	good := footprintsDoc()
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/content/footprints",
		map[string]any{"footprints": good}, map[string]string{"If-Match": `"` + env.Digest + `"`})
	if status != http.StatusOK {
		t.Fatalf("put footprints status = %d body = %s", status, raw)
	}
	var saved struct {
		Footprints content.FootprintsDoc `json:"footprints"`
		Digest     string                `json:"digest"`
	}
	decodeJSONBody(t, raw, &saved)
	if len(saved.Footprints.Cities) != 1 || saved.Footprints.Cities[0].Places[0].Name.Zh != "故宫" {
		t.Fatalf("saved footprints = %+v", saved.Footprints)
	}
	status, _, _ = c.do("PUT", "/api/v1/sites/"+siteID+"/content/footprints",
		map[string]any{"footprints": good}, map[string]string{"If-Match": `"` + env.Digest + `"`})
	if status != http.StatusPreconditionFailed {
		t.Fatalf("stale save status = %d", status)
	}
}

// uploadPhoto posts one JPEG through the multipart endpoint.
func uploadPhoto(c *tclient, siteID string) (int, string, []byte) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "test.jpeg")
	_, _ = part.Write(tinyJPEG)
	_ = writer.Close()
	req, _ := http.NewRequest(http.MethodPost, c.env.base+"/api/v1/sites/"+siteID+"/albums/photos", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", c.env.base)
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	if c.token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: c.token})
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, "", nil
	}
	defer resp.Body.Close()
	raw := make([]byte, 4096)
	n, _ := resp.Body.Read(raw)
	return resp.StatusCode, resp.Header.Get("ETag"), raw[:n]
}

// TestAlbumUploadProcessEditDelete drives the real album pipeline: upload,
// queued processing through the theme's Node indexer (EXIF + thumbnails),
// metadata editing with reordering, reference-checked deletion, and final
// deletion once unreferenced.
func TestAlbumUploadProcessEditDelete(t *testing.T) {
	var runner *mediatask.Runner
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		contentStore, err := content.New(e.provisionRoot)
		if err != nil {
			t.Fatalf("content store: %v", err)
		}
		st := store.New(e.pool)
		runner = mediatask.NewRunner(mediatask.Config{
			Tasks:  st.MediaTasks,
			Themes: st.Themes,
			ContentRoots: func(siteID uuid.UUID) (string, string, string, error) {
				contentRoot := contentStore.ContentRoot(siteID)
				return filepath.Join(contentRoot, "albums", "photos"),
					filepath.Join(contentRoot, "albums", "thumbnails"),
					filepath.Join(contentRoot, "albums", "metadata"), nil
			},
		})
		e.mutateConfig = func(cfg *Config) {
			cfg.Content = contentStore
			cfg.MediaTasks = st.MediaTasks
		}
	})
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("start media runner: %v", err)
	}
	t.Cleanup(runner.Stop)

	ctx := context.Background()
	owner, token, csrf := env.createUserSession("albumowner")
	c := env.clientFor(token, csrf)
	site := createSite(ctx, t, env, "album-site", "Album Site", "", "zh-CN", owner.ID)
	siteID := site.Site.ID.String()
	provisioner, err := siteprovision.New(env.provisionRoot)
	if err != nil {
		t.Fatalf("provisioner: %v", err)
	}
	if err := provisioner.Provision(site.Site, site.Settings); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// The processing pipeline needs a selected theme; the repo's own theme
	// source provides the scripts (read-only).
	registerFirstTheme(t, env)
	selectThemeForSite(t, c, mustUUID(siteID), "fuyao-homepage", themeVersionFromCatalog(t, env))

	// Upload one JPEG: accepted, task queued.
	status, _, raw := uploadPhoto(c, siteID)
	if status != http.StatusAccepted {
		t.Fatalf("upload status = %d body = %s", status, raw)
	}
	var accepted struct {
		Path   string `json:"path"`
		TaskID string `json:"taskId"`
	}
	decodeJSONBody(t, raw, &accepted)
	if accepted.Path == "" || accepted.TaskID == "" {
		t.Fatalf("accepted = %+v", accepted)
	}

	// Wait for the task to finish (real Node indexer).
	deadline := time.Now().Add(3 * time.Minute)
	done := false
	for time.Now().Before(deadline) {
		_, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/albums/tasks", nil, nil)
		var list struct {
			Tasks []struct {
				ID      string          `json:"id"`
				Status  string          `json:"status"`
				Failure json.RawMessage `json:"failure"`
			} `json:"tasks"`
		}
		decodeJSONBody(t, raw, &list)
		if len(list.Tasks) > 0 {
			if list.Tasks[0].Status == "succeeded" || list.Tasks[0].Status == "failed" {
				if list.Tasks[0].Status == "failed" {
					t.Fatalf("media task failed: %s", list.Tasks[0].Failure)
				}
				done = true
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if !done {
		t.Fatal("media task did not finish in time")
	}

	// The index and the current year document carry the photo with variants.
	status, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/albums", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("albums status = %d body = %s", status, raw)
	}
	var indexEnv struct {
		Index struct {
			Years       []int `json:"years"`
			TotalPhotos int   `json:"totalPhotos"`
		} `json:"index"`
	}
	decodeJSONBody(t, raw, &indexEnv)
	if indexEnv.Index.TotalPhotos == 0 || len(indexEnv.Index.Years) == 0 {
		t.Fatalf("index = %+v", indexEnv.Index)
	}
	year := fmt.Sprintf("%d", indexEnv.Index.Years[len(indexEnv.Index.Years)-1])
	status, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/albums/"+year, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("year status = %d body = %s", status, raw)
	}
	var yearEnv struct {
		Year struct {
			Photos []content.AlbumPhoto `json:"photos"`
		} `json:"year"`
		Digest string `json:"digest"`
	}
	decodeJSONBody(t, raw, &yearEnv)
	if len(yearEnv.Year.Photos) != 1 {
		t.Fatalf("year photos = %+v", yearEnv.Year)
	}
	photo := yearEnv.Year.Photos[0]
	if len(photo.Variants) == 0 {
		t.Fatal("indexer produced no thumbnail variants")
	}
	if photo.Path != accepted.Path {
		t.Fatalf("photo path %q != uploaded %q", photo.Path, accepted.Path)
	}

	// Edit: title/alt plus unchanged order; digest round-trips.
	editBody := map[string]any{"photos": []map[string]any{{
		"filename": photo.Filename, "title": "测试照片", "description": "一张测试图", "alt": "alt text",
	}}}
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/albums/"+year, editBody,
		map[string]string{"If-Match": `"` + yearEnv.Digest + `"`})
	if status != http.StatusOK {
		t.Fatalf("year edit status = %d body = %s", status, raw)
	}
	decodeJSONBody(t, raw, &yearEnv)
	if yearEnv.Year.Photos[0].Title != "测试照片" || yearEnv.Year.Photos[0].Alt != "alt text" {
		t.Fatalf("edited photo = %+v", yearEnv.Year.Photos[0])
	}

	// Reference-checked deletion: a frozen release listing the photo blocks
	// the delete; without references it succeeds. The check reads release
	// trees on disk, so build one directly.
	releasesRoot := t.TempDir()
	releaseID := uuid.New()
	siteReleaseDir := filepath.Join(releasesRoot, site.Site.ID.String(), releaseID.String(), "albums", "metadata")
	if err := os.MkdirAll(siteReleaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	refDoc := fmt.Sprintf(`{"year":%s,"photos":[{"filename":%q,"path":%q}]}`, year, photo.Filename, photo.Path)
	if err := os.WriteFile(filepath.Join(siteReleaseDir, year+".json"), []byte(refDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	contentStore, err := content.New(env.provisionRoot)
	if err != nil {
		t.Fatal(err)
	}
	blocked := contentStore.DeleteAlbumPhoto(site.Site.ID, year, photo.Filename,
		content.ReleaseMetadataForSite(releasesRoot, site.Site.ID), yearEnv.Digest)
	if blocked == nil {
		t.Fatal("delete with a live release reference must be refused")
	}
	var inUse *content.PhotoInUseError
	if !errors.As(blocked, &inUse) {
		t.Fatalf("expected PhotoInUseError, got %v", blocked)
	}

	// Without references the photo, thumbnails, and entry disappear.
	if err := contentStore.DeleteAlbumPhoto(site.Site.ID, year, photo.Filename, nil, yearEnv.Digest); err != nil {
		t.Fatalf("unreferenced delete: %v", err)
	}
	contentRoot := filepath.Join(env.provisionRoot, site.Site.ID.String(), "content")
	if _, err := os.Stat(filepath.Join(contentRoot, "albums", "photos", photo.Path)); !os.IsNotExist(err) {
		t.Fatal("original file still present after delete")
	}
	for _, variant := range photo.Variants {
		if _, err := os.Stat(filepath.Join(contentRoot, "albums", "thumbnails", variant.Path)); !os.IsNotExist(err) {
			t.Fatal("thumbnail still present after delete")
		}
	}
	_, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/albums/"+year, nil, nil)
	decodeJSONBody(t, raw, &yearEnv)
	if len(yearEnv.Year.Photos) != 0 {
		t.Fatalf("photo still listed after delete: %+v", yearEnv.Year.Photos)
	}
}
