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
)

type fakeRunner struct {
	active       []byte
	extra        string
	failValidate bool
	failReload   bool
	ignoreReload bool
	reloads      int
	afterReload  func()
}

func (r *fakeRunner) Adapt(_ context.Context, master string) ([]byte, error) {
	data, err := os.ReadFile(master)
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(strings.Split(string(data), "\n")[1])
	source, err := strconv.Unquote(strings.TrimPrefix(line, "import "))
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"sharedSnippet": "unchanged-amap", "otherSite": r.extra, "apps": map[string]any{"http": map[string]any{"servers": map[string]any{"main": map[string]any{"routes": []any{map[string]any{"handle": []any{map[string]any{"handler": "vars", "site_body": string(body)}, map[string]any{"handler": "file_server", "hide": []string{master, source}}}}}}}}}})
}
func (r *fakeRunner) Validate(_ context.Context, _ string) error {
	if r.failValidate {
		return ErrValidation
	}
	return nil
}
func (r *fakeRunner) Reload(ctx context.Context, path string) error {
	r.reloads++
	if r.failReload {
		r.failReload = false
		return ErrUnavailable
	}
	if !r.ignoreReload {
		data, err := r.Adapt(ctx, path)
		if err != nil {
			return err
		}
		r.active = data
	}
	if r.afterReload != nil {
		r.afterReload()
	}
	return nil
}
func (r *fakeRunner) Active(context.Context) ([]byte, error) { return r.active, nil }

type fixture struct {
	engine *Engine
	reg    Registration
	runner *fakeRunner
	source []byte
}

func setupEngine(t *testing.T) fixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(dir, "registry")
	templates := filepath.Join(dir, "templates")
	web := filepath.Join(dir, "web")
	for _, path := range []string{registry, templates, web} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	reg := Registration{SiteKey: "registered-site", Kind: "static", StaticRoot: web, MasterFile: filepath.Join(dir, "Caddyfile"), SourceFile: filepath.Join(dir, "site.caddy"), BaseTemplate: filepath.Join(templates, "site.caddy"), MasterTemplate: filepath.Join(templates, "master.caddy"), Capabilities: Capabilities{Compression: true, SecurityHeaders: true}, Preserve: Preserve{Compression: "encode gzip\n", SecurityHeaders: "header Content-Security-Policy \"default-src 'self'; report-uri /SENSITIVE\"\n"}}
	base := []byte("example.test {\n__FUYAO_COMPRESSION____FUYAO_SECURITY_HEADERS__root /unchanged\nimport fuyao_amap_proxy\n}\n")
	source, err := Render(reg, Settings{}, base)
	if err != nil {
		t.Fatal(err)
	}
	reg.InitialDigest = Digest(source)
	masterTemplate := []byte("(fuyao_amap_proxy) { unchanged-snippet }\nimport __FUYAO_SITE_SOURCE__\n")
	master, err := RenderMaster(masterTemplate, reg.SourceFile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(reg)
	for path, data := range map[string][]byte{reg.SourceFile: source, reg.MasterFile: master, reg.BaseTemplate: base, reg.MasterTemplate: masterTemplate, filepath.Join(registry, reg.SiteKey+".json"): encoded} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runner := &fakeRunner{}
	runner.active, err = runner.Adapt(t.Context(), reg.MasterFile)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(registry, filepath.Join(dir, "state"), runner, os.Getegid())
	engine.ownerUID = uint32(os.Geteuid())
	return fixture{engine, reg, runner, source}
}
func adopt(t *testing.T, f fixture) Result {
	t.Helper()
	result, err := f.engine.Run(t.Context(), Request{Operation: "adopt", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Adopted || !revisionPattern.MatchString(result.RootRevisionID) {
		t.Fatal("no sealed baseline revision")
	}
	return result
}
func validate(t *testing.T, f fixture, settings Settings) Result {
	t.Helper()
	result, err := f.engine.Run(t.Context(), Request{Operation: "validate", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAdoptPreservesComplexSourceAndSecretsStayPrivileged(t *testing.T) {
	f := setupEngine(t)
	result := adopt(t, f)
	actual, _ := os.ReadFile(f.reg.SourceFile)
	if string(actual) != string(f.source) || f.runner.reloads != 0 {
		t.Fatal("adoption changed live configuration")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "SENSITIVE") || strings.Contains(string(encoded), f.reg.SourceFile) {
		t.Fatal("privileged source escaped result")
	}
	if _, err := f.engine.Run(t.Context(), Request{Operation: "inspect", SiteKey: "unregistered"}); !errors.Is(err, ErrNotRegistered) {
		t.Fatal(err)
	}
}
func TestValidateDeployAndRollbackUseSealedPlanAndFullMaster(t *testing.T) {
	f := setupEngine(t)
	baseline := adopt(t, f)
	plan := validate(t, f, Settings{Compression: "zstd", SecurityHeaders: "baseline"})
	actual, _ := os.ReadFile(f.reg.SourceFile)
	if string(actual) != string(f.source) || f.runner.reloads != 0 {
		t.Fatal("validation modified live source")
	}
	result, err := f.engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), ValidationID: plan.ValidationID, Settings: Settings{CacheControl: "no_store"}})
	if err != nil {
		t.Fatal(err)
	}
	actual, _ = os.ReadFile(f.reg.SourceFile)
	if !strings.Contains(string(actual), "encode zstd\n") || !strings.Contains(string(actual), "SENSITIVE") || !strings.Contains(string(actual), "?X-Content-Type-Options") || strings.Contains(string(actual), "no-store") {
		t.Fatal("sealed plan or preserve fragments were ignored")
	}
	if result.Settings.CacheControl != "preserve" || result.SourceDigest != Digest(actual) {
		t.Fatal("deployment result does not match applied plan")
	}
	rolled, err := f.engine.Run(t.Context(), Request{Operation: "rollback", SiteKey: f.reg.SiteKey, ExpectedDigest: result.SourceDigest, RevisionID: baseline.RootRevisionID})
	if err != nil {
		t.Fatal(err)
	}
	actual, _ = os.ReadFile(f.reg.SourceFile)
	if string(actual) != string(f.source) || rolled.SourceDigest != Digest(f.source) {
		t.Fatal("rollback did not restore exact source")
	}
}
func TestRuntimeMismatchAndGlobalDriftStopDeployment(t *testing.T) {
	f := setupEngine(t)
	f.runner.active = []byte(`{"unexpected":"active-config"}`)
	if _, err := f.engine.Run(t.Context(), Request{Operation: "adopt", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source)}); !errors.Is(err, ErrConflict) {
		t.Fatal("adopt accepted a non-running configuration")
	}
	f.runner.active, _ = f.runner.Adapt(t.Context(), f.reg.MasterFile)
	adopt(t, f)
	plan := validate(t, f, Settings{Compression: "zstd"})
	f.runner.extra = "concurrent-other-site-change"
	if _, err := f.engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), ValidationID: plan.ValidationID}); !errors.Is(err, ErrConflict) {
		t.Fatal("global config drift was applied")
	}
	actual, _ := os.ReadFile(f.reg.SourceFile)
	if string(actual) != string(f.source) {
		t.Fatal("drift modified source")
	}
}
func TestReloadFailureRestoresSourceAndActiveConfiguration(t *testing.T) {
	f := setupEngine(t)
	adopt(t, f)
	plan := validate(t, f, Settings{Compression: "zstd"})
	f.runner.failReload = true
	if _, err := f.engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), ValidationID: plan.ValidationID}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(f.reg.SourceFile)
	if string(actual) != string(f.source) || f.runner.reloads != 2 {
		t.Fatal("last good configuration was not reloaded")
	}
	result, err := f.engine.Run(t.Context(), Request{Operation: "inspect", SiteKey: f.reg.SiteKey})
	if err != nil || result.Drifted || result.RuntimeUncertain {
		t.Fatal("restored runtime did not match source")
	}
}
func TestUnappliedReloadAndUnsupportedSettingsFailClosed(t *testing.T) {
	f := setupEngine(t)
	adopt(t, f)
	if _, err := f.engine.Run(t.Context(), Request{Operation: "validate", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), Settings: Settings{CacheControl: "no_store"}}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("disabled capability accepted")
	}
	plan := validate(t, f, Settings{Compression: "zstd"})
	f.runner.ignoreReload = true
	if _, err := f.engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), ValidationID: plan.ValidationID}); !errors.Is(err, ErrConflict) {
		t.Fatal("successful CLI without active change was accepted")
	}
	actual, _ := os.ReadFile(f.reg.SourceFile)
	if string(actual) != string(f.source) {
		t.Fatal("unapplied reload did not restore source")
	}
}
func TestSymlinksAndWritableTemplatesAreRejected(t *testing.T) {
	f := setupEngine(t)
	if err := os.Chmod(f.reg.BaseTemplate, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := f.engine.Run(t.Context(), Request{Operation: "inspect", SiteKey: f.reg.SiteKey}); !errors.Is(err, ErrPermission) {
		t.Fatal("writable privileged template accepted")
	}
	if err := os.Chmod(f.reg.BaseTemplate, 0600); err != nil {
		t.Fatal(err)
	}
	old := f.reg.SourceFile + ".real"
	if err := os.Rename(f.reg.SourceFile, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, f.reg.SourceFile); err != nil {
		t.Fatal(err)
	}
	if _, err := f.engine.Run(t.Context(), Request{Operation: "inspect", SiteKey: f.reg.SiteKey}); err == nil {
		t.Fatal("source symlink accepted")
	}
}
func TestRequestAllowlistRejectsRawCodePathsAndArbitraryFields(t *testing.T) {
	for _, data := range []string{
		`{"operation":"inspect","siteKey":"../etc/passwd"}`,
		`{"operation":"inspect","siteKey":"registered-site","source":"root /etc"}`,
		`{"operation":"validate","siteKey":"registered-site","expectedDigest":"` + strings.Repeat("a", 64) + `","settings":{"root":"/etc"}}`,
		`{"operation":"validate","siteKey":"registered-site","expectedDigest":"` + strings.Repeat("a", 64) + `","settings":{"compression":"gzip\nexec evil"}}`,
		`{"operation":"inspect","siteKey":"registered-site"} {}`,
	} {
		if _, err := DecodeRequest(strings.NewReader(data)); err == nil {
			t.Fatal("unsafe request accepted")
		}
	}
}

func TestPersistenceFailureAfterReloadCompensatesEvenForPreserve(t *testing.T) {
	for _, settings := range []Settings{{}, {Compression: "zstd"}} {
		t.Run(settings.Compression, func(t *testing.T) {
			f := setupEngine(t)
			adopt(t, f)
			plan := validate(t, f, settings)
			f.runner.afterReload = func() {
				f.runner.afterReload = nil
				sites := filepath.Join(f.engine.stateRoot, "sites")
				if err := os.Rename(sites, sites+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(sites, []byte("simulate unavailable state directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), ValidationID: plan.ValidationID}); err == nil {
				t.Fatal("failed persistence was acknowledged")
			}
			actual, _ := os.ReadFile(f.reg.SourceFile)
			if !bytes.Equal(actual, f.source) || f.runner.reloads != 2 {
				t.Fatal("persistence failure did not compensate the live source")
			}
			desired, _ := f.runner.Adapt(t.Context(), f.reg.MasterFile)
			a, _ := canonicalConfigDigest(desired, nil)
			b, _ := canonicalConfigDigest(f.runner.active, nil)
			if a != b {
				t.Fatal("persistence failure left runtime different from restored source")
			}
		})
	}
}
