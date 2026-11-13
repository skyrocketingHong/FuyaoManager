package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

type deferredPreviewTarget struct {
	buildrunner.PreviewTarget
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (target *deferredPreviewTarget) GetPreviewTarget(ctx context.Context, id uuid.UUID) (string, string, error) {
	target.once.Do(func() { close(target.entered) })
	select {
	case <-target.resume:
		return target.PreviewTarget.GetPreviewTarget(ctx, id)
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

// The real HTTP close may arrive after authorization but before the older
// link request has finished resolving its frozen target. That request must
// return preview_closed rather than creating another origin after the 204.
func TestPreviewCloseWinsOverPendingHTTPLink(t *testing.T) {
	root := t.TempDir()
	var previews *buildrunner.PreviewServer
	var target *deferredPreviewTarget
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatal(err)
		}
		target = &deferredPreviewTarget{PreviewTarget: store.New(e.pool).Builds, entered: make(chan struct{}), resume: make(chan struct{})}
		previews = buildrunner.NewPreviewServer(buildrunner.PreviewConfig{Target: target, BuildsRoot: root, TTL: time.Hour})
		e.mutateConfig = func(cfg *Config) { cfg.Previews = previews }
	})
	t.Cleanup(previews.Close)
	owner, token, csrf := env.createUserSession("pending-owner")
	site := createSite(t.Context(), t, env, "pending-site", "Pending", "", "zh-CN", owner.ID)
	themeRow, digest, themeID, version := fakeThemeFixture(t, env)
	build := fakeBuildInRoot(t, env, root, site.Site.ID, owner.ID, themeRow, themeID, version, digest, "pending")
	base := "/api/v1/sites/" + site.Site.ID.String() + "/builds/" + build.String()
	body := strings.NewReader("{}")
	request, err := http.NewRequest(http.MethodPost, env.base+base+"/preview-links", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", env.base)
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	type result struct {
		status int
		body   []byte
		err    error
	}
	issued := make(chan result, 1)
	go func() {
		response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
		if err != nil {
			issued <- result{err: err}
			return
		}
		defer response.Body.Close()
		raw, readErr := io.ReadAll(response.Body)
		issued <- result{response.StatusCode, raw, readErr}
	}()
	select {
	case <-target.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("link resolution was not reached")
	}
	client := env.clientFor(token, csrf)
	status, _, raw := client.do(http.MethodPost, base+"/preview-close", map[string]string{}, nil)
	close(target.resume)
	if status != http.StatusNoContent {
		t.Fatalf("close = %d: %s", status, raw)
	}
	response := <-issued
	if response.err != nil {
		t.Fatal(response.err)
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.body, &problem); err != nil {
		t.Fatal(err)
	}
	if response.status != http.StatusConflict || problem.Code != "preview_closed" {
		t.Fatalf("pending issue = %d %s", response.status, response.body)
	}
	if previews.IsActive(site.Site.ID, build) {
		t.Fatal("an old link resurrected its preview")
	}
	status, _, raw = client.do(http.MethodPost, base+"/preview-links", map[string]string{}, nil)
	if status != http.StatusCreated {
		t.Fatalf("fresh open = %d: %s", status, raw)
	}
	if !previews.IsActive(site.Site.ID, build) {
		t.Fatal("fresh open did not become active")
	}
}
