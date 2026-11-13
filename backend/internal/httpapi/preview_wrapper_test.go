package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func TestPreviewWrapperHostLeaseReloadAndClose(t *testing.T) {
	root := t.TempDir()
	var previews *buildrunner.PreviewServer
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatal(err)
		}
		previews = buildrunner.NewPreviewServer(buildrunner.PreviewConfig{Target: store.New(e.pool).Builds, BuildsRoot: root, TTL: time.Minute, ManagerOrigin: "https://manager.example.test", ExternalBase: "https://preview.manager.example.test"})
		e.mutateConfig = func(cfg *Config) { cfg.Previews = previews }
	})
	t.Cleanup(previews.Close)
	owner, token, csrf := env.createUserSession("wrapper-owner")
	site := createSite(t.Context(), t, env, "formal-site", "Preview", "", "zh-CN", owner.ID)
	themeID, digest, name, version := fakeThemeFixture(t, env)
	build := fakeBuildInRoot(t, env, root, site.Site.ID, owner.ID, themeID, name, version, digest, "wrapper")
	path := "/api/v1/sites/" + site.Site.ID.String() + "/builds/" + build.String()
	client := env.clientFor(token, csrf)
	status, _, body := client.do("POST", path+"/preview-links", map[string]string{}, nil)
	if status != 201 {
		t.Fatalf("issue=%d %s", status, body)
	}
	var link struct {
		URL string `json:"url"`
	}
	decodeJSONBody(t, body, &link)
	parsed, err := url.Parse(link.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "preview.manager.example.test" || parsed.Path != "/formal-site/" {
		t.Fatal(link.URL)
	}
	get := func(host, route string) (int, http.Header, string) {
		t.Helper()
		request, err := http.NewRequest("GET", env.base+route, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Host = host
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, response.Header, string(raw)
	}
	// The wrapper origin has no manager session cookie. Its short bearer
	// lease was authorized only by the management API above.
	status, headers, page := get(parsed.Host, parsed.RequestURI())
	if status != 200 || !strings.Contains(page, `sandbox="allow-scripts allow-same-origin"`) || !strings.Contains(headers.Get("Content-Security-Policy"), "frame-src https://preview.manager.example.test:") {
		t.Fatalf("wrapper=%d %s", status, page)
	}
	if !strings.Contains(page, "event.source!==frame.contentWindow") || !strings.Contains(page, "event.origin!==origin") || !strings.Contains(page, "event.data?.instance!==instance") {
		t.Fatal("route bridge lacks source/origin/instance checks")
	}
	status, _, _ = get(parsed.Host, "/formal-site/blog?lease="+parsed.Query().Get("lease"))
	if status != 200 {
		t.Fatalf("active lease reload=%d", status)
	}
	status, _, _ = get(parsed.Host, "/formal-site/?__fuyao_status=1&lease="+parsed.Query().Get("lease"))
	if status != 204 {
		t.Fatalf("live status=%d", status)
	}
	status, _, _ = get("manager.example.test", parsed.RequestURI())
	if status != 404 {
		t.Fatalf("manager origin served preview=%d", status)
	}
	status, _, _ = get(parsed.Host, "/api/v1/session")
	if status != 404 {
		t.Fatalf("preview origin exposed management API=%d", status)
	}
	status, _, _ = get(parsed.Host, "/another-site/?lease="+parsed.Query().Get("lease"))
	if status != 410 {
		t.Fatalf("lease crossed host=%d", status)
	}
	status, _, _ = client.do("POST", path+"/preview-close", map[string]string{}, nil)
	if status != 204 {
		t.Fatal(status)
	}
	status, _, _ = get(parsed.Host, parsed.RequestURI())
	if status != 410 {
		t.Fatalf("closed wrapper reopened=%d", status)
	}
	if previews.IsActive(site.Site.ID, build) {
		t.Fatal("wrapper reload created an origin")
	}
}

func TestSecureManagerCookiesRejectLegacyAndAmbiguity(t *testing.T) {
	env := setupApply(t, func(ctx context.Context, e *testEnv) {
		if _, err := db.ApplyMigrations(ctx, e.pool); err != nil {
			t.Fatal(err)
		}
		e.mutateConfig = func(cfg *Config) { cfg.CookieSecure = true }
	})
	user, token, _ := env.createUserSession("secure-cookie")
	get := func(cookies ...*http.Cookie) bool {
		request, _ := http.NewRequest("GET", env.base+"/api/v1/session", nil)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body struct {
			Authenticated bool `json:"authenticated"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.Authenticated
	}
	if get(&http.Cookie{Name: sessionCookieName, Value: token}) {
		t.Fatal("HTTPS accepted legacy cookie")
	}
	if !get(&http.Cookie{Name: secureSessionCookieName, Value: token}) {
		t.Fatal("HTTPS rejected its host-only cookie")
	}
	if get(&http.Cookie{Name: secureSessionCookieName, Value: token}, &http.Cookie{Name: secureSessionCookieName, Value: token}) {
		t.Fatal("ambiguous session cookies accepted")
	}
	api := &API{cfg: Config{CookieSecure: true, ChallengeTTL: time.Minute}}
	for _, set := range []func(http.ResponseWriter){func(w http.ResponseWriter) { api.setSessionCookie(w, "token", time.Minute) }, func(w http.ResponseWriter) { api.setAuthTxCookie(w, user.ID) }} {
		response := httptest.NewRecorder()
		set(response)
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || !strings.HasPrefix(cookies[0].Name, "__Host-") || cookies[0].Path != "/" || cookies[0].Domain != "" || !cookies[0].Secure || !cookies[0].HttpOnly {
			t.Fatal("invalid __Host cookie attributes")
		}
	}
}

func TestWithUIDelegatesOnlyConfiguredPreviewHostname(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("manager-spa"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := WithUI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(410)
		_, _ = w.Write([]byte("closed-lease"))
	}), dir, "preview.manager.example.test")
	preview := httptest.NewRecorder()
	handler.ServeHTTP(preview, httptest.NewRequest("GET", "https://preview.manager.example.test/formal/blog?lease=x", nil))
	if preview.Code != 410 || preview.Body.String() != "closed-lease" {
		t.Fatal("preview became SPA fallback")
	}
	manager := httptest.NewRecorder()
	handler.ServeHTTP(manager, httptest.NewRequest("GET", "https://manager.example.test/sites", nil))
	if manager.Code != 200 || manager.Body.String() != "manager-spa" {
		t.Fatal("manager SPA changed")
	}
}
