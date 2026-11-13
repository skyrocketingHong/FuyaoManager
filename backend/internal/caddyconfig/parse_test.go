package caddyconfig

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func config(t *testing.T, server string, listen []string, routes string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"apps": map[string]any{"http": map[string]any{"servers": map[string]any{server: map[string]any{"listen": listen, "routes": json.RawMessage(routes)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func one(t *testing.T, data []byte) Site {
	t.Helper()
	sites, err := Parse(data)
	if err != nil || len(sites) != 1 {
		t.Fatalf("want one site: count=%d error=%v", len(sites), err)
	}
	return sites[0]
}

func hostRoute(host, root string) string {
	data, _ := json.Marshal(map[string]any{"match": []any{map[string]any{"host": []string{host}}}, "handle": []any{map[string]any{"handler": "vars", "root": root}, map[string]any{"handler": "file_server"}}, "terminal": true})
	return string(data)
}

func TestSimpleStaticAndAliasNormalization(t *testing.T) {
	site := one(t, config(t, "srv0", []string{":443"}, `[{"match":[{"host":["WWW.EXAMPLE.TEST","example.test"]}],"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"vars","root":"/srv/example/current"},{"handler":"file_server","root":"{http.vars.root}"}]}]}],"terminal":true}]`))
	if site.Kind != KindStatic || site.ReadOnly || site.Root != "/srv/example/current" || site.RootHasPlaceholder {
		t.Fatalf("wrong static classification: %+v", site)
	}
	if !reflect.DeepEqual(site.Hosts, []string{"example.test", "www.example.test"}) || len(site.Digest) != 64 || !strings.HasPrefix(site.ID, "caddy-") {
		t.Fatalf("wrong identity metadata: %+v", site)
	}
	if site.RoutePath != "/apps/http/servers/srv0/routes/0" {
		t.Fatalf("wrong locator: %s", site.RoutePath)
	}
}

func TestIdentitySurvivesServerRenumberAndUnrelatedRouteRemoval(t *testing.T) {
	before, err := Parse(config(t, "srv4", []string{"127.0.0.1:443", "[::1]:443"}, `[`+hostRoute("longer.example.test", "/srv/unrelated")+`,`+hostRoute("target.example.test", "/srv/target")+`]`))
	if err != nil {
		t.Fatal(err)
	}
	var old Site
	for _, site := range before {
		if site.Hosts[0] == "target.example.test" {
			old = site
		}
	}
	after := one(t, config(t, "srv0", []string{"[::1]:443", "127.0.0.1:443"}, `[`+hostRoute("target.example.test", "/srv/target")+`]`))
	if old.ID != after.ID || old.RoutePath == after.RoutePath {
		t.Fatalf("locator leaked into stable identity: %s / %s", old.ID, after.ID)
	}
	changed := one(t, config(t, "renumbered", after.Listen, `[`+hostRoute("target.example.test", "/srv/new-root")+`]`))
	if changed.ID != after.ID || changed.Digest == after.Digest {
		t.Fatal("identity must stay stable while config digest detects root changes")
	}
}

func TestIdentityDistinguishesDifferentListenAndMatchers(t *testing.T) {
	base := one(t, config(t, "srv0", []string{"127.0.0.1:443"}, `[`+hostRoute("example.test", "/srv/example")+`]`))
	other := one(t, config(t, "srv0", []string{"192.0.2.20:443"}, `[`+hostRoute("example.test", "/srv/example")+`]`))
	path := one(t, config(t, "srv0", base.Listen, `[{"match":[{"host":["example.test"],"path":["/only/*"]}],"handle":[{"handler":"file_server","root":"/srv/example"}]}]`))
	if base.ID == other.ID || base.ID == path.ID || !path.ReadOnly {
		t.Fatal("network or conditional matcher boundary was lost")
	}
}

func TestDigestIgnoresWhitespaceAndObjectKeyOrder(t *testing.T) {
	first := []byte(`{"apps":{"http":{"servers":{"a":{"listen":[":80"],"routes":[{"handle":[{"handler":"file_server","root":"/srv/a"}]}]}}}}}`)
	second := []byte("{\n\"apps\":{\"http\":{\"servers\":{\"a\":{\"routes\":[{\"handle\":[{\"root\":\"/srv/a\",\"handler\":\"file_server\"}]}],\"listen\":[\":80\"]}}}}}")
	a, b := one(t, first), one(t, second)
	if a.ID != b.ID || a.Digest != b.Digest {
		t.Fatal("formatting changed canonical identity/digest")
	}
}

func TestHostlessRoutesStayOneSiteAndKeepPrimaryRoot(t *testing.T) {
	site := one(t, config(t, "srv0", []string{":8080"}, `[
	 {"handle":[{"handler":"vars","root":"/srv/main"}]},
	 {"match":[{"path":["/assets/*"]}],"handle":[{"handler":"vars","root":"/srv/assets"},{"handler":"file_server"}]},
	 {"handle":[{"handler":"file_server"}]}
	]`))
	if site.Kind != KindStatic || !site.ReadOnly || site.Root != "/srv/main" || len(site.Hosts) != 0 {
		t.Fatalf("hostless site lost its boundaries/root: %+v", site)
	}
}

func TestLaterVariablesDoNotRetroactivelyChangeEarlierFileServer(t *testing.T) {
	site := one(t, config(t, "srv0", []string{":8080"}, `[{"handle":[{"handler":"file_server"}]},{"handle":[{"handler":"vars","root":"/srv/later"}]}]`))
	if !site.ReadOnly || site.Kind != KindStatic {
		t.Fatal("multiple sequential routes marked writable")
	}
	// A later variable does not establish the root of an earlier response.
	if site.Root == "/srv/later" {
		t.Fatal("later variable was applied retroactively")
	}
}

func TestRootAssignmentsFollowExecutionOrderAndExplicitHandlerOverride(t *testing.T) {
	for _, routes := range []string{
		`[{"handle":[{"handler":"vars","root":"/srv/first"},{"handler":"vars","root":"/srv/last"},{"handler":"file_server"}]}]`,
		`[{"handle":[{"handler":"vars","root":"/srv/first"},{"handler":"file_server","root":"/srv/last"}]}]`,
		`[{"handle":[{"handler":"vars","root":"/srv/unused"}]},{"match":[{"path":["/only/*"]}],"handle":[{"handler":"file_server","root":"/srv/last"}]}]`,
	} {
		site := one(t, config(t, "a", []string{":443"}, routes))
		if site.Root != "/srv/last" {
			t.Fatalf("wrong effective root: %+v", site)
		}
	}
	unknown := one(t, config(t, "a", []string{":443"}, `[{"handle":[{"handler":"vars","root":"/srv/first"},{"handler":"vars","root":42},{"handler":"file_server"}]}]`))
	if !unknown.ReadOnly || unknown.Root != "" {
		t.Fatal("unknown variable reused an earlier root")
	}
	badHandler := one(t, config(t, "a", []string{":443"}, `[{"handle":[{"handler":"vars","root":"/srv/old"},{"handler":"file_server","root":42}]}]`))
	if !badHandler.ReadOnly || badHandler.Root != "" {
		t.Fatal("malformed handler root fell back to an earlier variable")
	}
}

func TestGlobalPreludeDoesNotCreateAnExtraCatchall(t *testing.T) {
	site := one(t, config(t, "srv0", []string{":8080"}, `[{"handle":[{"handler":"vars","root":"/srv/default"}]},{"match":[{"host":["example.test"]}],"handle":[{"handler":"file_server"}]}]`))
	if site.Root != "/srv/default" || !site.ReadOnly || site.Hosts[0] != "example.test" {
		t.Fatalf("global route scope lost: %+v", site)
	}
}

func TestNestedHostDiscoveryAndInheritedScope(t *testing.T) {
	direct := one(t, config(t, "a", []string{":443"}, `[`+hostRoute("example.test", "/srv/example")+`]`))
	nested := one(t, config(t, "b", direct.Listen, `[{"handle":[{"handler":"subroute","routes":[`+hostRoute("example.test", "/srv/example")+`]}]}]`))
	if nested.ID != direct.ID || nested.ReadOnly {
		t.Fatal("transparent subroute changed identity or syntax support")
	}
	conditional := one(t, config(t, "c", direct.Listen, `[{"match":[{"path":["/protected/*"]}],"handle":[{"handler":"subroute","routes":[`+hostRoute("example.test", "/srv/example")+`]}]}]`))
	if !conditional.ReadOnly || conditional.ID == direct.ID {
		t.Fatal("inherited matcher was lost")
	}
}

func TestRedirectStatusesAndRuntimeURI(t *testing.T) {
	for _, status := range []string{`301`, `"308"`} {
		site := one(t, config(t, "a", []string{":443"}, `[{"match":[{"host":["old.example.test"]}],"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"static_response","headers":{"Location":["https://new.example.test/path"]},"status_code":`+status+`}]}]}],"terminal":true}]`))
		if site.Kind != KindRedirect || site.Redirect == nil || site.Redirect.Location != "https://new.example.test/path" || site.ReadOnly {
			t.Fatalf("redirect not recognized: %+v", site)
		}
	}
	site := one(t, config(t, "a", []string{":443"}, `[{"handle":[{"handler":"static_response","headers":{"Location":["https://new.example.test{http.request.uri}"]},"status_code":308}]}]`))
	if site.Kind != KindRedirect || !site.ReadOnly || site.Redirect == nil || site.Redirect.Location != "https://new.example.test{http.request.uri}" || site.Redirect.TargetRedacted {
		t.Fatal("safe URI placeholder was not retained")
	}
}

func TestNestedHostDoesNotHideTheServerFallback(t *testing.T) {
	sites, err := Parse(config(t, "a", []string{":443"}, `[{"handle":[{"handler":"subroute","routes":[`+hostRoute("example.test", "/srv/host")+`]}]},{"handle":[{"handler":"file_server","root":"/srv/default"}]}]`))
	if err != nil || len(sites) != 2 {
		t.Fatalf("nested hosts hid default site: count=%d error=%v", len(sites), err)
	}
	host, fallback := false, false
	for _, site := range sites {
		if !site.ReadOnly {
			t.Fatal("shared routes marked writable")
		}
		if len(site.Hosts) == 0 {
			fallback = site.Root == "/srv/default"
		} else {
			host = site.Root == "/srv/host"
		}
	}
	if !host || !fallback {
		t.Fatal("host or fallback metadata lost")
	}
}

func TestPrivateRedirectValuesAndUnknownHandlersNeverEscape(t *testing.T) {
	for _, location := range []string{"https://user:SECRET@example.test/path", "https://example.test/path?token=SECRET", "https://example.test/path#SECRET", "https://example.test/{env.SECRET}"} {
		routes, _ := json.Marshal([]any{map[string]any{"handle": []any{map[string]any{"handler": "static_response", "status_code": 302, "headers": map[string][]string{"Location": {location}, "Authorization": {"Bearer SECRET"}}}}}})
		site := one(t, config(t, "a", []string{":443"}, string(routes)))
		encoded, _ := json.Marshal(site)
		if !site.ReadOnly || site.Redirect == nil || !site.Redirect.TargetRedacted || strings.Contains(string(encoded), "SECRET") {
			t.Fatalf("private redirect information escaped: %s", encoded)
		}
	}
	site := one(t, config(t, "a", []string{":443"}, `[{"handle":[{"handler":"authentication","providers":{"http_basic":{"accounts":[{"username":"private-user","password":"SECRET"}]}}},{"handler":"file_server","root":"/srv/static"}]}]`))
	encoded, _ := json.Marshal(site)
	if site.Kind != KindStatic || !site.ReadOnly || strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "private-user") {
		t.Fatal("security handler was ignored or leaked")
	}
}

func TestProxyAndMixedSitesRemainReadOnlyWithoutRawConfig(t *testing.T) {
	proxy := one(t, config(t, "a", []string{":443"}, `[{"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"private.internal:8080"}],"headers":{"request":{"set":{"Authorization":["Bearer SECRET"]}}}}]}]`))
	encoded, _ := json.Marshal(proxy)
	if proxy.Kind != KindReverseProxy || !proxy.ReadOnly || strings.Contains(string(encoded), "private.internal") || strings.Contains(string(encoded), "SECRET") {
		t.Fatal("proxy credentials or upstream escaped")
	}
	mixed := one(t, config(t, "a", []string{":443"}, `[{"handle":[{"handler":"vars","root":"/srv/main"},{"handler":"subroute","routes":[{"match":[{"path":["/api/*"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"127.0.0.1:9000"}]}]},{"handle":[{"handler":"file_server"}]}]}]}]`))
	if mixed.Kind != KindStatic || !mixed.ReadOnly || mixed.Root != "/srv/main" {
		t.Fatalf("mixed static/proxy site was flattened incorrectly: %+v", mixed)
	}
}

func TestUnsafeAndAmbiguousRootsAreReadOnly(t *testing.T) {
	for _, root := range []string{"{env.WEB_ROOT}", "relative-root", ""} {
		routes, _ := json.Marshal([]any{map[string]any{"handle": []any{map[string]any{"handler": "file_server", "root": root}}}})
		site := one(t, config(t, "a", []string{":443"}, string(routes)))
		if site.Kind != KindStatic || !site.ReadOnly {
			t.Fatalf("unsafe root accepted: %+v", site)
		}
		if strings.Contains(root, "{") && !site.RootHasPlaceholder {
			t.Fatal("root placeholder flag missing")
		}
	}
	site := one(t, config(t, "a", []string{":443"}, `[{"handle":[{"handler":"file_server","root":"/srv/static","hide":["/etc/caddy/Caddyfile"]}]}]`))
	if !site.ReadOnly || site.Kind != KindStatic {
		t.Fatal("custom static policy marked writable")
	}
}

func TestDuplicateMatchingBoundariesGetStableReadOnlyDisambiguation(t *testing.T) {
	a, b := hostRoute("example.test", "/srv/a"), hostRoute("example.test", "/srv/b")
	first, err := Parse(config(t, "srv0", []string{":443"}, `[`+a+`,`+b+`]`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse(config(t, "srv8", []string{":443"}, `[`+b+`,`+a+`]`))
	if err != nil {
		t.Fatal(err)
	}
	ids := func(sites []Site) []string {
		out := []string{}
		for _, site := range sites {
			if !site.ReadOnly || site.DuplicateCount != 2 {
				t.Fatal("duplicate candidate marked writable")
			}
			out = append(out, site.ID)
		}
		sort.Strings(out)
		return out
	}
	if !reflect.DeepEqual(ids(first), ids(second)) || first[0].ID == first[1].ID {
		t.Fatal("duplicate boundaries were not disambiguated stably")
	}
}

func TestNestedAndOuterCatchallShareAnAmbiguousBoundary(t *testing.T) {
	sites, err := Parse(config(t, "a", []string{":443"}, `[{"handle":[{"handler":"subroute","routes":[`+hostRoute("example.test", "/srv/host")+`,{"handle":[{"handler":"file_server","root":"/srv/inner"}]}]}]},{"handle":[{"handler":"file_server","root":"/srv/outer"}]}]`))
	if err != nil || len(sites) != 3 {
		t.Fatalf("catchall boundaries: count=%d error=%v", len(sites), err)
	}
	for _, site := range sites {
		if len(site.Hosts) == 0 && (!site.ReadOnly || site.DuplicateCount != 2) {
			t.Fatal("equivalent empty match scope was not disambiguated")
		}
	}
}

func TestInputBoundsAndInvalidJSON(t *testing.T) {
	for _, data := range [][]byte{nil, []byte(`null`), []byte(`[]`), []byte(`{"error":"SECRET"}`), []byte(`{"apps":[]} `), []byte(`{"apps":{},"apps":{}}`), []byte(`{} {}`), []byte(`{} trailing`), {'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'}} {
		_, err := Parse(data)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid input accepted: %v", err)
		}
		if strings.Contains(err.Error(), "SECRET") {
			t.Fatal("error leaked input")
		}
	}
	if _, err := Parse(bytesOf(' ', MaxConfigBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatal("oversized config accepted")
	}
	deep := `{"logging":` + strings.Repeat(`[`, maxDepth+1) + `0` + strings.Repeat(`]`, maxDepth+1) + `}`
	if _, err := Parse([]byte(deep)); !errors.Is(err, ErrComplexity) {
		t.Fatal("deep JSON accepted")
	}
	wide := `{"logging":[` + strings.Repeat(`0,`, maxTokens) + `0]}`
	if _, err := Parse([]byte(wide)); !errors.Is(err, ErrComplexity) {
		t.Fatal("wide JSON accepted")
	}
	for _, data := range []string{`{}`, `{"apps":{}}`, `{"admin":{"disabled":true}}`, `{"apps":{"http":{"servers":{}}}}`} {
		sites, err := Parse([]byte(data))
		if err != nil || sites == nil || len(sites) != 0 {
			t.Fatal("valid empty config rejected")
		}
	}
}

func bytesOf(value byte, count int) []byte {
	out := make([]byte, count)
	for index := range out {
		out[index] = value
	}
	return out
}

func TestOperatorAdaptedSnapshot(t *testing.T) {
	filename := os.Getenv("FUYAO_TEST_CADDY_JSON")
	if filename == "" {
		t.Skip("FUYAO_TEST_CADDY_JSON unset: private operator snapshot verification is optional")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal("cannot read the operator snapshot")
	}
	sites, err := Parse(data)
	if err != nil || len(sites) == 0 {
		t.Fatalf("operator snapshot inventory: count=%d error=%v", len(sites), err)
	}
	counts := map[Kind]int{}
	readonly := 0
	ids := map[string]bool{}
	for _, site := range sites {
		counts[site.Kind]++
		if site.ReadOnly {
			readonly++
		}
		if site.ID == "" || site.Digest == "" || ids[site.ID] {
			t.Fatal("operator inventory has a missing or duplicate identity")
		}
		ids[site.ID] = true
	}
	t.Logf("sites=%d static=%d redirect=%d reverse_proxy=%d unsupported=%d readonly=%d", len(sites), counts[KindStatic], counts[KindRedirect], counts[KindReverseProxy], counts[KindUnsupported], readonly)
	if output := os.Getenv("FUYAO_TEST_CADDY_OUTPUT"); output != "" {
		encoded, err := json.MarshalIndent(sites, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(encoded, '\n'), 0600); err != nil {
			t.Fatal("cannot write the operator inventory")
		}
	}
}
