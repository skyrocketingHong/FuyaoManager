package themecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// contractFixtures points at the repository contract directory from this
// package's directory (repo/backend/internal/themecontract).
const contractFixtures = "../../../contracts/theme"

func loadSetForTest(t *testing.T) *Set {
	t.Helper()
	set, err := Load(contractFixtures)
	if err != nil {
		t.Fatalf("Load(%q): %v", contractFixtures, err)
	}
	return set
}

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractFixtures, rel))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", rel, err)
	}
	return data
}

// runValidFixtures walks dir and runs each .json file through validate. Every
// file must succeed; failures report the full validation error.
func runValidFixtures(t *testing.T, set *Set, dir string, validate func([]byte) error) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(contractFixtures, dir))
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s contains no fixtures", dir)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data := readFixture(t, filepath.Join(dir, entry.Name()))
		if err := validate(data); err != nil {
			t.Errorf("%s/%s: unexpected error: %v", dir, entry.Name(), err)
		}
	}
}

// runInvalidFixtures walks dir and requires each .json file to fail validation
// with an error containing the expected substring. Every fixture must appear
// in the expectation table and vice versa.
func runInvalidFixtures(t *testing.T, set *Set, dir string, validate func([]byte) error, wantSubstring map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(contractFixtures, dir))
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		seen[entry.Name()] = true
		data := readFixture(t, filepath.Join(dir, entry.Name()))
		err := validate(data)
		if err == nil {
			t.Errorf("%s/%s: expected validation error, got none", dir, entry.Name())
			continue
		}
		want, ok := wantSubstring[entry.Name()]
		if !ok {
			t.Errorf("%s/%s: missing expected substring in test table", dir, entry.Name())
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s/%s: error %q does not contain %q", dir, entry.Name(), err.Error(), want)
		}
	}
	for name := range wantSubstring {
		if !seen[name] {
			t.Errorf("%s: expected fixture %s not found", dir, name)
		}
	}
}

func TestLoad(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("Load on an empty directory must fail")
	}
}

func TestValidateRejectsNonJSON(t *testing.T) {
	set := loadSetForTest(t)
	for name, validate := range map[string]func([]byte) error{
		"manifest": set.ValidateManifest,
		"input":    set.ValidateInputMetadata,
		"release":  set.ValidateRelease,
	} {
		if err := validate([]byte("not json")); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
			t.Errorf("%s: expected invalid JSON error, got %v", name, err)
		}
	}
}

func TestManifestFixtures(t *testing.T) {
	set := loadSetForTest(t)
	runValidFixtures(t, set, filepath.Join("examples", "manifests", "valid"), set.ValidateManifest)
	runInvalidFixtures(t, set, filepath.Join("examples", "manifests", "invalid"), set.ValidateManifest, map[string]string{
		"bad-contract.json":                "fuyao.theme.manifest/v1",
		"bad-id.json":                      "pattern",
		"bad-version.json":                 "pattern",
		"unknown-adapter.json":             "node22-sveltekit-static",
		"unknown-input-protocol.json":      "fuyao.theme.build-input/v1",
		"unknown-capability.json":          "one of",
		"duplicate-capabilities.json":      "are equal",
		"empty-capabilities.json":          "minItems",
		"config-schema-traversal.json":     "pattern",
		"config-schema-absolute.json":      "pattern",
		"config-schema-bad-format.json":    "json-schema-2020-12",
		"spa-fallback-traversal.json":      "pattern",
		"trailing-slash-path.json":         "not",
		"unknown-url-convention.json":      "one of",
		"required-artifact-traversal.json": "pattern",
		"extra-field.json":                 "not allowed",
		"missing-required.json":            "outputs",
	})
}

func TestBuildInputFixtures(t *testing.T) {
	set := loadSetForTest(t)
	runValidFixtures(t, set, filepath.Join("examples", "build-input", "valid"), set.ValidateInputMetadata)
	runInvalidFixtures(t, set, filepath.Join("examples", "build-input", "invalid"), set.ValidateInputMetadata, map[string]string{
		"bad-contract.json":      "fuyao.theme.build-input/v1",
		"unknown-mode.json":      "one of",
		"bad-digest.json":        "pattern",
		"bad-theme-version.json": "pattern",
		"bad-datetime.json":      "date-time",
		"bad-calendar-date.json": "date-time",
		"extra-field.json":       "operator",
	})
}

func TestReleaseFixtures(t *testing.T) {
	set := loadSetForTest(t)
	runValidFixtures(t, set, filepath.Join("examples", "release", "valid"), set.ValidateRelease)
	runInvalidFixtures(t, set, filepath.Join("examples", "release", "invalid"), set.ValidateRelease, map[string]string{
		"bad-schema-version.json": "schemaVersion",
		"bad-release-id.json":     "pattern",
		"dot-release-id.json":     "'not' failed",
		"dotdot-release-id.json":  "'not' failed",
		"bad-reason.json":         "one of",
		"bad-theme-digest.json":   "pattern",
		"bad-spa-fallback.json":   "none matched",
		"bad-calendar-date.json":  "date-time",
		"missing-routes.json":     "routes",
		"extra-field.json":        "rollbackTo",
	})
}

func TestThemeConfigFixtures(t *testing.T) {
	set := loadSetForTest(t)
	schemaDoc := readFixture(t, filepath.Join("examples", "theme-config", "fuyao-homepage.config.schema.json"))

	if err := set.ValidateThemeConfig(
		schemaDoc, readFixture(t, filepath.Join("examples", "theme-config", "valid", "empty.json")),
	); err != nil {
		t.Errorf("valid theme config rejected: %v", err)
	}

	for name, want := range map[string]string{
		"unknown-option.json": "not allowed",
		"not-object.json":     "array",
	} {
		err := set.ValidateThemeConfig(schemaDoc, readFixture(t, filepath.Join("examples", "theme-config", "invalid", name)))
		if err == nil {
			t.Errorf("%s: expected validation error, got none", name)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q does not contain %q", name, err.Error(), want)
		}
	}
}

func TestThemeConfigRejectsWrongDialect(t *testing.T) {
	set := loadSetForTest(t)
	const draft07 = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`
	err := set.ValidateThemeConfig([]byte(draft07), []byte("{}"))
	if err == nil || !strings.Contains(err.Error(), "2020-12") {
		t.Fatalf("expected dialect rejection, got %v", err)
	}
}

func TestCheckPairing(t *testing.T) {
	manifest := readFixture(t, filepath.Join("examples", "manifests", "valid", "fuyao-homepage.json"))
	production := readFixture(t, filepath.Join("examples", "build-input", "valid", "production.json"))
	if err := CheckPairing(manifest, production); err != nil {
		t.Fatalf("matching pair rejected: %v", err)
	}

	wrongContract := readFixture(t, filepath.Join("examples", "build-input", "invalid", "bad-contract.json"))
	if err := CheckPairing(manifest, wrongContract); err == nil || !strings.Contains(err.Error(), "inputProtocol") {
		t.Errorf("contract mismatch not reported: %v", err)
	}

	wrongVersion := readFixture(t, filepath.Join("examples", "build-input", "invalid", "bad-theme-version.json"))
	if err := CheckPairing(manifest, wrongVersion); err == nil || !strings.Contains(err.Error(), "does not match manifest") {
		t.Errorf("version mismatch not reported: %v", err)
	}
}

func TestMissingCapabilities(t *testing.T) {
	manifest := readFixture(t, filepath.Join("examples", "manifests", "valid", "fuyao-homepage.json"))
	all := []string{"blog", "albums", "footprint", "friends", "pay", "favicon"}
	missing, err := MissingCapabilities(manifest, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Errorf("full coverage reported missing capabilities: %v", missing)
	}

	missing, err = MissingCapabilities(manifest, []string{"sitemap", "blog", "guestbook", "blog", "pay"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sitemap", "guestbook"}
	if len(missing) != len(want) || missing[0] != want[0] || missing[1] != want[1] {
		t.Errorf("missing = %v, want %v (ordered, deduplicated)", missing, want)
	}
}

func TestMissingURLConventions(t *testing.T) {
	manifest := readFixture(t, filepath.Join("examples", "manifests", "valid", "fuyao-homepage.json"))
	missing, err := MissingURLConventions(manifest, []string{
		"release.json", "posts", "data", "media-albums", "sitemap", "blog-rss", "blog-search",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Errorf("full coverage reported missing conventions: %v", missing)
	}

	minimal := readFixture(t, filepath.Join("examples", "manifests", "valid", "minimal.json"))
	missing, err = MissingURLConventions(minimal, []string{"posts", "sitemap", "posts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "sitemap" {
		t.Errorf("missing = %v, want [sitemap]", missing)
	}
}

func TestConfigSchemaFile(t *testing.T) {
	declared := readFixture(t, filepath.Join("examples", "manifests", "valid", "fuyao-homepage.json"))
	file, ok, err := ConfigSchemaFile(declared)
	if err != nil || !ok || file != "theme.config.schema.json" {
		t.Fatalf("file=%q ok=%v err=%v", file, ok, err)
	}
	withoutSchema := readFixture(t, filepath.Join("examples", "manifests", "valid", "minimal.json"))
	if _, ok, err := ConfigSchemaFile(withoutSchema); err != nil || ok {
		t.Fatalf("minimal manifest should declare no configSchema, got ok=%v err=%v", ok, err)
	}
	if _, _, err := ConfigSchemaFile([]byte("{")); err == nil {
		t.Fatal("malformed manifest must fail ConfigSchemaFile")
	}
}

func TestValidateThemeConfigSchema(t *testing.T) {
	set := loadSetForTest(t)
	valid := readFixture(t, filepath.Join("examples", "theme-config", "fuyao-homepage.config.schema.json"))
	if err := set.ValidateThemeConfigSchema(valid); err != nil {
		t.Fatalf("valid schema rejected: %v", err)
	}

	for name, doc := range map[string]string{
		"invalid JSON":    "{",
		"not an object":   "[]",
		"wrong dialect":   `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`,
		"unknown keyword": `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"not-a-type"}`,
	} {
		if err := set.ValidateThemeConfigSchema([]byte(doc)); err == nil {
			t.Errorf("%s: expected schema rejection, got none", name)
		}
	}
}

func TestValidateEmptyThemeConfig(t *testing.T) {
	set := loadSetForTest(t)
	if err := set.ValidateEmptyThemeConfig([]byte("{}")); err != nil {
		t.Fatalf("empty object rejected: %v", err)
	}
	for name, want := range map[string]string{
		"unknown option": "only {}",
		"array":          "must be an object",
		"invalid JSON":   "invalid JSON",
	} {
		doc := map[string]string{
			"unknown option": `{"accentColor":"#006F6B"}`,
			"array":          `[]`,
			"invalid JSON":   "{",
		}[name]
		err := set.ValidateEmptyThemeConfig([]byte(doc))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v does not contain %q", name, err, want)
		}
	}
}
