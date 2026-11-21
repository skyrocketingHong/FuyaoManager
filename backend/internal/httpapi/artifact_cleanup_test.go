package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestStaticArtifactDeletionProtectsLivePreviewAndRelease(t *testing.T) {
	env, buildsRoot, releasesRoot := setupPublish(t)
	ctx := context.Background()
	owner, token, csrf := env.createUserSession("artifactowner")
	editor, editorToken, editorCSRF := env.createUserSession("artifacteditor")
	if _, err := env.pool.Exec(ctx, `UPDATE users SET platform_role='admin' WHERE id=ANY($1)`, []uuid.UUID{owner.ID, editor.ID}); err != nil {
		t.Fatal(err)
	}
	site := createSite(ctx, t, env, "cleanup-test", "Cleanup test", "", "en-US", owner.ID).Site
	if _, err := env.pool.Exec(ctx, `INSERT INTO site_memberships (site_id,user_id,role) VALUES ($1,$2,'editor')`, site.ID, editor.ID); err != nil {
		t.Fatal(err)
	}
	client := env.clientFor(token, csrf)
	editorClient := env.clientFor(editorToken, editorCSRF)
	base := "/api/v1/sites/" + site.ID.String()
	row, digest, theme, version := fakeThemeFixture(t, env)
	selectThemeForSite(t, client, site.ID, theme, version)
	publishBuild := func(marker string) (uuid.UUID, releaseResp) {
		t.Helper()
		build := insertFakeSucceededBuild(t, env, site.ID, owner.ID, row, theme, version, digest, marker)
		task, err := env.store.Builds.Get(ctx, build)
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(buildsRoot, build.String(), "artifacts")
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			t.Fatal(err)
		}
		// Move within the test's own filesystem and make the artifact
		// directory read-only as the real runner does.
		if err := os.Rename(task.ArtifactPath, output); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(output, 0o555); err != nil {
			t.Fatal(err)
		}
		if err := env.store.Builds.MarkSucceeded(ctx, build, output, task.InputDigests); err != nil {
			t.Fatal(err)
		}
		return build, publishViaAPI(t, client, site.ID, build.String())
	}
	a, old := publishBuild("old")
	b, newer := publishBuild("newer")
	c, live := publishBuild("live")
	body := func(ids ...uuid.UUID) any { return map[string][]uuid.UUID{"ids": ids} }
	buildCleanup := base + "/builds/artifacts/delete"
	releaseCleanup := base + "/releases/artifacts/delete"
	expect := func(requester *tclient, url string, payload any, code int) {
		t.Helper()
		if status, _, data := requester.do(http.MethodPost, url, payload, nil); status != code {
			t.Fatalf("%s: HTTP %d want %d, %s", url, status, code, data)
		}
	}
	expect(editorClient, buildCleanup, body(a), http.StatusForbidden)
	expect(client, releaseCleanup, body(old.ID, live.ID), http.StatusConflict)
	if state, err := env.store.Releases.GetForUser(ctx, old.ID, site.ID, owner.ID); err != nil || state.ArtifactState != "available" {
		t.Fatalf("protected batch changed an eligible record: %+v %v", state, err)
	}
	pointer := filepath.Join(releasesRoot, site.ID.String(), "current")
	if err := os.Symlink(a.String(), pointer); err != nil {
		t.Fatal(err)
	}
	expect(client, releaseCleanup, body(old.ID, newer.ID), http.StatusConflict)
	if err := os.Remove(pointer); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(c.String(), pointer); err != nil {
		t.Fatal(err)
	}
	expect(client, releaseCleanup, body(old.ID, newer.ID), http.StatusNoContent)
	expect(client, releaseCleanup, body(old.ID, newer.ID), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(releasesRoot, site.ID.String(), a.String())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed release artifacts remain: %v", err)
	}
	expect(client, base+"/releases/"+old.ID.String()+"/rollback", nil, http.StatusConflict)

	// Reusing a build after retiring its release must create a distinct row
	// that cannot be touched by retries for the old release.
	republished := publishViaAPI(t, client, site.ID, a.String())
	if republished.ID == old.ID {
		t.Fatal("old release history was overwritten")
	}
	expect(client, releaseCleanup, body(old.ID), http.StatusNoContent)
	if html := fetchPublished(t, currentOriginURL(t, client, site.ID), ""); !strings.Contains(html, "fake:old") {
		t.Fatal("retry for old release changed the live publication")
	}

	preview := base + "/builds/" + b.String()
	// A port reservation also protects previews opened by another process.
	if _, _, err := env.store.NetworkEntries.EnsurePreviewEntry(ctx, site.ID, b, 25480, 25490); err != nil {
		t.Fatalf("reserve preview port: %v", err)
	}
	expect(client, buildCleanup, body(b, c), http.StatusConflict)
	if err := env.store.NetworkEntries.ReleasePreview(ctx, b); err != nil {
		t.Fatal(err)
	}
	expect(client, preview+"/preview-links", nil, http.StatusCreated)
	expect(client, buildCleanup, body(b, c), http.StatusConflict)
	expect(client, preview+"/preview-close", nil, http.StatusNoContent)
	expect(client, buildCleanup, body(b, c), http.StatusNoContent)
	expect(client, preview+"/preview-links", nil, http.StatusConflict)
	if _, _, err := env.store.NetworkEntries.EnsurePreviewEntry(ctx, site.ID, b, 25480, 25490); err == nil {
		t.Fatal("deleted build acquired a preview reservation")
	}
	expect(client, buildCleanup, body(b, c), http.StatusNoContent)
	if artifact, err := env.store.Builds.Get(ctx, b); err != nil || artifact.ArtifactState != "deleted" {
		t.Fatalf("missing build history after deletion: %+v %v", artifact, err)
	}
	if _, err := os.Stat(filepath.Join(buildsRoot, b.String(), "artifacts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted build output still exists: %v", err)
	}
}
