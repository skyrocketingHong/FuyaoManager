package httpapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
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
	status, headers, raw := w.client.do("GET", base+"/file?path="+url.QueryEscape(legacy), nil, nil)
	if status != 200 || headers.Get("Content-Type") != "image/jpeg" || headers.Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(raw, tinyJPEGBytes) {
		t.Fatalf("preview=%d type=%q", status, headers.Get("Content-Type"))
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
