package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func platformContentWorld(t *testing.T) (*testEnv, *content.Store, uuid.UUID) {
	t.Helper()
	library, err := content.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(library.SitesRoot, content.PlatformScopeID.String())
	for _, name := range []string{"content/posts", "content/data", "content/albums/photos", "content/albums/thumbnails", "content/albums/metadata", "config"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0750); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{"content/data/friends.yaml": "online: []\noffline: []\n", "content/data/footprints.yaml": "cities: []\n", "content/albums/metadata/index.json": `{"schemaVersion":1,"totalPhotos":0,"years":[],"devices":[]}`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	themeID := uuid.New()
	env := setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatal(err)
		}
		manifest := json.RawMessage(`{"contract":"fuyao.theme.manifest/v1","id":"platform-importer","version":"1.0.0","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["albums"],"outputs":{"spaFallback":"200.html","urlConventions":["media-albums"],"requiredArtifacts":[]}}`)
		sum := sha256.Sum256(manifest)
		if _, err := store.New(env.pool).Themes.Register(ctx, model.Theme{ID: themeID, ThemeID: "platform-importer", Version: "1.0.0", Adapter: "node22-sveltekit-static", Manifest: manifest, ManifestDigest: sum[:], SourcePath: fixtureThemeRoot(t)}); err != nil {
			t.Fatal(err)
		}
		env.mutateConfig = func(cfg *Config) {
			cfg.PlatformContent = library
			cfg.PlatformContentThemeID = themeID
			cfg.MediaTasks = store.New(env.pool).MediaTasks
		}
	})
	return env, library, themeID
}

func TestPlatformContentUsesIndependentScopeWithoutFabricatingSite(t *testing.T) {
	env, library, _ := platformContentWorld(t)
	_, admin := adminClient(env)
	_, token, csrf := env.createUserSession("library-reader")
	reader := env.clientFor(token, csrf)
	path := "/api/v1/content/posts"
	_, raw := adminRequest(t, reader, "GET", path, nil, 200)
	var list struct {
		Posts   []content.Post `json:"posts"`
		CanEdit bool           `json:"canEdit"`
	}
	decodeJSONBody(t, raw, &list)
	if list.CanEdit || len(list.Posts) != 0 {
		t.Fatal("member content permission or initial library changed")
	}
	adminRequest(t, reader, "POST", path, postBody("tech", "blocked", "Blocked", false), 403)
	headers, raw := adminRequest(t, admin, "POST", path, postBody("tech", "library-post", "Library Post", false), 201)
	var post postContentDTO
	decodeJSONBody(t, raw, &post)
	if !post.CanEdit {
		t.Fatal("administrator cannot edit platform content")
	}
	encodedPath := path + "/tech%2Flibrary-post.md"
	_, raw = adminRequest(t, reader, "GET", encodedPath, nil, 200)
	decodeJSONBody(t, raw, &post)
	if post.CanEdit || post.Title != "Library Post" {
		t.Fatal("platform post read lost body or authorization")
	}
	status, _, raw := admin.do("PUT", encodedPath, postBody("tech", "library-post", "Updated", false), map[string]string{"If-Match": headers.Get("ETag")})
	if status != 200 {
		t.Fatalf("save: %d %s", status, raw)
	}
	var saved postContentDTO
	decodeJSONBody(t, raw, &saved)
	status, _, _ = admin.do("PUT", encodedPath, postBody("tech", "library-post", "Stale", false), map[string]string{"If-Match": headers.Get("ETag")})
	if status != 412 {
		t.Fatal("platform content ignored ETag")
	}
	var sites, scopes int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM sites WHERE id=$1`, content.PlatformScopeID).Scan(&sites); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM content_scopes WHERE id=$1 AND kind='platform' AND site_id IS NULL`, content.PlatformScopeID).Scan(&scopes); err != nil {
		t.Fatal(err)
	}
	if sites != 0 || scopes != 1 {
		t.Fatal("platform library was modeled as a website")
	}
	if _, err := library.GetPost(content.PlatformScopeID, "tech/library-post.md"); err != nil {
		t.Fatal("post was not stored in its independent library")
	}
	status, _, _ = admin.do("DELETE", encodedPath, nil, map[string]string{"If-Match": `"` + saved.Digest + `"`})
	if status != 204 {
		t.Fatalf("delete: %d", status)
	}
	adminRequest(t, env.newClient(), "GET", path, nil, 401)
	restricted, rCSRF, _, err := env.store.Sessions.CreateWithKind(t.Context(), mustUUIDFromUser(t, env, "platform-admin"), model.SessionKindRestricted, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	adminRequest(t, env.clientFor(restricted, rCSRF), "GET", path, nil, 403)
	adminRequest(t, env.clientFor(admin.token, ""), "POST", path, postBody("tech", "csrf", "Blocked", false), 403)
}

func mustUUIDFromUser(t *testing.T, env *testEnv, name string) uuid.UUID {
	t.Helper()
	u, err := env.store.Users.GetByUsername(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestPlatformContentDocumentsAndFrozenMediaImporter(t *testing.T) {
	env, _, themeID := platformContentWorld(t)
	_, admin := adminClient(env)
	_, token, csrf := env.createUserSession("media-reader")
	reader := env.clientFor(token, csrf)
	for _, path := range []string{"/api/v1/content/friends", "/api/v1/content/footprints", "/api/v1/content/albums", "/api/v1/content/albums/tasks"} {
		_, raw := adminRequest(t, reader, "GET", path, nil, 200)
		if !strings.Contains(string(raw), `"canEdit":false`) {
			t.Fatal("library read did not return read-only capability")
		}
	}
	adminRequest(t, reader, "POST", "/api/v1/content/albums/process", map[string]any{}, 403)
	adminRequest(t, admin, "POST", "/api/v1/content/albums/process", map[string]any{"sourcePath": "/arbitrary"}, 400)
	_, raw := adminRequest(t, admin, "POST", "/api/v1/content/albums/process", map[string]any{}, 202)
	var result struct {
		TaskID uuid.UUID `json:"taskId"`
	}
	decodeJSONBody(t, raw, &result)
	var scope, frozen uuid.UUID
	if err := env.pool.QueryRow(t.Context(), `SELECT site_id,theme_id FROM site_media_tasks WHERE id=$1`, result.TaskID).Scan(&scope, &frozen); err != nil {
		t.Fatal(err)
	}
	if scope != content.PlatformScopeID || frozen != themeID {
		t.Fatal("platform importer was not frozen to the trusted theme row")
	}
	_, raw = adminRequest(t, reader, "GET", "/api/v1/content/albums/tasks", nil, 200)
	if !strings.Contains(string(raw), result.TaskID.String()) {
		t.Fatal("full member cannot read the platform task")
	}
	if strings.Contains(string(raw), themeID.String()) || strings.Contains(string(raw), "sourcePath") {
		t.Fatal("task leaked trusted importer internals")
	}
	// New websites receive a separate scope through the database trigger.
	u := mustUUIDFromUser(t, env, "platform-admin")
	site := createSite(t.Context(), t, env, "library-independent", "Independent", "", "zh-CN", u)
	var linked uuid.UUID
	if err := env.pool.QueryRow(t.Context(), `SELECT site_id FROM content_scopes WHERE id=$1 AND kind='site'`, site.Site.ID).Scan(&linked); err != nil || linked != site.Site.ID {
		t.Fatalf("site scope trigger: %v", err)
	}
}
