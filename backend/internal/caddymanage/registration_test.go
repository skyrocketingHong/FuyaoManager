package caddymanage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

type registrationRunner struct {
	root       string
	hosts      []string
	active     []byte
	reloads    int
	failReload bool
}

func (r *registrationRunner) Adapt(_ context.Context, master string) ([]byte, error) {
	data, err := os.ReadFile(master)
	if err != nil {
		return nil, err
	}
	hide := []string{master}
	var expanded []byte
	for _, line := range strings.SplitAfter(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import \"") {
			path, err := strconv.Unquote(strings.TrimPrefix(trimmed, "import "))
			if err != nil {
				return nil, err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			hide = append(hide, path)
			expanded = append(expanded, body...)
		} else {
			expanded = append(expanded, line...)
		}
	}
	var routes []any
	for _, host := range r.hosts {
		routes = append(routes, map[string]any{"match": []any{map[string]any{"host": []string{host}}}, "handle": []any{map[string]any{"handler": "vars", "root": r.root}, map[string]any{"handler": "file_server", "hide": hide, "body_digest": Digest(expanded)}}})
	}
	return json.Marshal(map[string]any{"apps": map[string]any{"http": map[string]any{"servers": map[string]any{"srv0": map[string]any{"listen": []string{":443"}, "routes": routes}}}}})
}
func (*registrationRunner) Validate(context.Context, string) error   { return nil }
func (r *registrationRunner) Active(context.Context) ([]byte, error) { return r.active, nil }
func (r *registrationRunner) Reload(ctx context.Context, path string) error {
	r.reloads++
	if r.failReload {
		r.failReload = false
		return ErrUnavailable
	}
	data, err := r.Adapt(ctx, path)
	r.active = data
	return err
}

func TestCompleteBlockExtractionHonorsQuotesCommentsHeredocsAndBoundaries(t *testing.T) {
	source := []byte("{\nadmin unix//run/caddy/admin.sock\n}\n(shared) {\nheader X-Test `quoted } {`\n@assets path_regexp \"(?i)\\.(js|css)$\"\n}\n# keep master comment\none.example {\nroot * /srv/one\nrespond <<END\nbody with { and }\nEND\nfile_server\n}\n# second\ntwo.example, other.example {\nroot /srv/two\nhandle /api/* {\nreverse_proxy localhost:3000\n}\nfile_server\n}\n")
	blocks, err := extractBlocks(source)
	if err != nil || len(blocks) != 4 {
		t.Fatal("failed complete extraction", len(blocks), err)
	}
	one := source[blocks[2].start:blocks[2].end]
	if !bytes.HasPrefix(one, []byte("one.example {")) || !bytes.HasSuffix(one, []byte("}\n")) || !bytes.Contains(one, []byte("body with { and }")) || bytes.Contains(one, []byte("# second")) {
		t.Fatal("block range changed")
	}
	for _, malformed := range []string{"host {\nroot /x\n", "host {\nrespond <<END\nnever terminated\n}", "host {\nheader X \"unterminated\n}"} {
		if _, err := extractBlocks([]byte(malformed)); err == nil {
			t.Fatal("ambiguous block accepted")
		}
	}
}

func TestStaticRegistrationDryRunApplyAndMultipleSites(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(dir, "web")
	if err := os.Mkdir(web, 0700); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(dir, "Caddyfile")
	body := []byte("(shared) { header X-Shared \"kept\" }\none.example {\nroot * " + web + "\nimport shared\nfile_server\n}\ntwo.example {\nroot " + web + "\nfile_server\n}\n")
	if err := os.WriteFile(master, body, 0600); err != nil {
		t.Fatal(err)
	}
	runner := &registrationRunner{root: web, hosts: []string{"one.example", "two.example"}}
	runner.active, err = runner.Adapt(t.Context(), master)
	if err != nil {
		t.Fatal(err)
	}
	sites, err := caddyconfig.Parse(runner.active)
	if err != nil || len(sites) != 2 {
		t.Fatal("inventory fixture failed", err)
	}
	engine := NewEngine(filepath.Join(dir, "registry"), filepath.Join(dir, "state"), runner, os.Getegid())
	engine.ownerUID = uint32(os.Geteuid())
	options := OperatorRegistration{SiteKey: sites[0].ID, MasterFile: master, SourceRoot: filepath.Join(dir, "managed"), TemplateRoot: filepath.Join(dir, "templates")}
	result, err := engine.RegisterStatic(t.Context(), options)
	if err != nil || result.Registered || runner.reloads != 0 {
		t.Fatal("dry run changed live state", err)
	}
	actual, _ := os.ReadFile(master)
	if !bytes.Equal(actual, body) {
		t.Fatal("dry run changed source")
	}
	options.Apply = true
	runner.failReload = true
	if _, err := engine.RegisterStatic(t.Context(), options); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed registration reload accepted", err)
	}
	actual, _ = os.ReadFile(master)
	if !bytes.Equal(actual, body) {
		t.Fatal("registration failure changed original master")
	}
	if _, err := os.Stat(filepath.Join(options.SourceRoot, sites[0].ID+".caddy")); !os.IsNotExist(err) {
		t.Fatal("registration failure left source")
	}
	first, err := engine.RegisterStatic(t.Context(), options)
	if err != nil || !first.Registered {
		t.Fatal("first registration failed", err)
	}
	baseline, err := engine.Run(t.Context(), Request{Operation: "adopt", SiteKey: sites[0].ID, ExpectedDigest: first.SourceDigest})
	if err != nil {
		t.Fatal(err)
	}
	options.SiteKey = sites[1].ID
	second, err := engine.RegisterStatic(t.Context(), options)
	if err != nil || !second.Registered {
		t.Fatal("second registration failed", err)
	}
	inspected, err := engine.Run(t.Context(), Request{Operation: "inspect", SiteKey: sites[0].ID})
	if err != nil || inspected.Drifted || inspected.RuntimeUncertain {
		t.Fatal("second registration invalidated first site", err)
	}
	plan, err := engine.Run(t.Context(), Request{Operation: "validate", SiteKey: sites[0].ID, ExpectedDigest: first.SourceDigest, Settings: Settings{SecurityHeaders: "baseline"}})
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: sites[0].ID, ExpectedDigest: first.SourceDigest, ValidationID: plan.ValidationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Run(t.Context(), Request{Operation: "rollback", SiteKey: sites[0].ID, ExpectedDigest: deployed.SourceDigest, RevisionID: baseline.RootRevisionID}); err != nil {
		t.Fatal("unrelated registration broke rollback", err)
	}
	if _, err := engine.RegisterStatic(t.Context(), options); !errors.Is(err, ErrConflict) {
		t.Fatal("registered a site twice", err)
	}
}

func TestPublicationTemplatePreservesUnmappedRootsAndProxyRules(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("publication source cannot be root")
	}
	source := []byte("example.test {\nroot * /srv/old/current\nhandle /images/* {\nroot /srv/old/content/images\nfile_server\n}\nhandle /legacy/* {\nroot /srv/old/legacy\nfile_server\n}\nimport shared_amap\nreverse_proxy /api/* 127.0.0.1:3191\n}\n")
	pub := Publication{SiteID: "e55df730-947b-4fa7-b05a-2c29cf2f3149", ReleasesRoot: "/srv/manager/releases", BuildsRoot: "/srv/manager/builds", SourceUID: uint32(os.Geteuid()), AllowedThemes: []string{"fuyao-homepage"}}
	base, policy, err := AttachPublication(source, "/srv/old/current", pub, map[string]string{"images": "/srv/old/content/images"})
	if err != nil || len(policy.Paths) != 2 {
		t.Fatal(err)
	}
	preserved, err := Render(Registration{Publication: policy}, Settings{}, base)
	if err != nil || !bytes.Equal(preserved, source) {
		t.Fatal("preserve changed complex rules", err)
	}
	if !bytes.Contains(base, []byte("root /srv/old/legacy")) || !bytes.Contains(base, []byte("import shared_amap")) || !bytes.Contains(base, []byte("127.0.0.1:3191")) {
		t.Fatal("unmapped rules changed")
	}
}

func TestDirectImportRegistrationRetainsProxyAndScopesSettings(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(dir, "web")
	if err := os.Mkdir(web, 0700); err != nil {
		t.Fatal(err)
	}
	imported := filepath.Join(dir, "journal.caddy")
	proxy := "public.example {\nreverse_proxy 127.0.0.1:18082\n}\n"
	original := []byte(proxy + "one.example {\nroot * " + web + "\nfile_server\n}\n")
	if err := os.WriteFile(imported, original, 0600); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(master, []byte(QuotedImport(imported)), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &registrationRunner{root: web, hosts: []string{"one.example"}}
	runner.active, _ = runner.Adapt(t.Context(), master)
	sites, err := caddyconfig.Parse(runner.active)
	if err != nil || len(sites) != 1 {
		t.Fatal(err)
	}
	engine := NewEngine(filepath.Join(dir, "registry"), filepath.Join(dir, "state"), runner, os.Getegid())
	engine.ownerUID = uint32(os.Geteuid())
	result, err := engine.RegisterStatic(t.Context(), OperatorRegistration{SiteKey: sites[0].ID, MasterFile: master, SourceRoot: filepath.Join(dir, "managed"), TemplateRoot: filepath.Join(dir, "templates"), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := engine.loadPolicy(sites[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Publication != nil {
		t.Fatal("import registration granted publication")
	}
	if _, err := engine.Run(t.Context(), Request{Operation: "adopt", SiteKey: p.SiteKey, ExpectedDigest: result.SourceDigest}); err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Run(t.Context(), Request{Operation: "validate", SiteKey: p.SiteKey, ExpectedDigest: result.SourceDigest, Settings: Settings{SecurityHeaders: "baseline"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: p.SiteKey, ExpectedDigest: result.SourceDigest, ValidationID: plan.ValidationID}); err != nil {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(p.SourceFile)
	if !bytes.HasPrefix(actual, []byte(proxy)) || !bytes.Contains(actual, []byte("?X-Content-Type-Options")) {
		t.Fatal("proxy changed or static setting absent")
	}
	old, _ := os.ReadFile(imported)
	if !bytes.Equal(old, original) {
		t.Fatal("original imported source overwritten")
	}
}

func TestProtectedControlEntriesAndFakeImportsAreRejected(t *testing.T) {
	if !protectedEntry(caddyconfig.Site{Hosts: []string{"manager.fuyaoskyrocket.ing"}}) || !protectedEntry(caddyconfig.Site{Root: "/srv/fuyao-admin/static-page"}) {
		t.Fatal("control entry not protected")
	}
	file := "/etc/caddy/fuyao-managed/site.caddy"
	fake := []byte("example.test {\nrespond <<END\nimport " + strconv.Quote(file) + "\nEND\n}\n")
	if _, err := liveMasterTemplate(fake, file); !errors.Is(err, ErrConflict) {
		t.Fatal("heredoc import treated as active source", err)
	}
	actual := append(fake, []byte(QuotedImport(file))...)
	if _, err := liveMasterTemplate(actual, file); err != nil {
		t.Fatal("real top-level import not recognized", err)
	}
}

func TestHiddenPatternCanonicalizationPreservesEverySecurityRule(t *testing.T) {
	before := []byte(`{"handler":"file_server","hide":["/etc/caddy/Caddyfile","*.env","/etc/secret"]}`)
	candidate := []byte(`{"handler":"file_server","hide":["/etc/secret","/tmp/master","*.env","/tmp/site"]}`)
	a, err := canonicalConfigDigest(before, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalConfigDigest(candidate, map[string]string{"/tmp/master": "/etc/caddy/Caddyfile", "/tmp/site": "/etc/caddy/Caddyfile"})
	if err != nil || a != b {
		t.Fatal("equivalent hidden pattern set differs", err)
	}
	weakened := []byte(`{"handler":"file_server","hide":["/etc/caddy/Caddyfile","/etc/secret"]}`)
	c, err := canonicalConfigDigest(weakened, nil)
	if err != nil || a == c {
		t.Fatal("lost security pattern was ignored", err)
	}
	routesA, _ := canonicalConfigDigest([]byte(`{"routes":[{"terminal":true},{"terminal":false}]}`), nil)
	routesB, _ := canonicalConfigDigest([]byte(`{"routes":[{"terminal":false},{"terminal":true}]}`), nil)
	if routesA == routesB {
		t.Fatal("route ordering was normalized")
	}
}
