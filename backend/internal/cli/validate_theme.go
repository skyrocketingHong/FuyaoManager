package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themeregistry"
)

// runValidateTheme validates a theme root against the contracts/theme
// schemas: theme.json must be valid, a declared configSchema file must exist
// inside the (realpath of the) theme root and compile as JSON Schema 2020-12,
// and an optional --config file must satisfy the theme's configuration rules.
func runValidateTheme(args []string) error {
	fs := flag.NewFlagSet("validate-theme", flag.ContinueOnError)
	contractsDir := fs.String("contracts", "", "path to the contracts/theme directory (required)")
	configPath := fs.String("config", "", "optional theme config JSON file validated against the theme config rules")
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
	manifestPath, err := themeregistry.ResolveWithin(themeRoot, "theme.json")
	if err != nil {
		return fmt.Errorf("locating theme.json: %w", err)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", manifestPath, err)
	}
	if err := set.ValidateManifest(manifestData); err != nil {
		return err
	}

	var configData []byte
	if *configPath != "" {
		configData, err = os.ReadFile(*configPath)
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
	}

	schemaFile, declared, err := themecontract.ConfigSchemaFile(manifestData)
	if err != nil {
		return err
	}
	if !declared {
		if configData == nil {
			fmt.Println("theme.json valid; no config schema declared")
			return nil
		}
		// The manifest declares no configSchema: per the contract, the only
		// valid theme configuration is an empty object.
		if err := set.ValidateEmptyThemeConfig(configData); err != nil {
			return err
		}
		fmt.Println("theme.json and theme config valid; no config schema declared, config is {}")
		return nil
	}
	// The schema pattern already forbids traversal and absolute paths; the
	// realpath check below additionally rejects symlinks that resolve outside
	// the theme root (the theme root itself may legitimately be a symlink).
	schemaPath, err := themeregistry.ResolveWithin(themeRoot, schemaFile)
	if err != nil {
		return fmt.Errorf("reading config schema %s: %w", schemaFile, err)
	}
	schemaDoc, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("reading config schema %s: %w", schemaFile, err)
	}
	if err := set.ValidateThemeConfigSchema(schemaDoc); err != nil {
		return err
	}
	if configData == nil {
		fmt.Printf("theme.json valid; config schema %s compiles (draft 2020-12)\n", schemaFile)
		return nil
	}
	if err := set.ValidateThemeConfig(schemaDoc, configData); err != nil {
		return err
	}
	fmt.Printf("theme.json and theme config valid; config schema %s\n", schemaFile)
	return nil
}
