package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/themecontract"
)

// runValidateTheme validates a theme root against the contracts/theme
// schemas: theme.json must be valid, a declared configSchema file must exist
// inside the theme root, and an optional --config file must satisfy it.
func runValidateTheme(args []string) error {
	fs := flag.NewFlagSet("validate-theme", flag.ContinueOnError)
	contractsDir := fs.String("contracts", "", "path to the contracts/theme directory (required)")
	configPath := fs.String("config", "", "optional theme config JSON file validated against the theme config schema")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *contractsDir == "" {
		return errors.New("--contracts is required")
	}
	if fs.NArg() != 1 {
		return errors.New("usage: fuyao-admin validate-theme --contracts <dir> [--config <file>] <theme-root>")
	}
	themeRoot := fs.Arg(0)

	set, err := themecontract.Load(*contractsDir)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(themeRoot, "theme.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", manifestPath, err)
	}
	if err := set.ValidateManifest(manifestData); err != nil {
		return err
	}

	schemaFile, declared, err := themecontract.ConfigSchemaFile(manifestData)
	if err != nil {
		return err
	}
	if !declared {
		if *configPath != "" {
			return errors.New("--config given, but the manifest declares no configSchema")
		}
		fmt.Println("theme.json valid; no config schema declared")
		return nil
	}
	// The schema pattern already forbids traversal and absolute paths; keep a
	// defense-in-depth check that the resolved path stays inside the theme root.
	schemaPath, err := resolveWithin(themeRoot, schemaFile)
	if err != nil {
		return err
	}
	schemaDoc, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("reading config schema %s: %w", schemaPath, err)
	}
	if *configPath == "" {
		fmt.Printf("theme.json valid; config schema %s present\n", schemaFile)
		return nil
	}
	configData, err := os.ReadFile(*configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	if err := set.ValidateThemeConfig(schemaDoc, configData); err != nil {
		return err
	}
	fmt.Printf("theme.json and theme config valid; config schema %s\n", schemaFile)
	return nil
}

func resolveWithin(root, name string) (string, error) {
	resolved := filepath.Clean(filepath.Join(root, name))
	relative, err := filepath.Rel(filepath.Clean(root), resolved)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || len(relative) >= 2 && relative[:2] == ".." {
		return "", fmt.Errorf("path %q escapes the theme root", name)
	}
	return resolved, nil
}
