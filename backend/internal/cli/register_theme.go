package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/themeregistry"
)

// runRegisterTheme validates a theme root against the theme contracts and
// records it in the theme catalog. This is the only registration entry: the
// management HTTP API never accepts file paths, shell commands, or Git
// addresses. The recorded source path is the theme root itself — the same
// directory the manifest and configuration schema were validated from.
// Re-registering the same theme id and version with an identical manifest and
// configuration schema is idempotent; any change under the same version is
// rejected.
func runRegisterTheme(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("register-theme", flag.ContinueOnError)
	contractsDir := fs.String("contracts", "", "path to the contracts/theme directory (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *contractsDir == "" {
		return errors.New("--contracts is required")
	}
	if fs.NArg() != 1 {
		return errors.New("usage: fuyao-admin register-theme --contracts <dir> <theme-root>")
	}
	themeRoot := fs.Arg(0)

	pool, _, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	theme, created, err := themeregistry.Register(ctx, store.New(pool).Themes, *contractsDir, themeRoot)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("theme %s@%s is already registered with a different manifest or configuration schema; bump the theme version instead", theme.ThemeID, theme.Version)
		}
		return err
	}
	if created {
		fmt.Printf("theme registered: %s@%s (adapter %s, digest %x)\n",
			theme.ThemeID, theme.Version, theme.Adapter, theme.ManifestDigest[:8])
		return nil
	}
	fmt.Printf("theme already registered with an identical manifest and configuration schema; source path refreshed: %s@%s\n",
		theme.ThemeID, theme.Version)
	return nil
}
