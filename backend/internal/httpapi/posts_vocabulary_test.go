package httpapi

import (
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

func TestPostVocabularyPermissionsAndETag(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "vocabulary-site")
	base := "/api/v1/sites/" + site.Site.ID.String() + "/posts/vocabulary"
	headers, raw := workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	var response struct {
		content.PostVocabulary
		CanEdit bool `json:"canEdit"`
	}
	decodeJSONBody(t, raw, &response)
	if !response.CanEdit || response.Version != 1 {
		t.Fatalf("initial vocabulary: %+v", response)
	}
	body := content.PostVocabulary{Version: 1, Categories: []string{"notes"}, Tags: []string{"Go"}}
	workspaceRequest(t, w.client, "PUT", base, body, headers.Get("ETag"), 200)
	workspaceRequest(t, w.client, "PUT", base, body, headers.Get("ETag"), 412)
	if _, _, err := w.library.SavePostVocabulary(content.PlatformScopeID, content.PostVocabulary{Version: 1, Categories: []string{}, Tags: []string{"platform"}}, content.Digest(nil)); err != nil {
		t.Fatal(err)
	}
	sources, sourceETag := workspaceSources(t, w.client, site.Site.ID)
	sources.Sources.Posts = "platform"
	workspaceRequest(t, w.client, "PUT", "/api/v1/sites/"+site.Site.ID.String()+"/content-sources", sources, sourceETag, 200)
	sharedHeaders, sharedRaw := workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	decodeJSONBody(t, sharedRaw, &response)
	if response.CanEdit || len(response.Tags) != 1 || response.Tags[0] != "platform" {
		t.Fatalf("shared vocabulary: %+v", response)
	}
	workspaceRequest(t, w.client, "PUT", base, body, sharedHeaders.Get("ETag"), 409)
	_, token, csrf := w.env.createUserSession("vocabulary-reader")
	reader := w.env.clientFor(token, csrf)
	platform := "/api/v1/content/posts/vocabulary"
	readerHeaders, _ := workspaceRequest(t, reader, "GET", platform, nil, "", 200)
	workspaceRequest(t, reader, "PUT", platform, body, readerHeaders.Get("ETag"), 403)
}

func TestPostDirectoryPermissionsAndETag(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "directory-site")
	base := "/api/v1/sites/" + site.Site.ID.String() + "/posts/directories/notes"
	headers, raw := workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	var directory struct {
		content.PostDirectoryDocument
		CanEdit bool `json:"canEdit"`
	}
	decodeJSONBody(t, raw, &directory)
	if !directory.CanEdit || directory.Title != "notes" {
		t.Fatalf("initial: %+v", directory)
	}
	saved, _ := workspaceRequest(t, w.client, "PUT", base, map[string]string{"title": "Notes"}, headers.Get("ETag"), 200)
	workspaceRequest(t, w.client, "PUT", base, map[string]string{"title": "Stale"}, headers.Get("ETag"), 412)
	workspaceRequest(t, w.client, "PUT", base, map[string]string{"title": "Updated"}, saved.Get("ETag"), 200)
	if _, _, err := w.library.SavePostDirectory(content.PlatformScopeID, "notes", "Platform", content.Digest(nil)); err != nil {
		t.Fatal(err)
	}
	sources, sourceETag := workspaceSources(t, w.client, site.Site.ID)
	sources.Sources.Posts = "platform"
	workspaceRequest(t, w.client, "PUT", "/api/v1/sites/"+site.Site.ID.String()+"/content-sources", sources, sourceETag, 200)
	shared, raw := workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	decodeJSONBody(t, raw, &directory)
	if directory.CanEdit || directory.Title != "Platform" {
		t.Fatalf("shared: %+v", directory)
	}
	workspaceRequest(t, w.client, "PUT", base, map[string]string{"title": "Bad"}, shared.Get("ETag"), 409)
	_, token, csrf := w.env.createUserSession("directory-reader")
	reader := w.env.clientFor(token, csrf)
	platform := "/api/v1/content/posts/directories/notes"
	readerHeaders, _ := workspaceRequest(t, reader, "GET", platform, nil, "", 200)
	workspaceRequest(t, reader, "PUT", platform, map[string]string{"title": "Bad"}, readerHeaders.Get("ETag"), 403)
}
