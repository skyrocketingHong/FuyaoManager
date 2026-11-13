package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentbind"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/siteprovision"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"gopkg.in/yaml.v3"
)

type workspaceTestWorld struct {
	env            *testEnv
	local, library *content.Store
	owner          model.User
	client         *tclient
}

func workspaceWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0640); err != nil {
		t.Fatal(err)
	}
}
func workspaceYAML(t *testing.T, s *content.Store, id uuid.UUID, name string, value any) {
	t.Helper()
	raw, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	workspaceWrite(t, filepath.Join(s.ContentRoot(id), name), raw)
}
func newWorkspaceTestWorld(t *testing.T) *workspaceTestWorld {
	t.Helper()
	library, err := content.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"posts", "data", "albums/metadata", "albums/photos"} {
		if err := os.MkdirAll(filepath.Join(library.ContentRoot(content.PlatformScopeID), directory), 0750); err != nil {
			t.Fatal(err)
		}
	}
	workspaceWrite(t, filepath.Join(library.ConfigRoot(content.PlatformScopeID), "site.yaml"), []byte("services:\n  amap:\n    browserKey: browser-test-key\n    serviceHost: https://maps.fixture.invalid\n    securityCode: TEST-AMAP-SECRET\n"))
	control := newCaddyControlFake()
	var local *content.Store
	env := setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatal(err)
		}
		local, err = content.New(env.provisionRoot)
		if err != nil {
			t.Fatal(err)
		}
		bindings := &contentbind.Store{Store: local, Platform: library, Resolve: store.New(env.pool).GetContentSources}
		env.mutateConfig = func(cfg *Config) {
			cfg.Content = bindings
			cfg.PlatformContent = library
			cfg.CaddyControl = control.control
		}
	})
	owner, client := adminClient(env)
	return &workspaceTestWorld{env: env, local: local, library: library, owner: owner, client: client}
}
func (w *workspaceTestWorld) createSite(t *testing.T, slug string) store.SiteRecord {
	t.Helper()
	record := createSite(t.Context(), t, w.env, slug, slug, "", "en-US", w.owner.ID)
	provisioner, err := siteprovision.New(w.env.provisionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := provisioner.Provision(record.Site, record.Settings, "https://"+slug+".fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	return record
}
func workspaceRequest(t *testing.T, client *tclient, method, path string, body any, etag string, want int) (http.Header, []byte) {
	t.Helper()
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	status, response, raw := client.do(method, path, body, headers)
	if status != want {
		t.Fatalf("%s %s: status=%d want=%d response=%s", method, path, status, want, raw)
	}
	return response, raw
}
func workspaceSources(t *testing.T, client *tclient, siteID uuid.UUID) (store.ContentSourceRecord, string) {
	t.Helper()
	headers, raw := workspaceRequest(t, client, "GET", "/api/v1/sites/"+siteID.String()+"/content-sources", nil, "", 200)
	var value store.ContentSourceRecord
	decodeJSONBody(t, raw, &value)
	if headers.Get("ETag") == "" {
		t.Fatal("content sources omitted their ETag")
	}
	return value, headers.Get("ETag")
}
func seedWorkspaceContent(t *testing.T, s *content.Store, id uuid.UUID, prefix string, posts, photos, friends int, years []int, places content.FootprintsDoc) {
	t.Helper()
	for i := 0; i < posts; i++ {
		if _, err := s.CreatePost(id, content.Post{Category: "tech", Slug: fmt.Sprintf("%s-%d", prefix, i), Title: prefix, Date: "2026-11-13", Body: prefix}); err != nil {
			t.Fatal(err)
		}
	}
	doc := content.FriendsDoc{Online: []content.Friend{}, Offline: []content.Friend{}}
	for i := 0; i < friends; i++ {
		doc.Online = append(doc.Online, content.Friend{Name: fmt.Sprintf("%s-%d", prefix, i), URL: "https://friend.fixture.invalid"})
	}
	workspaceYAML(t, s, id, "data/friends.yaml", doc)
	workspaceYAML(t, s, id, "data/footprints.yaml", places)
	album, err := json.Marshal(content.AlbumIndex{SchemaVersion: 1, TotalPhotos: photos, Years: years, Devices: []any{}})
	if err != nil {
		t.Fatal(err)
	}
	workspaceWrite(t, filepath.Join(s.ContentRoot(id), "albums/metadata/index.json"), album)
}

func TestWorkspacePlatformCountsReflectContentAndHideSecrets(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	places := footprintsDoc()
	city := places.Cities[0]
	city.ID = "second-city"
	city.Places = nil
	places.Cities = append(places.Cities, city)
	seedWorkspaceContent(t, w.library, content.PlatformScopeID, "library", 2, 7, 3, []int{2024, 2026}, places)
	_, token, csrf := w.env.createUserSession("statistics-reader")
	reader := w.env.clientFor(token, csrf)
	for _, entry := range []struct {
		client  *tclient
		canEdit bool
	}{{w.client, true}, {reader, false}} {
		headers, raw := workspaceRequest(t, entry.client, "GET", "/api/v1/content", nil, "", 200)
		var result struct {
			Counts  contentCounts     `json:"counts"`
			CanEdit bool              `json:"canEdit"`
			Amap    map[string]string `json:"amap"`
		}
		decodeJSONBody(t, raw, &result)
		if result.Counts != (contentCounts{Posts: 2, Albums: 2, Photos: 7, Places: 3, Friends: 3}) || result.CanEdit != entry.canEdit {
			t.Fatalf("wrong content statistics or permission: %+v", result)
		}
		if result.Amap["browserKey"] != "browser-test-key" || headers.Get("Cache-Control") != "no-store" {
			t.Fatal("platform metadata lost safe configuration or cache policy")
		}
		if strings.Contains(string(raw), "TEST-AMAP-SECRET") || strings.Contains(string(raw), "securityCode") {
			t.Fatal("platform metadata leaked a write-only secret")
		}
	}
	workspaceWrite(t, filepath.Join(w.library.ContentRoot(content.PlatformScopeID), "albums/metadata/index.json"), []byte("malformed-json"))
	_, raw := workspaceRequest(t, w.client, "GET", "/api/v1/content", nil, "", 500)
	if strings.Contains(string(raw), `"counts"`) {
		t.Fatal("unreadable content was presented as zero counts")
	}
}

func TestWorkspaceContentSourcesRequireOwnerExactETagAndOneConcurrentWinner(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "source-owner")
	path := "/api/v1/sites/" + site.Site.ID.String() + "/content-sources"
	initial, etag := workspaceSources(t, w.client, site.Site.ID)
	if initial.Sources != store.IndependentSources() {
		t.Fatal("new site did not start with independent sources")
	}
	for _, role := range []string{"editor", "viewer"} {
		user, token, csrf := w.env.createUserSession("source-" + role)
		if _, err := w.env.pool.Exec(t.Context(), `INSERT INTO site_memberships(site_id,user_id,role) VALUES($1,$2,$3)`, site.Site.ID, user.ID, role); err != nil {
			t.Fatal(err)
		}
		client := w.env.clientFor(token, csrf)
		workspaceRequest(t, client, "GET", path, nil, "", 200)
		workspaceRequest(t, client, "PUT", path, initial, etag, 403)
	}
	workspaceRequest(t, w.client, "PUT", path, initial, "", 428)
	badRevision := initial
	badRevision.Revision++
	workspaceRequest(t, w.client, "PUT", path, badRevision, etag, 412)
	invalid := initial
	invalid.Sources.Posts = "file-path"
	workspaceRequest(t, w.client, "PUT", path, invalid, etag, 422)
	start := make(chan struct{})
	results := make(chan int, 2)
	var group sync.WaitGroup
	for _, category := range []string{"posts", "friends"} {
		body := initial
		if category == "posts" {
			body.Sources.Posts = "platform"
		} else {
			body.Sources.Friends = "platform"
		}
		group.Go(func() {
			<-start
			status, _, _ := w.client.do("PUT", path, body, map[string]string{"If-Match": etag})
			results <- status
		})
	}
	close(start)
	group.Wait()
	close(results)
	statuses := map[int]int{}
	for status := range results {
		statuses[status]++
	}
	if statuses[200] != 1 || statuses[412] != 1 {
		t.Fatalf("ETag race did not have one winner: %v", statuses)
	}
	current, newETag := workspaceSources(t, w.client, site.Site.ID)
	if current.Revision != initial.Revision+1 || newETag == etag || (current.Sources.Posts == "platform") == (current.Sources.Friends == "platform") {
		t.Fatalf("lost or combined concurrent bindings: %+v", current)
	}
	workspaceRequest(t, w.client, "PUT", path, initial, etag, 412)
	var releases int
	if err := w.env.pool.QueryRow(t.Context(), `SELECT count(*) FROM site_releases WHERE site_id=$1`, site.Site.ID).Scan(&releases); err != nil || releases != 0 {
		t.Fatalf("binding a source published a site: %d %v", releases, err)
	}
}

func TestWorkspaceSharedContentIsReadOnlyAndSiteConfigurationStaysIndependent(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "shared-site")
	seedWorkspaceContent(t, w.library, content.PlatformScopeID, "library", 2, 7, 2, []int{2026}, footprintsDoc())
	localPlaces := footprintsDoc()
	localPlaces.Cities[0].Places = nil
	seedWorkspaceContent(t, w.local, site.Site.ID, "local", 1, 3, 1, []int{2025}, localPlaces)
	base := "/api/v1/sites/" + site.Site.ID.String()
	record, etag := workspaceSources(t, w.client, site.Site.ID)
	record.Sources = store.ContentSources{Posts: "platform", Albums: "platform", Footprints: "platform", Friends: "platform"}
	workspaceRequest(t, w.client, "PUT", base+"/content-sources", record, etag, 200)
	for _, path := range []string{"/posts", "/content/friends", "/content/footprints", "/albums"} {
		_, raw := workspaceRequest(t, w.client, "GET", base+path, nil, "", 200)
		var envelope map[string]json.RawMessage
		decodeJSONBody(t, raw, &envelope)
		if string(envelope["canEdit"]) != "false" {
			t.Fatalf("shared %s is writable", path)
		}
	}
	for _, entry := range []struct {
		method, path string
		body         any
	}{{"POST", "/posts", postBody("tech", "blocked", "Blocked", false)}, {"PUT", "/content/friends", map[string]any{"friends": content.FriendsDoc{}}}, {"PUT", "/content/footprints", map[string]any{"footprints": footprintsDoc()}}, {"POST", "/albums/process", map[string]any{}}} {
		_, raw := workspaceRequest(t, w.client, entry.method, base+entry.path, entry.body, `"any-content-digest"`, 409)
		var problem problemResp
		decodeJSONBody(t, raw, &problem)
		if problem.Code != "content_source_shared" {
			t.Fatalf("wrong shared content failure: %+v", problem)
		}
	}
	globalConfig := filepath.Join(w.library.ConfigRoot(content.PlatformScopeID), "site.yaml")
	before, err := os.ReadFile(globalConfig)
	if err != nil {
		t.Fatal(err)
	}
	headers, raw := workspaceRequest(t, w.client, "GET", base+"/site-config", nil, "", 200)
	var config struct {
		Config content.SiteConfigDoc `json:"config"`
	}
	decodeJSONBody(t, raw, &config)
	config.Config.Profile.Name = "Independent site profile"
	workspaceRequest(t, w.client, "PUT", base+"/site-config", map[string]any{"config": config.Config}, headers.Get("ETag"), 200)
	after, err := os.ReadFile(globalConfig)
	if err != nil || string(after) != string(before) {
		t.Fatal("site.yaml save changed the platform configuration")
	}
	current, etag := workspaceSources(t, w.client, site.Site.ID)
	current.Sources.Posts = "independent"
	workspaceRequest(t, w.client, "PUT", base+"/content-sources", current, etag, 200)
	headers, raw = workspaceRequest(t, w.client, "GET", base+"/posts/tech%2Flocal-0.md", nil, "", 200)
	var post postContentDTO
	decodeJSONBody(t, raw, &post)
	if !post.CanEdit || post.Title != "local" {
		t.Fatal("independent post was not retained after switching sources")
	}
	workspaceRequest(t, w.client, "PUT", base+"/posts/tech%2Flocal-0.md", postBody("tech", "local-0", "Edited independent", false), headers.Get("ETag"), 200)
	libraryPost, err := w.library.GetPost(content.PlatformScopeID, "tech/library-0.md")
	if err != nil || libraryPost.Title != "library" {
		t.Fatalf("independent edit affected library: %v %+v", err, libraryPost)
	}
}

func TestWorkspaceOverviewAggregatesResolvedSources(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	site := w.createSite(t, "mixed-statistics")
	globalPlaces := footprintsDoc()
	extra := globalPlaces.Cities[0]
	extra.ID = "second"
	extra.Places = nil
	globalPlaces.Cities = append(globalPlaces.Cities, extra)
	localPlaces := footprintsDoc()
	localPlaces.Cities[0].Places = nil
	seedWorkspaceContent(t, w.library, content.PlatformScopeID, "library", 2, 7, 4, []int{2024, 2026}, globalPlaces)
	seedWorkspaceContent(t, w.local, site.Site.ID, "local", 1, 3, 1, []int{2025}, localPlaces)
	record, etag := workspaceSources(t, w.client, site.Site.ID)
	record.Sources.Posts, record.Sources.Footprints = "platform", "platform"
	base := "/api/v1/sites/" + site.Site.ID.String()
	workspaceRequest(t, w.client, "PUT", base+"/content-sources", record, etag, 200)
	_, raw := workspaceRequest(t, w.client, "GET", base+"/overview", nil, "", 200)
	var result struct {
		Counts    contentCounts        `json:"counts"`
		Sources   store.ContentSources `json:"sources"`
		Theme     siteThemeResp        `json:"theme"`
		Analytics map[string]any       `json:"analytics"`
	}
	decodeJSONBody(t, raw, &result)
	if result.Counts != (contentCounts{Posts: 2, Albums: 1, Photos: 3, Places: 3, Friends: 1}) || result.Sources != record.Sources {
		t.Fatalf("overview ignored selected sources: %+v", result)
	}
	if result.Theme.Selected || result.Analytics["configured"] != false || result.Analytics["available"] != false || result.Analytics["latest"] != nil {
		t.Fatal("overview fabricated a theme or traffic values")
	}
}

func TestWorkspaceAdoptedStaticCanSelectThemeWithoutChangingSourceIdentity(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	root := t.TempDir()
	marker := filepath.Join(root, "index.html")
	workspaceWrite(t, marker, []byte("existing public website"))
	entry := caddyconfig.Site{ID: "adopted-static", Kind: caddyconfig.KindStatic, Root: root, Hosts: []string{"adopted.fixture.invalid"}, Listen: []string{":443"}, ReadOnly: true}
	if _, err := w.env.store.SyncCaddySites(t.Context(), w.owner.ID, []caddyconfig.Site{entry}); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := w.env.pool.QueryRow(t.Context(), `SELECT site_id FROM caddy_sites WHERE source_key=$1`, entry.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/sites/" + id.String()
	headers, _ := workspaceRequest(t, w.client, "GET", base+"/caddy", nil, "", 200)
	workspaceRequest(t, w.client, "POST", base+"/caddy/convert", map[string]any{}, headers.Get("ETag"), 200)
	_, raw := workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	var before siteDTO
	decodeJSONBody(t, raw, &before)
	if before.Kind != "static" || before.ManagementMode != "platform" || !before.Workspace.ThemeSelectable || before.Workspace.ThemeEnabled || before.Workspace.Buildable || !before.Workspace.HasCaddy {
		t.Fatalf("adopted static workspace: %+v", before)
	}
	workspaceRequest(t, w.client, "POST", base+"/posts", postBody("tech", "pre-theme", "Before theme selection", false), "", 201)
	registerFirstTheme(t, w.env)
	version := themeVersionFromCatalog(t, w.env)
	headers, _ = workspaceRequest(t, w.client, "GET", base+"/theme", nil, "", 200)
	workspaceRequest(t, w.client, "PUT", base+"/theme", themePutBody(fixtureThemeID, version, "{}"), headers.Get("ETag"), 200)
	_, raw = workspaceRequest(t, w.client, "GET", base, nil, "", 200)
	var after siteDTO
	decodeJSONBody(t, raw, &after)
	if after.Kind != "static" || after.ManagementMode != "platform" || !after.Workspace.ThemeEnabled || !after.Workspace.Buildable || !after.Workspace.HasCaddy {
		t.Fatalf("theme selection lost source identity or did not enable builds: %+v", after)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "existing public website" {
		t.Fatal("selecting a theme modified the running website")
	}
	var builds, releases int
	if err := w.env.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM site_builds WHERE site_id=$1),(SELECT count(*) FROM site_releases WHERE site_id=$1)`, id).Scan(&builds, &releases); err != nil || builds != 0 || releases != 0 {
		t.Fatalf("selecting a theme built or published automatically: %d %d %v", builds, releases, err)
	}
}

func TestWorkspaceInapplicableCaddyKindsCannotBindContentSources(t *testing.T) {
	w := newWorkspaceTestWorld(t)
	for _, kind := range []caddyconfig.Kind{caddyconfig.KindStatic, caddyconfig.KindRedirect, caddyconfig.KindReverseProxy, caddyconfig.KindUnsupported} {
		t.Run(string(kind), func(t *testing.T) {
			entry := caddyconfig.Site{ID: "unmanaged-" + string(kind), Kind: kind, Hosts: []string{string(kind) + ".fixture.invalid"}, Listen: []string{":443"}, Root: t.TempDir(), ReadOnly: true}
			if _, err := w.env.store.SyncCaddySites(t.Context(), w.owner.ID, []caddyconfig.Site{entry}); err != nil {
				t.Fatal(err)
			}
			var id uuid.UUID
			if err := w.env.pool.QueryRow(t.Context(), `SELECT site_id FROM caddy_sites WHERE source_key=$1`, entry.ID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			record, etag := workspaceSources(t, w.client, id)
			_, raw := workspaceRequest(t, w.client, "PUT", "/api/v1/sites/"+id.String()+"/content-sources", record, etag, 409)
			var problem problemResp
			decodeJSONBody(t, raw, &problem)
			if problem.Code != "site_kind_mismatch" {
				t.Fatalf("wrong kind rejection: %+v", problem)
			}
		})
	}
}
