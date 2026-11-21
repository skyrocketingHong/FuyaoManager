package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themeimport"
)

func TestGitHubThemeInstallBuildPublish(t *testing.T) {
	if os.Getenv("FUYAO_TEST_GITHUB_THEME") != "1" {
		t.Skip("set FUYAO_TEST_GITHUB_THEME=1 to download and build the public theme")
	}
	env, _, _ := setupPublishWithContract(t, nil, func(cfg *Config, env *testEnv) {
		contracts := filepath.Join(repoRoot(t), "contracts", "theme")
		importer, err := themeimport.New(t.Context(), store.New(env.pool), cfg.ThemeContracts.(*themecontract.Set),
			contracts, t.TempDir(),
			`[{"id":"fuyao-homepage","url":"https://github.com/skyrocketingHong/FuyaoHomepage","branch":"main","path":"site"}]`, slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		cfg.ThemeImporter = importer
	})
	ctx := context.Background()
	owner, token, csrf := env.createUserSession("githubowner")
	_, otherToken, otherCSRF := env.createUserSession("githubmember")
	if _, err := env.pool.Exec(ctx, `UPDATE users SET platform_role='admin' WHERE id=$1`, owner.ID); err != nil {
		t.Fatal(err)
	}
	client := env.clientFor(token, csrf)
	member := env.clientFor(otherToken, otherCSRF)
	base := "/api/v1/themes/sources/fuyao-homepage"
	if status, _, _ := member.do(http.MethodGet, base+"/candidate", nil, nil); status != http.StatusForbidden {
		t.Fatalf("member check: HTTP %d", status)
	}
	status, _, raw := client.do(http.MethodGet, base+"/candidate", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("check: HTTP %d %s", status, raw)
	}
	var candidate themeimport.Candidate
	decodeJSONBody(t, raw, &candidate)
	t.Logf("GitHub candidate %s (%s)", candidate.Version, candidate.Commit)
	status, _, raw = client.do(http.MethodPost, base+"/install", map[string]string{"commit": candidate.Commit}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("install: HTTP %d %s", status, raw)
	}
	var task *themeimport.Task
	for deadline := time.Now().Add(6 * time.Minute); time.Now().Before(deadline); {
		status, _, raw = client.do(http.MethodGet, "/api/v1/themes/sources", nil, nil)
		if status != http.StatusOK {
			t.Fatalf("poll import: HTTP %d %s", status, raw)
		}
		var response struct{ Sources []themeimport.SourceStatus }
		decodeJSONBody(t, raw, &response)
		if len(response.Sources) != 1 {
			t.Fatalf("expected one configured theme source, got %d", len(response.Sources))
		}
		task = response.Sources[0].Task
		if task != nil && task.State != "installing" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if task == nil || task.State != "installed" || task.Commit != candidate.Commit {
		t.Fatalf("import did not complete: %+v", task)
	}
	status, _, raw = client.do(http.MethodGet, base+"/candidate", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("check installed: HTTP %d %s", status, raw)
	}
	decodeJSONBody(t, raw, &candidate)
	if candidate.State != "installed" {
		t.Fatalf("the catalog does not report the installed commit: %+v", candidate)
	}

	site := createSite(ctx, t, env, "github-theme", "GitHub Theme Acceptance", "", "en-US", owner.ID)
	selectThemeForSite(t, client, site.Site.ID, "fuyao-homepage", candidate.Version)
	build := createBuildAndWait(t, client, site.Site.ID)
	release := publishViaAPI(t, client, site.Site.ID, build.ID)
	page := fetchPublished(t, currentOriginURL(t, client, site.Site.ID), "")
	if release.Theme.Version != candidate.Version || !strings.Contains(page, "GitHub Theme Acceptance") {
		t.Fatalf("isolated published origin did not serve the imported theme version %s", candidate.Version)
	}
}
