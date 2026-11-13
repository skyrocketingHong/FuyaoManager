package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/managedpublish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
)

type managedRootFake struct {
	mu                                  sync.Mutex
	current                             caddymanage.Result
	history                             map[string]caddymanage.Result
	sealed                              map[string]string
	siteID                              uuid.UUID
	builds, releases                    string
	counter, releaseCalls, restoreCalls int
	failRelease, failRestore            bool
	manager                             *publish.Manager
}

func (f *managedRootFake) control(_ context.Context, request caddymanage.Request) (caddymanage.Result, error) {
	data, _ := json.Marshal(request)
	request, err := caddymanage.DecodeRequest(bytes.NewReader(data))
	if err != nil {
		return caddymanage.Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if request.SiteKey != "managed-publish" {
		return caddymanage.Result{}, caddymanage.ErrNotRegistered
	}
	if request.Operation == "inspect" {
		return f.current, nil
	}
	if request.ExpectedDigest != f.current.SourceDigest {
		return caddymanage.Result{}, caddymanage.ErrConflict
	}
	switch request.Operation {
	case "adopt":
	case "release":
		f.releaseCalls++
		if request.SiteID != f.siteID.String() {
			return caddymanage.Result{}, caddymanage.ErrPermission
		}
		release, err := os.ReadFile(filepath.Join(f.releases, request.SiteID, request.ReleaseID, "release.json"))
		if err != nil || publicationmedia.Hash(release) != request.ManifestDigest {
			return caddymanage.Result{}, caddymanage.ErrConflict
		}
		media, err := os.ReadFile(filepath.Join(f.builds, request.ReleaseID, "publication-media", publicationmedia.ManifestName))
		if err != nil || publicationmedia.Hash(media) != request.MediaManifestDigest {
			return caddymanage.Result{}, caddymanage.ErrConflict
		}
		if previous, ok := f.sealed[request.ReleaseID]; ok && previous != request.ManifestDigest {
			return caddymanage.Result{}, caddymanage.ErrConflict
		}
		f.sealed[request.ReleaseID] = request.ManifestDigest
		if f.failRelease {
			return caddymanage.Result{}, caddymanage.ErrValidation
		}
		f.current.SourceDigest = publicationmedia.Hash([]byte("source:" + request.ReleaseID))
		f.current.PublishedReleaseID = request.ReleaseID
	case "restore":
		f.restoreCalls++
		if f.failRestore {
			return caddymanage.Result{}, caddymanage.ErrUnavailable
		}
		old, ok := f.history[request.RevisionID]
		if !ok {
			return caddymanage.Result{}, caddymanage.ErrConflict
		}
		f.current = old
	default:
		return caddymanage.Result{}, caddymanage.ErrInvalidRequest
	}
	f.counter++
	f.current.Adopted = true
	f.current.RootRevisionID = fmt.Sprintf("%032x", f.counter)
	f.history[f.current.RootRevisionID] = f.current
	return f.current, nil
}

func managedPublicationWorld(t *testing.T) (*testEnv, *tclient, uuid.UUID, *managedRootFake, func(string) uuid.UUID) {
	t.Helper()
	builds, releases := t.TempDir(), t.TempDir()
	root := &managedRootFake{current: caddymanage.Result{Registered: true, PublicationEnabled: true, SourceDigest: strings.Repeat("a", 64), Settings: caddymanage.Settings{Compression: "preserve", CacheControl: "preserve", SecurityHeaders: "preserve"}}, history: map[string]caddymanage.Result{}, sealed: map[string]string{}, builds: builds, releases: releases}
	env := setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatal(err)
		}
		env.mutateConfig = func(cfg *Config) {
			st := store.New(env.pool)
			contracts, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
			if err != nil {
				t.Fatal(err)
			}
			cfg.DeploymentEnabled = false
			cfg.CaddyManagementEnabled = true
			cfg.CaddyControl = root.control
			root.manager = publish.NewManager(publish.Config{Pool: env.pool, Releases: st.Releases, Builds: st.Builds, Themes: st.Themes, ReleasesRoot: releases, Contracts: contracts, Requirements: buildrunner.SiteRequirements{Capabilities: []string{"blog"}}, Activation: managedpublish.New(managedpublish.Config{Store: st, Control: root.control, Enabled: true, ReleasesRoot: releases, BuildsRoot: builds})})
			cfg.Publisher = root.manager
		}
	})
	t.Cleanup(func() { _ = buildrunner.MakeTreeWritable(releases); _ = buildrunner.MakeTreeWritable(builds) })
	actor, client := adminClient(env)
	if _, err := env.store.SyncCaddySites(t.Context(), actor.ID, []caddyconfig.Site{{ID: "managed-publish", Kind: caddyconfig.KindStatic, Root: t.TempDir(), Hosts: []string{"publication.example"}, Listen: []string{":443"}, ReadOnly: true}}); err != nil {
		t.Fatal(err)
	}
	var siteID uuid.UUID
	if err := env.pool.QueryRow(t.Context(), `SELECT site_id FROM caddy_sites WHERE source_key='managed-publish'`).Scan(&siteID); err != nil {
		t.Fatal(err)
	}
	root.siteID = siteID
	themeRow, digest, themeID, version := fakeThemeFixture(t, env)
	if _, err := env.pool.Exec(t.Context(), `INSERT INTO site_theme_selections(site_id,theme_id,theme_config,revision,selected_by) VALUES($1,$2,'{}',1,$3)`, siteID, themeRow, actor.ID); err != nil {
		t.Fatal(err)
	}
	caddyRequest(t, client, "POST", "/api/v1/sites/"+siteID.String()+"/caddy/convert", map[string]any{}, store.CaddyETag(siteID, nil), 200)
	makeBuild := func(marker string) uuid.UUID {
		buildID := insertFakeSucceededBuild(t, env, siteID, actor.ID, themeRow, themeID, version, digest, marker)
		if err := os.MkdirAll(filepath.Join(builds, buildID.String()), 0750); err != nil {
			t.Fatal(err)
		}
		mediaSource, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := publicationmedia.Freeze(mediaSource, filepath.Join(builds, buildID.String(), "publication-media"), siteID, buildID); err != nil {
			t.Fatal(err)
		}
		return buildID
	}
	return env, client, siteID, root, makeBuild
}

func assertManagedPublicationCoherent(t *testing.T, env *testEnv, siteID uuid.UUID, root *managedRootFake, want uuid.UUID) {
	t.Helper()
	var current *uuid.UUID
	if err := env.pool.QueryRow(t.Context(), `SELECT current_release_id FROM sites WHERE id=$1`, siteID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if want == uuid.Nil {
		if current != nil {
			t.Fatal("uncommitted publication was marked live")
		}
	} else if current == nil || *current != want {
		t.Fatal("database points to a different live release")
	}
	management, err := env.store.GetCaddyManagement(t.Context(), siteID)
	if err != nil {
		t.Fatal(err)
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if management.SourceDigest != root.current.SourceDigest || management.RootRevisionID != root.current.RootRevisionID {
		t.Fatal("root and database revision metadata disagree")
	}
	if want == uuid.Nil {
		if root.current.PublishedReleaseID != "" {
			t.Fatal("compensated root still publishes the failed build")
		}
	} else {
		var dir string
		if err := env.pool.QueryRow(t.Context(), `SELECT release_dir FROM site_releases WHERE id=$1`, want).Scan(&dir); err != nil || dir != root.current.PublishedReleaseID {
			t.Fatal("root publication and live release disagree")
		}
	}
}

func TestManagedPublishActivatesBeforeCommitAndRollsBackWithoutLegacyDeployment(t *testing.T) {
	env, client, site, root, build := managedPublicationWorld(t)
	path := "/api/v1/sites/" + site.String() + "/releases"
	_, raw := adminRequest(t, client, "POST", path, map[string]any{"buildId": build("first")}, 201)
	var first releaseResp
	decodeJSONBody(t, raw, &first)
	assertManagedPublicationCoherent(t, env, site, root, first.ID)
	_, raw = adminRequest(t, client, "POST", path, map[string]any{"buildId": build("second")}, 201)
	var second releaseResp
	decodeJSONBody(t, raw, &second)
	assertManagedPublicationCoherent(t, env, site, root, second.ID)
	adminRequest(t, client, "POST", path+"/"+first.ID.String()+"/rollback", map[string]any{}, 200)
	assertManagedPublicationCoherent(t, env, site, root, first.ID)
	var entries int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM site_network_entries WHERE site_id=$1`, site).Scan(&entries); err != nil || entries != 0 {
		t.Fatal("managed Caddy publication opened a legacy port")
	}
}

func TestManagedPublishHelperFailureNeverMarksLiveAndCanRetryFrozenBuild(t *testing.T) {
	env, client, site, root, build := managedPublicationWorld(t)
	id := build("retry")
	root.failRelease = true
	path := "/api/v1/sites/" + site.String() + "/releases"
	status, _, raw := client.do("POST", path, map[string]any{"buildId": id}, nil)
	if status < 400 {
		t.Fatalf("failed root returned %d %s", status, raw)
	}
	assertManagedPublicationCoherent(t, env, site, root, uuid.Nil)
	root.failRelease = false
	_, raw = adminRequest(t, client, "POST", path, map[string]any{"buildId": id}, 201)
	var release releaseResp
	decodeJSONBody(t, raw, &release)
	assertManagedPublicationCoherent(t, env, site, root, release.ID)
}

func TestManagedPublishCommitFailureCompensatesAndPreservesRetryIdentity(t *testing.T) {
	env, client, site, root, build := managedPublicationWorld(t)
	id := build("commit-failure")
	_, err := env.pool.Exec(t.Context(), `CREATE FUNCTION reject_managed_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.root_revision_id='00000000000000000000000000000002' THEN RAISE EXCEPTION 'injected deferred failure'; END IF; RETURN NEW; END $$;
 CREATE CONSTRAINT TRIGGER reject_managed_commit AFTER UPDATE ON caddy_management DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_managed_commit()`)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/sites/" + site.String() + "/releases"
	status, _, _ := client.do("POST", path, map[string]any{"buildId": id}, nil)
	if status < 400 {
		t.Fatal("failed database commit reported live")
	}
	assertManagedPublicationCoherent(t, env, site, root, uuid.Nil)
	if root.restoreCalls != 1 {
		t.Fatal("root apply was not compensated after failed database commit")
	}
	var live int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM site_releases WHERE site_id=$1 AND status='live'`, site).Scan(&live); err != nil || live != 0 {
		t.Fatal("failed commit left a false live row")
	}
	_, raw := adminRequest(t, client, "POST", path, map[string]any{"buildId": id}, 201)
	var release releaseResp
	decodeJSONBody(t, raw, &release)
	assertManagedPublicationCoherent(t, env, site, root, release.ID)
}

func TestManagedPublishUncertainCompensationBlocksLaterWrites(t *testing.T) {
	env, client, site, root, build := managedPublicationWorld(t)
	id := build("uncertain")
	_, err := env.pool.Exec(t.Context(), `CREATE FUNCTION reject_uncertain_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.root_revision_id='00000000000000000000000000000002' THEN RAISE EXCEPTION 'injected deferred failure'; END IF; RETURN NEW; END $$;
 CREATE CONSTRAINT TRIGGER reject_uncertain_commit AFTER UPDATE ON caddy_management DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_uncertain_commit()`)
	if err != nil {
		t.Fatal(err)
	}
	root.failRestore = true
	path := "/api/v1/sites/" + site.String() + "/releases"
	status, _, _ := client.do("POST", path, map[string]any{"buildId": id}, nil)
	if status < 400 {
		t.Fatal("unreconciled commit reported success")
	}
	calls := root.releaseCalls
	status, _, _ = client.do("POST", path, map[string]any{"buildId": id}, nil)
	if status < 400 || root.releaseCalls != calls {
		t.Fatal("unreconciled root state accepted another publication")
	}
	var current *uuid.UUID
	if err := env.pool.QueryRow(t.Context(), `SELECT current_release_id FROM sites WHERE id=$1`, site).Scan(&current); err != nil || current != nil {
		t.Fatal("uncertain root was disguised as a live database pointer")
	}
}

func TestManagedPublishRequiresCurrentAdministratorOwner(t *testing.T) {
	env, client, site, _, build := managedPublicationWorld(t)
	id := build("permissions")
	path := "/api/v1/sites/" + site.String() + "/releases"
	user, token, csrf := env.createUserSession("managed-editor")
	if _, err := env.pool.Exec(t.Context(), `INSERT INTO site_memberships(site_id,user_id,role) VALUES($1,$2,'editor')`, site, user.ID); err != nil {
		t.Fatal(err)
	}
	status, _, _ := env.clientFor(token, csrf).do("POST", path, map[string]any{"buildId": id}, nil)
	if status < 400 {
		t.Fatal("member editor published through root capability")
	}
	actor, _ := env.store.Users.GetByUsername(t.Context(), "platform-admin")
	if _, err := env.pool.Exec(t.Context(), `UPDATE site_memberships SET role='viewer' WHERE site_id=$1 AND user_id=$2`, site, actor.ID); err != nil {
		t.Fatal(err)
	}
	status, _, _ = client.do("POST", path, map[string]any{"buildId": id}, nil)
	if status < 400 {
		t.Fatal("administrator without site ownership published")
	}
}

func TestManagedPublishBridgeRechecksRoleWithoutRelyingOnHTTPGate(t *testing.T) {
	env, _, site, root, build := managedPublicationWorld(t)
	id := build("direct-permission")
	actor, err := env.store.Users.GetByUsername(t.Context(), "platform-admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(t.Context(), `UPDATE users SET platform_role='member' WHERE id=$1`, actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := root.manager.Publish(t.Context(), site, id, actor.ID, nil); err != store.ErrOperatorForbidden {
		t.Fatalf("bridge did not recheck current platform role: %v", err)
	}
	if root.releaseCalls != 0 {
		t.Fatal("demoted actor reached the root publisher")
	}
}
