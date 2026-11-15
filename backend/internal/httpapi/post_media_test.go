package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentbind"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func postMediaUpload(t *testing.T, client *tclient, target, path string, data []byte) int {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("path", path); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "image.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, client.env.base+target, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", client.env.base)
	request.Header.Set("X-CSRF-Token", client.csrf)
	if client.token != "" {
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: client.token})
	}
	response, err := client.hc.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func TestPostMediaAuthorizationAndLegacyPath(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "post-media")
	base := "/api/v1/sites/" + site.Site.ID.String() + "/posts/media"
	legacy := "/wp-content/uploads/2020/06/image-1.jpg"
	if got := postMediaUpload(t, w.client, base, legacy, tinyJPEGBytes); got != http.StatusCreated {
		t.Fatalf("upload=%d", got)
	}
	if _, _, err := w.local.ReadPostMedia(site.Site.ID, legacy); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("upload unexpectedly wrote a legacy file: %v", err)
	}
	if data, _, err := w.env.store.PostMedia.Get(t.Context(), site.Site.ID, legacy); err != nil || !bytes.Equal(data, tinyJPEGBytes) {
		t.Fatalf("database upload: %v", err)
	}
	status, headers, raw := w.client.do("GET", base+"/file?path="+url.QueryEscape(legacy), nil, nil)
	if status != 200 || headers.Get("Content-Type") != "image/jpeg" || headers.Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(raw, tinyJPEGBytes) {
		t.Fatalf("preview=%d type=%q", status, headers.Get("Content-Type"))
	}
	variant := "/wp-content/uploads/2020/06/image-1-1024x227.jpg"
	status, _, raw = w.client.do("GET", base+"/file?path="+url.QueryEscape(variant), nil, nil)
	if status != 200 || !bytes.Equal(raw, tinyJPEGBytes) {
		t.Fatalf("legacy size preview=%d", status)
	}
	exact := "/wp-content/uploads/2020/06/image-1-640x480.jpg"
	distinct := append(append([]byte{}, tinyJPEGBytes...), 1)
	if got := postMediaUpload(t, w.client, base, exact, distinct); got != http.StatusCreated {
		t.Fatalf("exact size upload=%d", got)
	}
	status, _, raw = w.client.do("GET", base+"/file?path="+url.QueryEscape(exact), nil, nil)
	if status != 200 || !bytes.Equal(raw, distinct) {
		t.Fatalf("exact size preview=%d", status)
	}
	workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	if got := postMediaUpload(t, w.client, base, "/wp-content/uploads/../escape.jpg", tinyJPEGBytes); got != http.StatusBadRequest {
		t.Fatalf("traversal=%d", got)
	}
	platformPath := "/wp-content/uploads/2020/06/platform.jpg"
	if _, err := w.library.StorePostMedia(content.PlatformScopeID, platformPath, bytes.NewReader(tinyJPEGBytes), int64(len(tinyJPEGBytes))); err != nil {
		t.Fatal(err)
	}
	sources, tag := workspaceSources(t, w.client, site.Site.ID)
	sources.Sources.Posts = "platform"
	workspaceRequest(t, w.client, "PUT", "/api/v1/sites/"+site.Site.ID.String()+"/content-sources", sources, tag, 200)
	_, raw = workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	var result struct {
		Items   []content.PostMedia `json:"items"`
		CanEdit bool                `json:"canEdit"`
	}
	decodeJSONBody(t, raw, &result)
	if result.CanEdit || len(result.Items) != 1 || result.Items[0].Path != platformPath {
		t.Fatalf("shared media=%+v", result)
	}
	if err := os.Remove(filepath.Join(w.library.ContentRoot(content.PlatformScopeID), strings.TrimPrefix(platformPath, "/"))); err != nil {
		t.Fatal(err)
	}
	workspaceRequest(t, w.client, "GET", base+"/file?path="+url.QueryEscape(legacy), nil, "", 404)
	workspaceRequest(t, w.client, "GET", base+"/file?path="+url.QueryEscape(platformPath), nil, "", 200)
	if got := postMediaUpload(t, w.client, base, legacy, tinyJPEGBytes); got != http.StatusConflict {
		t.Fatalf("shared upload=%d", got)
	}
	_, token, csrf := w.env.createUserSession("post-media-reader")
	reader := w.env.clientFor(token, csrf)
	if got := postMediaUpload(t, reader, "/api/v1/content/posts/media", legacy, tinyJPEGBytes); got != http.StatusForbidden {
		t.Fatalf("member upload=%d", got)
	}
}

func TestPostMediaUpgradeAndFrozenDatabaseSnapshot(t *testing.T) {
	env := setupAtVersion(t, 19)
	legacy, err := content.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacy.ContentRoot(content.PlatformScopeID), 0750); err != nil {
		t.Fatal(err)
	}
	path := "/wp-content/uploads/2020/06/old.jpg"
	if _, err := legacy.StorePostMedia(content.PlatformScopeID, path, bytes.NewReader(tinyJPEGBytes), int64(len(tinyJPEGBytes))); err != nil {
		t.Fatal(err)
	}
	variant := "/wp-content/uploads/2020/06/old-1024x227.jpg"
	workspaceWrite(t, filepath.Join(legacy.ContentRoot(content.PlatformScopeID), "posts", "tech", "old.md"), []byte("---\ntitle: Old\nslug: old\ndate: 2020-01-01\n---\n![Old]("+variant+")\n"))
	if _, err := db.ApplyMigrations(t.Context(), env.pool); err != nil {
		t.Fatal(err)
	}
	if err := contentbind.SyncPostMedia(t.Context(), legacy, env.store.PostMedia, content.PlatformScopeID); err != nil {
		t.Fatal(err)
	}
	if data, _, err := env.store.PostMedia.Get(t.Context(), content.PlatformScopeID, variant); err != nil || !bytes.Equal(data, tinyJPEGBytes) {
		t.Fatalf("legacy size alias: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(legacy.ContentRoot(content.PlatformScopeID), "wp-content")); err != nil {
		t.Fatal(err)
	}
	if err := contentbind.SyncPostMedia(t.Context(), legacy, env.store.PostMedia, content.PlatformScopeID); err != nil {
		t.Fatal(err)
	}
	task, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, frozen := filepath.Join(task, "input"), filepath.Join(task, "frozen")
	if err := contentbind.ExportPostMedia(t.Context(), env.store.PostMedia, content.PlatformScopeID, root); err != nil {
		t.Fatal(err)
	}
	if _, err := publicationmedia.Freeze(root, frozen, uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publicationmedia.Remove(frozen) })
	if _, err := env.pool.Exec(t.Context(), `DELETE FROM post_media WHERE scope_id=$1 AND path=$2`, content.PlatformScopeID, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(frozen, strings.TrimPrefix(path, "/")))
	if err != nil || !bytes.Equal(got, tinyJPEGBytes) {
		t.Fatalf("frozen database image: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(frozen, strings.TrimPrefix(variant, "/")))
	if err != nil || !bytes.Equal(got, tinyJPEGBytes) {
		t.Fatalf("frozen legacy size alias: %v", err)
	}
}

func TestPostMediaConcurrentDatabaseWritesDoNotOverwrite(t *testing.T) {
	env := setup(t)
	path := "/wp-content/uploads/race.jpg"
	other := append(append([]byte{}, tinyJPEGBytes...), 1)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, data := range [][]byte{tinyJPEGBytes, other} {
		wg.Add(1)
		go func(data []byte) {
			defer wg.Done()
			results <- env.store.PostMedia.Put(context.Background(), content.PlatformScopeID, path, data)
		}(data)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, content.ErrExists) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
}

func TestPostMediaSharedDatabaseSnapshotRejectsPathConflicts(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "post-media-snapshot")
	path := "/wp-content/uploads/shared.jpg"
	if err := w.env.store.PostMedia.Put(t.Context(), content.PlatformScopeID, path, tinyJPEGBytes); err != nil {
		t.Fatal(err)
	}
	other := append(append([]byte{}, tinyJPEGBytes...), 1)
	if _, err := w.local.StorePostMedia(site.Site.ID, path, bytes.NewReader(other), int64(len(other))); err != nil {
		t.Fatal(err)
	}
	sources := store.IndependentSources()
	sources.Posts = "platform"
	task, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(task, "input")
	if _, _, _, err := contentbind.MaterializeWithMedia(t.Context(), w.local, w.library, w.env.store.PostMedia, site.Site.ID, sources, target); err == nil {
		t.Fatal("conflicting site image was accepted")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed snapshot was not removed: %v", err)
	}
	if err := os.Remove(filepath.Join(w.local.ContentRoot(site.Site.ID), strings.TrimPrefix(path, "/"))); err != nil {
		t.Fatal(err)
	}
	root, _, _, err := contentbind.MaterializeWithMedia(t.Context(), w.local, w.library, w.env.store.PostMedia, site.Site.ID, sources, target)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, strings.TrimPrefix(path, "/")))
	if err != nil || !bytes.Equal(got, tinyJPEGBytes) {
		t.Fatalf("shared database image missing: %v", err)
	}
}
