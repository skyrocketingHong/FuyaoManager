package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// themeContractDir points at the repository contract directory from this
// package's directory (repo/admin/backend/internal/cli).
const themeContractDir = "../../../../contracts/theme"

func fixture(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(themeContractDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// themeRootWith builds a temporary theme root containing theme.json and, when
// schemaContent is non-empty, the declared configuration schema.
func themeRootWith(t *testing.T, manifest, schemaContent string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "theme.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if schemaContent != "" {
		if err := os.WriteFile(filepath.Join(root, "theme.config.schema.json"), []byte(schemaContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunValidateThemeValid(t *testing.T) {
	root := themeRootWith(
		t,
		fixture(t, filepath.Join("examples", "manifests", "valid", "fuyao-homepage.json")),
		fixture(t, filepath.Join("examples", "theme-config", "fuyao-homepage.config.schema.json")),
	)
	if err := runValidateTheme([]string{"--contracts", themeContractDir, root}); err != nil {
		t.Fatalf("valid theme rejected: %v", err)
	}
}

func TestRunValidateThemeWithConfig(t *testing.T) {
	root := themeRootWith(
		t,
		fixture(t, filepath.Join("examples", "manifests", "valid", "fuyao-homepage.json")),
		fixture(t, filepath.Join("examples", "theme-config", "fuyao-homepage.config.schema.json")),
	)
	configFile := filepath.Join(root, "theme.config.json")
	if err := os.WriteFile(configFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runValidateTheme([]string{"--contracts", themeContractDir, "--config", configFile, root}); err != nil {
		t.Fatalf("valid theme config rejected: %v", err)
	}
	if err := os.WriteFile(configFile, []byte(`{"accentColor":"#006F6B"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runValidateTheme([]string{"--contracts", themeContractDir, "--config", configFile, root})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("invalid theme config accepted: %v", err)
	}
}

func TestRunValidateThemeNoConfigSchema(t *testing.T) {
	root := themeRootWith(t, fixture(t, filepath.Join("examples", "manifests", "valid", "minimal.json")), "")
	if err := runValidateTheme([]string{"--contracts", themeContractDir, root}); err != nil {
		t.Fatalf("minimal theme rejected: %v", err)
	}
	if err := runValidateTheme([]string{"--contracts", themeContractDir, "--config", root, root}); err == nil {
		t.Fatal("--config with a manifest that declares no configSchema must fail")
	}
}

func TestRunValidateThemeInvalidManifest(t *testing.T) {
	root := themeRootWith(t, fixture(t, filepath.Join("examples", "manifests", "invalid", "extra-field.json")), "")
	err := runValidateTheme([]string{"--contracts", themeContractDir, root})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("invalid manifest accepted: %v", err)
	}
}

func TestRunValidateThemeMissingSchemaFile(t *testing.T) {
	// Valid manifest declaring a config schema, but the file is absent.
	root := themeRootWith(t, fixture(t, filepath.Join("examples", "manifests", "valid", "fuyao-homepage.json")), "")
	err := runValidateTheme([]string{"--contracts", themeContractDir, root})
	if err == nil || !strings.Contains(err.Error(), "config schema") {
		t.Fatalf("missing config schema not reported: %v", err)
	}
}

func TestRunValidateThemeArgumentErrors(t *testing.T) {
	if err := runValidateTheme([]string{"/tmp"}); err == nil {
		t.Fatal("missing --contracts must fail")
	}
	if err := runValidateTheme([]string{"--contracts", themeContractDir}); err == nil {
		t.Fatal("missing theme root must fail")
	}
}
