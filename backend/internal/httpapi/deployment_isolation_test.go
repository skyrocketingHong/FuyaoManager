package httpapi

// Run-isolation regression: the deployment switch defaults to off, and the
// isolated surface answers the deployment-mutating endpoints with an
// explicit deployment_disabled problem while every read — and the whole
// build/preview surface — keeps working. The enabled state exposes the
// real edge facts instead.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/edge"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// newIsolationEnv builds a world with an explicit deployment switch state
// and a real edge manager (no snippet directory: snippet management stays
// unconfigured; the switch only controls isolation and API gating).
func newIsolationEnv(t *testing.T, deploymentEnabled bool) *testEnv {
	t.Helper()
	var edgeMgr *edge.Manager
	return setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
		edgeMgr = edge.New(edge.Config{
			Pool:              e.pool,
			Entries:           store.New(e.pool).NetworkEntries,
			ReleasesRoot:      t.TempDir(),
			SitesRoot:         t.TempDir(),
			PortMin:           21040,
			PortMax:           21050,
			DeploymentEnabled: deploymentEnabled,
		})
		e.mutateConfig = func(cfg *Config) {
			cfg.Edge = edgeMgr
			cfg.DeploymentEnabled = deploymentEnabled
			cfg.PortMin = 21040
			cfg.PortMax = 21050
		}
	})
}

func decodeProblem(t *testing.T, raw []byte) (status int, code string) {
	t.Helper()
	var problem struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(raw, &problem); err != nil {
		t.Fatalf("decode problem %q: %v", raw, err)
	}
	return problem.Status, problem.Code
}

// TestRunIsolationBlocksDeploymentWrites proves the isolation default has
// zero deployment side effects: publish, rollback, visibility, and edge
// reapply answer 503 deployment_disabled before any pipeline logic runs,
// while reads stay available and the edge reports the true isolated state.
func TestRunIsolationBlocksDeploymentWrites(t *testing.T) {
	env := newIsolationEnv(t, false)
	ctx := context.Background()

	owner, token, csrf := env.createUserSession("isolated")
	site := createSite(ctx, t, env, "isolated-site", "Isolated Site", "", "zh-CN", owner.ID)
	// Provisioning reserves the platform entry; the port and URL exist while
	// nothing is deployed.
	if _, _, err := env.store.NetworkEntries.EnsureSiteEntry(ctx, site.Site.ID, 21040, 21050); err != nil {
		t.Fatalf("reserve site entry: %v", err)
	}
	client := env.clientFor(token, csrf)
	base := "/api/v1/sites/" + site.Site.ID.String()

	// Reads keep working: history and edge state answer normally.
	if status, _, _ := client.do("GET", base+"/releases", nil, nil); status != http.StatusOK {
		t.Fatalf("GET releases in isolation: status %d, want 200", status)
	}
	if status, _, _ := client.do("GET", base+"/edge", nil, nil); status != http.StatusOK {
		t.Fatalf("GET edge in isolation: status %d, want 200", status)
	}

	// Publish: blocked before any pipeline logic (not even theme contracts
	// are consulted).
	status, _, raw := client.do("POST", base+"/releases", map[string]string{}, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("publish in isolation: status %d, want 503", status)
	}
	if _, code := decodeProblem(t, raw); code != "deployment_disabled" {
		t.Fatalf("publish problem code = %q, want deployment_disabled", code)
	}

	// Rollback: blocked even for a nonexistent release — the switch gate
	// precedes the lookup.
	status, _, raw = client.do("POST", base+"/releases/00000000-0000-0000-0000-000000000000/rollback", map[string]string{}, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("rollback in isolation: status %d, want 503", status)
	}
	if _, code := decodeProblem(t, raw); code != "deployment_disabled" {
		t.Fatalf("rollback problem code = %q, want deployment_disabled", code)
	}

	// Visibility change: blocked; the stored value stays untouched.
	status, _, raw = client.do("PUT", base+"/visibility", map[string]string{"visibility": "public"}, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("visibility in isolation: status %d, want 503", status)
	}
	if _, code := decodeProblem(t, raw); code != "deployment_disabled" {
		t.Fatalf("visibility problem code = %q, want deployment_disabled", code)
	}
	after, err := env.store.Sites.GetForUser(ctx, site.Site.ID, owner.ID)
	if err != nil || after.Site.Visibility != "private" {
		t.Fatalf("visibility changed during isolation: err=%v visibility=%v", err, after.Site.Visibility)
	}

	// Edge reapply: blocked; the manager never ran an apply.
	status, _, raw = client.do("POST", base+"/edge/reapply", map[string]string{}, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("edge reapply in isolation: status %d, want 503", status)
	}
	if _, code := decodeProblem(t, raw); code != "deployment_disabled" {
		t.Fatalf("edge reapply problem code = %q, want deployment_disabled", code)
	}

	// The edge status reports the real isolated state: snippet management
	// unconfigured (enabled=false), run isolation on, and currentOk false —
	// the reserved port and rendered URL are not a running origin.
	var state struct {
		Enabled  bool `json:"enabled"`
		Isolated bool `json:"isolated"`
		Site     struct {
			Port      int    `json:"port"`
			OriginURL string `json:"originUrl"`
			CurrentOK bool   `json:"currentOk"`
		} `json:"site"`
	}
	status, _, raw = client.do("GET", base+"/edge", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET edge: status %d", status)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode edge state: %v", err)
	}
	if state.Enabled || !state.Isolated {
		t.Fatalf("edge facts: enabled=%v isolated=%v, want enabled=false isolated=true", state.Enabled, state.Isolated)
	}
	if state.Site.CurrentOK {
		t.Fatal("currentOk must stay false while isolated")
	}
	if state.Site.OriginURL == "" || state.Site.Port == 0 {
		t.Fatal("the reservation (port and URL) is still reported for provisioning")
	}
}

// TestDeploymentEnabledEdgeState proves the enabled surface reports the real
// edge facts: isolation off, snippet management still unconfigured until the
// operator sets a snippet directory, and currentOk false because no release
// is live yet — the port/URL alone never count as running.
func TestDeploymentEnabledEdgeState(t *testing.T) {
	env := newIsolationEnv(t, true)
	ctx := context.Background()

	owner, token, csrf := env.createUserSession("deployed")
	site := createSite(ctx, t, env, "deployed-site", "Deployed Site", "", "zh-CN", owner.ID)
	if _, _, err := env.store.NetworkEntries.EnsureSiteEntry(ctx, site.Site.ID, 21040, 21050); err != nil {
		t.Fatalf("reserve site entry: %v", err)
	}
	client := env.clientFor(token, csrf)

	// Publishing is not blocked by the switch (the pipeline itself may still
	// answer its own errors; here there is no build, so the response is not
	// deployment_disabled).
	status, _, raw := client.do("POST", "/api/v1/sites/"+site.Site.ID.String()+"/releases", map[string]string{}, nil)
	if status == http.StatusServiceUnavailable {
		var problem struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(raw, &problem); err == nil && problem.Code == "deployment_disabled" {
			t.Fatal("enabled deployment must not answer deployment_disabled")
		}
	}

	status, _, raw = client.do("GET", "/api/v1/sites/"+site.Site.ID.String()+"/edge", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET edge: status %d", status)
	}
	var state struct {
		Enabled  bool `json:"enabled"`
		Isolated bool `json:"isolated"`
		Site     struct {
			Port      int    `json:"port"`
			OriginURL string `json:"originUrl"`
			CurrentOK bool   `json:"currentOk"`
		} `json:"site"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode edge state: %v", err)
	}
	if state.Isolated {
		t.Fatal("enabled deployment must not report isolated")
	}
	if state.Enabled {
		t.Fatal("snippet management stays unconfigured without a snippet directory")
	}
	if state.Site.CurrentOK {
		t.Fatal("currentOk must stay false without a live release")
	}
	if state.Site.OriginURL == "" || state.Site.Port == 0 {
		t.Fatal("provisioning must have reserved the entry")
	}
}
