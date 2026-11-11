package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/siteprovision"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// contentWorld provisions one owner, one site, and wires the content store
// into the API.
func contentWorld(t *testing.T) (*testEnv, *tclient, string) {
	t.Helper()
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		store, err := content.New(e.provisionRoot)
		if err != nil {
			t.Fatalf("content store: %v", err)
		}
		e.mutateConfig = func(cfg *Config) { cfg.Content = store }
	})
	ctx := context.Background()
	owner, token, csrf := env.createUserSession("writer")
	site := createSite(ctx, t, env, "content-site", "Content Site", "", "zh-CN", owner.ID)
	// The store-level helper skips HTTP provisioning; run it directly so the
	// content trees exist.
	provisioner, err := siteprovision.New(env.provisionRoot)
	if err != nil {
		t.Fatalf("provisioner: %v", err)
	}
	provisioner.SeedFaviconDir = filepath.Join(repoRoot(t), "site", "static", "favicon")
	if err := provisioner.Provision(site.Site, site.Settings); err != nil {
		t.Fatalf("provision: %v", err)
	}
	return env, env.clientFor(token, csrf), site.Site.ID.String()
}

func postBody(category, slug, title string, draft bool) map[string]any {
	return map[string]any{
		"category": category, "slug": slug, "title": title,
		"date": "2026-11-11T10:00:00.000Z", "draft": draft,
		"categories": []string{"技术开发"}, "tags": []string{"test"},
		"description": "A test post.", "body": "## Hello\n\nBody text.",
	}
}

// TestPostsCRUDAndConcurrency covers the full post lifecycle through the
// API: create, list (search + pagination), read with digest, stale If-Match
// rejection, save, delete verification, duplicate creation, and permission
// boundaries.
func TestPostsCRUDAndConcurrency(t *testing.T) {
	env, c, siteID := contentWorld(t)
	ctx := context.Background()
	owner, _ := env.store.Users.GetByUsername(ctx, "writer")

	// Viewer and stranger boundaries.
	viewer, vtoken, vcsrf := env.createUserSession("cviewer")
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		mustUUID(siteID), viewer.ID); err != nil {
		t.Fatal(err)
	}
	_, strangerToken, strangerCSRF := env.createUserSession("cstranger")

	// Create two posts.
	status, hdr, raw := c.do("POST", "/api/v1/sites/"+siteID+"/posts", postBody("tech", "first-post", "First Post", false), nil)
	if status != http.StatusCreated {
		t.Fatalf("create post status = %d body = %s", status, raw)
	}
	var created content.Post
	decodeJSONBody(t, raw, &created)
	if created.Path != "tech/first-post.md" || created.Digest == "" {
		t.Fatalf("created post = %+v", created)
	}
	firstETag := strings.Trim(hdr.Get("ETag"), `"`)

	if status, _, raw = c.do("POST", "/api/v1/sites/"+siteID+"/posts", postBody("tech", "first-post", "Duplicate", false), nil); status != http.StatusConflict {
		t.Fatalf("duplicate create should conflict, got %d %s", status, raw)
	}

	status, _, _ = c.do("POST", "/api/v1/sites/"+siteID+"/posts", postBody("life", "draft-note", "Draft Note", true), nil)
	if status != http.StatusCreated {
		t.Fatalf("draft create status = %d", status)
	}

	// List: both, newest-first ordering irrelevant with 2 entries; query
	// narrows; pagination works.
	status, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/posts?query=first", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list status = %d", status)
	}
	var list struct {
		Posts []content.Post `json:"posts"`
		Total int            `json:"total"`
	}
	decodeJSONBody(t, raw, &list)
	if list.Total != 1 || list.Posts[0].Slug != "first-post" {
		t.Fatalf("filtered list = %+v total = %d", list.Posts, list.Total)
	}
	status, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/posts?pageSize=1&page=1", nil, nil)
	decodeJSONBody(t, raw, &list)
	if len(list.Posts) != 1 || list.Total != 2 {
		t.Fatalf("paged list len = %d total = %d", len(list.Posts), list.Total)
	}

	// Read one post (slash encoded).
	status, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/posts/tech%2Ffirst-post.md", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get post status = %d body = %s", status, raw)
	}
	var got content.Post
	decodeJSONBody(t, raw, &got)
	if got.Digest != firstETag || got.Body == "" || !strings.Contains(got.Body, "Body text.") {
		t.Fatalf("got post digest/body mismatch: %+v", got)
	}

	// Stale If-Match is rejected; fresh one saves.
	stale := `"sha256:` + strings.Repeat("a", 64) + `"`
	edit := postBody("tech", "first-post", "First Post (edited)", false)
	edit["body"] = "Edited body."
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/posts/tech%2Ffirst-post.md", edit, map[string]string{"If-Match": stale})
	if status != http.StatusPreconditionFailed {
		t.Fatalf("stale save status = %d body = %s", status, raw)
	}
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/posts/tech%2Ffirst-post.md", edit, map[string]string{"If-Match": `"` + firstETag + `"`})
	if status != http.StatusOK {
		t.Fatalf("save status = %d body = %s", status, raw)
	}
	var saved content.Post
	decodeJSONBody(t, raw, &saved)
	if saved.Title != "First Post (edited)" || saved.Digest == firstETag {
		t.Fatalf("saved post = %+v", saved)
	}

	// Viewer cannot write; stranger sees nothing.
	vc := env.clientFor(vtoken, vcsrf)
	status, _, raw = vc.do("PUT", "/api/v1/sites/"+siteID+"/posts/tech%2Ffirst-post.md", edit, map[string]string{"If-Match": `"` + saved.Digest + `"`})
	if status != http.StatusForbidden {
		t.Fatalf("viewer write status = %d body = %s", status, raw)
	}
	sc := env.clientFor(strangerToken, strangerCSRF)
	status, _, _ = sc.do("GET", "/api/v1/sites/"+siteID+"/posts/tech%2Ffirst-post.md", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("stranger read status = %d, want 404", status)
	}

	// Delete: wrong digest rejected, right digest succeeds, then 404.
	status, _, _ = c.do("DELETE", "/api/v1/sites/"+siteID+"/posts/life%2Fdraft-note.md", nil, map[string]string{"If-Match": `"sha256:` + strings.Repeat("0", 64) + `"`})
	if status != http.StatusPreconditionFailed {
		t.Fatalf("delete wrong digest status = %d", status)
	}
	_, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/posts/life%2Fdraft-note.md", nil, nil)
	var draftPost content.Post
	decodeJSONBody(t, raw, &draftPost)
	status, _, _ = c.do("DELETE", "/api/v1/sites/"+siteID+"/posts/life%2Fdraft-note.md", nil, map[string]string{"If-Match": `"` + draftPost.Digest + `"`})
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d", status)
	}
	status, _, _ = c.do("GET", "/api/v1/sites/"+siteID+"/posts/life%2Fdraft-note.md", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete status = %d", status)
	}
	_ = owner
}

// TestFriendsAndSocialLinks covers both data YAML editors end to end:
// validation failures, If-Match concurrency, and persistence.
func TestFriendsAndSocialLinks(t *testing.T) {
	_, c, siteID := contentWorld(t)

	status, _, raw := c.do("GET", "/api/v1/sites/"+siteID+"/content/friends", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get friends status = %d body = %s", status, raw)
	}
	var env1 struct {
		Friends content.FriendsDoc `json:"friends"`
		Digest  string             `json:"digest"`
	}
	decodeJSONBody(t, raw, &env1)

	// Invalid entry rejected.
	bad := content.FriendsDoc{Online: []content.Friend{{Name: "x", URL: "not-a-url"}}}
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/content/friends",
		map[string]any{"friends": bad}, map[string]string{"If-Match": `"` + env1.Digest + `"`})
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "validation_error") {
		t.Fatalf("invalid friends status = %d body = %s", status, raw)
	}

	good := content.FriendsDoc{
		Online: []content.Friend{{Name: "Alpha", URL: "https://alpha.example.org", Description: "ok"}},
	}
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/content/friends",
		map[string]any{"friends": good}, map[string]string{"If-Match": `"` + env1.Digest + `"`})
	if status != http.StatusOK {
		t.Fatalf("put friends status = %d body = %s", status, raw)
	}
	var saved struct {
		Friends content.FriendsDoc `json:"friends"`
		Digest  string             `json:"digest"`
	}
	decodeJSONBody(t, raw, &saved)
	if len(saved.Friends.Online) != 1 || saved.Friends.Online[0].Name != "Alpha" {
		t.Fatalf("saved friends = %+v", saved.Friends)
	}

	// Stale digest now conflicts.
	status, _, _ = c.do("PUT", "/api/v1/sites/"+siteID+"/content/friends",
		map[string]any{"friends": good}, map[string]string{"If-Match": `"` + env1.Digest + `"`})
	if status != http.StatusPreconditionFailed {
		t.Fatalf("stale friends save status = %d", status)
	}

	// Social links: replace and verify.
	status, _, raw = c.do("GET", "/api/v1/sites/"+siteID+"/content/social-links", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get social links status = %d", status)
	}
	var linkEnv struct {
		Links  []content.SocialLink `json:"links"`
		Digest string               `json:"digest"`
	}
	decodeJSONBody(t, raw, &linkEnv)
	links := []content.SocialLink{{Name: "GitHub", Href: "https://github.com/skyrocketingHong", Icon: "github"}}
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/content/social-links",
		map[string]any{"links": links}, map[string]string{"If-Match": `"` + linkEnv.Digest + `"`})
	if status != http.StatusOK {
		t.Fatalf("put social links status = %d body = %s", status, raw)
	}
	decodeJSONBody(t, raw, &linkEnv)
	if len(linkEnv.Links) != 1 || linkEnv.Links[0].Icon != "github" {
		t.Fatalf("saved links = %+v", linkEnv.Links)
	}
}

// TestSiteConfigSecretChannel proves the amap security code never leaves the
// server: reads report only whether it is set, and saves without the secret
// preserve the stored value.
func TestSiteConfigSecretChannel(t *testing.T) {
	env, c, siteID := contentWorld(t)

	status, _, raw := c.do("GET", "/api/v1/sites/"+siteID+"/site-config", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get config status = %d body = %s", status, raw)
	}
	var cfgEnv struct {
		Config content.SiteConfigDoc `json:"config"`
		SecSet bool                  `json:"amapSecurityCodeSet"`
		Digest string                `json:"digest"`
	}
	decodeJSONBody(t, raw, &cfgEnv)
	if cfgEnv.SecSet {
		t.Fatal("fresh scaffold must not report a set security code")
	}

	// Save with a secret; the response must not echo it.
	cfgEnv.Config.Services.Amap.BrowserKey = "test-browser-key"
	body := map[string]any{"config": cfgEnv.Config, "secrets": map[string]any{
		"amap": map[string]any{"securityCode": "super-secret-code"},
	}}
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/site-config", body, map[string]string{"If-Match": `"` + cfgEnv.Digest + `"`})
	if status != http.StatusOK {
		t.Fatalf("put config status = %d body = %s", status, raw)
	}
	var savedEnv struct {
		Config content.SiteConfigDoc `json:"config"`
		SecSet bool                  `json:"amapSecurityCodeSet"`
		Digest string                `json:"digest"`
	}
	decodeJSONBody(t, raw, &savedEnv)
	if !savedEnv.SecSet {
		t.Fatal("security code must be reported as set")
	}
	if strings.Contains(string(raw), "super-secret-code") {
		t.Fatal("security code leaked in the response")
	}

	// Save again WITHOUT the secret: the stored value survives.
	savedEnv.Config.SEO.Author = "Editor"
	body = map[string]any{"config": savedEnv.Config}
	status, _, raw = c.do("PUT", "/api/v1/sites/"+siteID+"/site-config", body, map[string]string{"If-Match": `"` + savedEnv.Digest + `"`})
	if status != http.StatusOK {
		t.Fatalf("second put status = %d body = %s", status, raw)
	}
	if !strings.Contains(string(raw), `"amapSecurityCodeSet":true`) {
		t.Fatalf("security code must survive an omitted save: %s", raw)
	}

	// The stored secret is present in the raw file for the theme's proxy
	// consumption.
	siteUUID := mustUUID(siteID)
	rawYAML, err := os.ReadFile(filepath.Join(env.provisionRoot, siteUUID.String(), "config", "site.yaml"))
	if err != nil || !strings.Contains(string(rawYAML), "super-secret-code") {
		t.Fatalf("site.yaml must carry the stored security code: %v", err)
	}
}

func mustUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

func TestContentAuthorityProductionBuildSlow(t *testing.T) {
	if testing.Short() {
		t.Skip("real Node/SvelteKit build")
	}
	buildsRoot := filepath.Join(t.TempDir(), "admin-builds")
	t.Cleanup(func() { _ = buildrunner.MakeTreeWritable(buildsRoot) })

	enq := &lateEnqueuer{}
	prev := &latePreviews{}
	var prodRunner *buildrunner.Runner
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		contentStore, err := content.New(e.provisionRoot)
		if err != nil {
			t.Fatalf("content store: %v", err)
		}
		e.mutateConfig = func(cfg *Config) {
			cfg.BuildEnqueuer = enq
			cfg.Previews = prev
			cfg.Content = contentStore
		}
		// Production runner: per-site content roots override the fixtures.
		prodSt := store.New(e.pool)
		prodRunner = buildrunner.NewRunner(buildrunner.Config{
			BuildStore: prodSt.Builds,
			ThemeStore: prodSt.Themes,
			Pool:       e.pool,
			RepoRoot:   repoRoot(t),
			BuildsRoot: buildsRoot,
			Mode:       "production",
			ContentRoots: func(siteID uuid.UUID) (string, string, string, error) {
				return contentStore.ContentRoot(siteID), contentStore.ConfigRoot(siteID), contentStore.FaviconRoot(siteID), nil
			},
			Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}, URLConventions: []string{"posts", "blog-rss", "sitemap"}},
		})
	})
	runner := prodRunner
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(runner.Stop)
	t.Cleanup(func() {
		if t.Failed() {
			_ = os.MkdirAll("/tmp/n3-failed-tasks", 0o755)
			_ = buildrunner.MakeTreeWritable(buildsRoot)
			_ = execCopyTree(buildsRoot, "/tmp/n3-failed-tasks")
		}
		_ = buildrunner.MakeTreeWritable(buildsRoot)
	})
	previews := buildrunner.NewPreviewServer(buildrunner.PreviewConfig{Target: env.store.Builds, BuildsRoot: buildsRoot, TTL: time.Hour})
	t.Cleanup(previews.Close)
	enq.set(runner)
	prev.set(previews)

	ctx := context.Background()
	owner, token, csrf := env.createUserSession("prodwriter")
	c := env.clientFor(token, csrf)
	site := createSite(ctx, t, env, "prod-site", "Prod Site", "", "zh-CN", owner.ID)
	siteID := site.Site.ID.String()
	provisioner, err := siteprovision.New(env.provisionRoot)
	if err != nil {
		t.Fatalf("provisioner: %v", err)
	}
	provisioner.SeedFaviconDir = filepath.Join(repoRoot(t), "site", "static", "favicon")
	if err := provisioner.Provision(site.Site, site.Settings); err != nil {
		t.Fatalf("provision: %v", err)
	}
	registerFirstTheme(t, env)
	selectThemeForSite(t, c, mustUUID(siteID), "fuyao-homepage", themeVersionFromCatalog(t, env))

	// Content edits through the API.
	if status, _, raw := c.do("POST", "/api/v1/sites/"+siteID+"/posts", postBody("tech", "published-one", "Published One", false), nil); status != http.StatusCreated {
		t.Fatalf("create published post: %d %s", status, raw)
	}
	if status, _, raw := c.do("POST", "/api/v1/sites/"+siteID+"/posts", postBody("tech", "hidden-draft", "Hidden Draft", true), nil); status != http.StatusCreated {
		t.Fatalf("create draft post: %d %s", status, raw)
	}
	_, _, rawFriends := c.do("GET", "/api/v1/sites/"+siteID+"/content/friends", nil, nil)
	var friendsEnv struct {
		Digest string `json:"digest"`
	}
	decodeJSONBody(t, rawFriends, &friendsEnv)
	if status, _, raw := c.do("PUT", "/api/v1/sites/"+siteID+"/content/friends",
		map[string]any{"friends": content.FriendsDoc{Online: []content.Friend{{Name: "Alpha", URL: "https://alpha.example.org"}}}},
		map[string]string{"If-Match": `"` + friendsEnv.Digest + `"`}); status != http.StatusOK {
		t.Fatalf("put friends: %d %s", status, raw)
	}

	// Build and wait for success.
	status, _, raw := c.do("POST", "/api/v1/sites/"+siteID+"/builds", map[string]string{}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("create build status = %d body = %s", status, raw)
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
		t.Fatalf("production build status = %s failure = %+v", build.Status, build.Failure)
	}

	// The frozen snapshot carries the API-written content; the draft is
	// excluded from the public index.
	snapshotPublic := filepath.Join(buildsRoot, build.ID, "snapshot", "public")
	allJSON, err := os.ReadFile(filepath.Join(snapshotPublic, "posts", "all.json"))
	if err != nil {
		t.Fatalf("snapshot all.json missing: %v", err)
	}
	if !strings.Contains(string(allJSON), "published-one") {
		t.Fatal("published post missing from the frozen snapshot index")
	}
	if strings.Contains(string(allJSON), "hidden-draft") {
		t.Fatal("draft post leaked into the frozen snapshot index")
	}
	friendsYAML, err := os.ReadFile(filepath.Join(snapshotPublic, "data", "friends.yaml"))
	if err != nil {
		t.Fatalf("snapshot friends.yaml missing: %v", err)
	}
	if !strings.Contains(string(friendsYAML), "alpha.example.org") {
		t.Fatal("friends edit missing from the frozen snapshot")
	}
}
