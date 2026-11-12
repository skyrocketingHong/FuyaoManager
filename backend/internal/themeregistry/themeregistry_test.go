package themeregistry

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// repoRoot resolves the repository root from this test file's location.
func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..")
}

func contractsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "contracts", "theme")
}

// fixtureThemeRoot points at the repository's bundled contract-valid theme
// (see internal/httpapi/testdata/theme); registration of a real external
// theme is exercised through FUYAO_TEST_THEME_ROOT in the httpapi build
// tests.
func fixtureThemeRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "backend", "internal", "httpapi", "testdata", "theme")
}

// TestLoadFirstTheme loads the bundled fixture theme: it must register from
// its existing theme.json and config schema.
func TestLoadFirstTheme(t *testing.T) {
	theme, err := LoadThemeRoot(context.Background(), contractsDir(t), fixtureThemeRoot(t))
	if err != nil {
		t.Fatalf("load fixture theme: %v", err)
	}
	if theme.ThemeID != "manager-test-theme" || theme.Version == "" {
		t.Fatalf("unexpected identity %s@%s", theme.ThemeID, theme.Version)
	}
	if theme.Adapter != "node22-sveltekit-static" {
		t.Fatalf("unexpected adapter %q", theme.Adapter)
	}
	if theme.ConfigSchema == nil {
		t.Fatal("expected the declared config schema to be loaded")
	}
	if theme.SourcePath == "" || !filepath.IsAbs(theme.SourcePath) {
		t.Fatalf("source path %q must be absolute", theme.SourcePath)
	}
	// The digest covers the exact theme.json bytes on disk.
	raw, err := os.ReadFile(filepath.Join(fixtureThemeRoot(t), "theme.json"))
	if err != nil {
		t.Fatalf("read theme.json: %v", err)
	}
	want := sha256.Sum256(raw)
	if string(theme.ManifestDigest) != string(want[:]) {
		t.Fatal("manifest digest does not cover the exact theme.json bytes")
	}
}

// TestLoadRelativeThemeRoot proves a relative theme-root spelling (the common
// `../<theme>` CLI invocation) resolves to the same absolute real path instead
// of failing or recording a relative source.
func TestLoadRelativeThemeRoot(t *testing.T) {
	root := fixtureThemeRoot(t)
	absTheme, err := LoadThemeRoot(context.Background(), contractsDir(t), root)
	if err != nil {
		t.Fatalf("absolute load: %v", err)
	}
	relTheme, err := LoadThemeRoot(context.Background(), contractsDir(t), filepath.Join("..", "httpapi", "testdata", "theme"))
	if err != nil {
		t.Fatalf("relative load: %v", err)
	}
	if relTheme.SourcePath != absTheme.SourcePath || !filepath.IsAbs(relTheme.SourcePath) {
		t.Fatalf("relative source %q must equal the absolute %q", relTheme.SourcePath, absTheme.SourcePath)
	}
}

// TestLoadRejectsNonDirectoryRoot proves a theme root that is a plain file is
// rejected instead of registering a source path that cannot hold a theme.
func TestLoadRejectsNonDirectoryRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadThemeRoot(context.Background(), contractsDir(t), file); err == nil {
		t.Fatal("expected rejection for a file as theme root")
	}
}

// TestLoadRejectsInvalidManifests proves registration refuses invalid
// manifests instead of degrading silently.
func TestLoadRejectsInvalidManifests(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
	}{
		{"unknown adapter", `{"contract":"fuyao.theme.manifest/v1","id":"bad-theme-x","version":"1.0.0","adapter":"shell-executor","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["blog"],"outputs":{"spaFallback":null,"urlConventions":["release.json"],"requiredArtifacts":[]}}`},
		{"missing required", `{"contract":"fuyao.theme.manifest/v1","id":"bad-theme-x","version":"1.0.0"}`},
		{"traversal config schema", `{"contract":"fuyao.theme.manifest/v1","id":"bad-theme-x","version":"1.0.0","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["blog"],"configSchema":{"file":"../escape.json","format":"json-schema-2020-12"},"outputs":{"spaFallback":null,"urlConventions":["release.json"],"requiredArtifacts":[]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "theme.json"), []byte(tc.manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadThemeRoot(context.Background(), contractsDir(t), root); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

// TestLoadRejectsBrokenConfigSchema proves a declared config schema must
// exist inside the root and compile as JSON Schema 2020-12.
func TestLoadRejectsBrokenConfigSchema(t *testing.T) {
	manifest := `{"contract":"fuyao.theme.manifest/v1","id":"schemaless-x","version":"1.0.0","adapter":"node22-sveltekit-static","inputProtocol":"fuyao.theme.build-input/v1","capabilities":["blog"],"configSchema":{"file":"theme.config.schema.json","format":"json-schema-2020-12"},"outputs":{"spaFallback":null,"urlConventions":["release.json"],"requiredArtifacts":[]}}`

	t.Run("missing file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "theme.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadThemeRoot(context.Background(), contractsDir(t), root); err == nil {
			t.Fatal("expected rejection for a missing config schema file")
		}
	})

	t.Run("non-compiling schema", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "theme.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		bad := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"bogus-type"}`
		if err := os.WriteFile(filepath.Join(root, "theme.config.schema.json"), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadThemeRoot(context.Background(), contractsDir(t), root); err == nil {
			t.Fatal("expected rejection for a non-compiling config schema")
		}
	})

	t.Run("symlink escape", func(t *testing.T) {
		outside := t.TempDir()
		outsideDoc := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`
		if err := os.WriteFile(filepath.Join(outside, "outside.json"), []byte(outsideDoc), 0o644); err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "theme.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "outside.json"), filepath.Join(root, "theme.config.schema.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadThemeRoot(context.Background(), contractsDir(t), root); err == nil {
			t.Fatal("expected rejection for a config schema symlink escaping the root")
		}
	})
}

func TestCheckTrustedAdapter(t *testing.T) {
	if err := CheckTrustedAdapter("node22-sveltekit-static"); err != nil {
		t.Fatalf("trusted adapter rejected: %v", err)
	}
	err := CheckTrustedAdapter("evil-executor")
	if !errors.Is(err, ErrAdapterNotTrusted) {
		t.Fatalf("expected ErrAdapterNotTrusted, got %v", err)
	}
}
