package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentbind"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/traffic"
)

func TestWorkspaceTrafficOnlyFetchesBoundAuthorizedSiteAndDegradesIndependently(t *testing.T) {
	var requests atomic.Int32
	var unavailable atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/stats/cloudflare" || r.URL.RawQuery != "" {
			t.Error("overview changed the trusted metrics route")
		}
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-Data-Cache", "stale")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"days":[{"date":"2026-11-12","pageViews":999,"requests":2000,"uniques":444},{"date":"2026-11-13","pageViews":12,"requests":17,"uniques":5},{"date":"2026-11-11","pageViews":100,"requests":200,"uniques":50}],"totals":{"pageViews":1111,"requests":2217,"uniques":8888}}`))
	}))
	t.Cleanup(upstream.Close)
	world := newWorkspaceTestWorld(t)
	bound := world.createSite(t, "traffic-bound")
	other := world.createSite(t, "traffic-unbound")
	seedWorkspaceContent(t, world.local, bound.Site.ID, "bound", 2, 5, 2, []int{2024, 2026}, footprintsDoc())
	seedWorkspaceContent(t, world.local, other.Site.ID, "unbound", 1, 3, 1, []int{2025}, footprintsDoc())

	// Reuse the workspace's actual stores and credentials, with a separate
	// server whose immutable configuration binds exactly one existing site.
	bindings := &contentbind.Store{Store: world.local, Platform: world.library, Resolve: world.env.store.GetContentSources}
	server := httptest.NewServer(New(world.env.store, nil, Config{
		RPID:               "localhost",
		RPOrigins:          []string{world.env.base},
		Content:            bindings,
		PlatformContent:    world.library,
		TrafficStatsURL:    upstream.URL + "/api/stats/cloudflare",
		TrafficStatsSiteID: bound.Site.ID,
	}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(server.Close)
	configuredEnv := *world.env
	configuredEnv.base = server.URL
	client := configuredEnv.clientFor(world.client.token, world.client.csrf)
	path := "/api/v1/sites/" + bound.Site.ID.String() + "/overview"
	type overview struct {
		Counts    contentCounts           `json:"counts"`
		Analytics traffic.TrafficSnapshot `json:"analytics"`
	}

	headers, raw := workspaceRequest(t, client, http.MethodGet, path, nil, "", http.StatusOK)
	var result overview
	decodeJSONBody(t, raw, &result)
	expectedCounts := contentCounts{Posts: 2, Albums: 2, Photos: 5, Places: 2, Friends: 2}
	if result.Counts != expectedCounts || !result.Analytics.Configured || !result.Analytics.Available || result.Analytics.Cache != "stale" || result.Analytics.Provider != "cloudflare" {
		t.Fatalf("bound overview lost content or stale provider state: %+v", result)
	}
	if latest := result.Analytics.Latest; latest == nil || latest.Date != "2026-11-13" || latest.PageViews != 12 || latest.Requests != 17 || latest.Uniques != 5 {
		t.Fatal("overview used period totals or array order instead of the latest single day")
	}
	if headers.Get("Cache-Control") != "no-store" || requests.Load() != 1 {
		t.Fatal("overview changed its cache policy or requested metrics more than once")
	}

	_, raw = workspaceRequest(t, client, http.MethodGet, "/api/v1/sites/"+other.Site.ID.String()+"/overview", nil, "", http.StatusOK)
	decodeJSONBody(t, raw, &result)
	if result.Counts != (contentCounts{Posts: 1, Albums: 1, Photos: 3, Places: 2, Friends: 1}) || result.Analytics.Configured || result.Analytics.Available || result.Analytics.Latest != nil || requests.Load() != 1 {
		t.Fatal("another site's overview reused or fetched the bound site's metrics")
	}

	unavailable.Store(true)
	_, raw = workspaceRequest(t, client, http.MethodGet, path, nil, "", http.StatusOK)
	decodeJSONBody(t, raw, &result)
	if result.Counts != expectedCounts || !result.Analytics.Configured || result.Analytics.Available || result.Analytics.Latest != nil || requests.Load() != 2 {
		t.Fatal("unavailable metrics broke content counts or manufactured zero traffic")
	}

	workspaceRequest(t, configuredEnv.newClient(), http.MethodGet, path, nil, "", http.StatusUnauthorized)
	_, token, csrf := configuredEnv.createUserSession("traffic-nonmember")
	workspaceRequest(t, configuredEnv.clientFor(token, csrf), http.MethodGet, path, nil, "", http.StatusNotFound)
	if requests.Load() != 2 {
		t.Fatal("anonymous or nonmember requests reached the trusted metrics source")
	}
}
