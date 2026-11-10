// Package cli implements the fuyao-admin commands: migrate, serve,
// bootstrap-admin, and create-site.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/config"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/httpapi"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/wa"
)

// Main dispatches a subcommand and returns the process exit code.
func Main(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args[0] {
	case "migrate":
		err = runMigrate(ctx)
	case "serve":
		err = runServe(ctx)
	case "bootstrap-admin":
		err = runBootstrapAdmin(ctx, args[1:])
	case "create-site":
		err = runCreateSite(ctx, args[1:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "fuyao-admin: %v\n", err)
		return 1
	}
	return 0
}

func usage(f *os.File) {
	fmt.Fprint(f, `Usage: fuyao-admin <command> [flags]

Commands:
  migrate                 Apply pending SQL migrations and exit.
  serve                   Run the management API (loopback only).
  bootstrap-admin         Register the first administrator and print a
                          single-use passkey enrollment permit.
  create-site             Create a site owned by an active administrator.

Configuration is read from the environment; see README.md for the variable
reference.
`)
}

// openPool loads config, opens the pool, and applies migrations check.
func openPool(ctx context.Context) (*pgxpool.Pool, config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cfg, err
	}
	pool, err := db.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, cfg, err
	}
	return pool, cfg, nil
}

func runMigrate(ctx context.Context) error {
	pool, _, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	applied, err := db.ApplyMigrations(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Printf("applied %d migration(s)\n", applied)
	return nil
}

func runServe(ctx context.Context) error {
	pool, cfg, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	current, err := db.MigrationsApplied(ctx, pool)
	if err != nil {
		return err
	}
	latest, err := db.LatestVersion()
	if err != nil {
		return err
	}
	if current < latest {
		return fmt.Errorf("database schema is at migration %d while %d is required; run `fuyao-admin migrate` first", current, latest)
	}

	webAuthn, err := wa.New(wa.Params{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPDisplayName,
		RPOrigins:     cfg.RPOrigins,
		ChallengeTTL:  cfg.ChallengeTTL,
	})
	if err != nil {
		return fmt.Errorf("initialize WebAuthn: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler := httpapi.New(store.New(pool), webAuthn, httpapi.Config{
		RPID:         cfg.RPID,
		RPOrigins:    cfg.RPOrigins,
		CookieSecure: cfg.CookieSecure,
		SessionTTL:   cfg.SessionTTL,
		ChallengeTTL: cfg.ChallengeTTL,
	}, logger)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	logger.Info("management API listening", "addr", cfg.Addr, "rp_id", cfg.RPID, "origins", cfg.RPOrigins)
	fmt.Printf("fuyao-admin: listening on http://%s\n", cfg.Addr)

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "fuyao-admin: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

func runBootstrapAdmin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	username := fs.String("username", "", "administrator username (lowercase letters, digits, '.', '_', '-')")
	displayName := fs.String("display-name", "", "display name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("--username is required")
	}

	pool, cfg, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	code, expiresAt, err := store.New(pool).BootstrapAdmin(ctx, *username, *displayName, cfg.PermitTTL)
	if err != nil {
		return err
	}
	// The permit is a secret: it is printed once to the terminal and never
	// logged.
	fmt.Printf("administrator %q registered (pending passkey enrollment)\n", *username)
	fmt.Printf("enrollment permit: %s\n", code)
	fmt.Printf("permit expires:    %s\n", expiresAt.Local().Format(time.RFC3339))
	fmt.Println("Enter this permit in the admin console to register your passkey. It can be used once.")
	return nil
}

func runCreateSite(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("create-site", flag.ContinueOnError)
	slug := fs.String("slug", "", "site slug (lowercase letters, digits, hyphens)")
	name := fs.String("name", "", "site display name")
	description := fs.String("description", "", "site description")
	locale := fs.String("locale", "zh-CN", "default locale (zh-CN or en-US)")
	owner := fs.String("owner", "", "owner username (defaults to the only active administrator)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" || *name == "" {
		return errors.New("--slug and --name are required")
	}
	if !slugPattern.MatchString(*slug) {
		return fmt.Errorf("slug must match %s", slugPattern.String())
	}
	if *locale != "zh-CN" && *locale != "en-US" {
		return errors.New("--locale must be zh-CN or en-US")
	}
	if n := len([]rune(*name)); n < 1 || n > 200 {
		return errors.New("--name must be 1 to 200 characters")
	}

	pool, _, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)

	ownerName := *owner
	if ownerName == "" {
		active, err := st.Users.ListActive(ctx)
		if err != nil {
			return err
		}
		if len(active) != 1 {
			return errors.New("--owner is required unless exactly one active administrator exists")
		}
		ownerName = active[0].Username
	}
	ownerUser, err := st.Users.GetByUsername(ctx, ownerName)
	if err != nil {
		return fmt.Errorf("owner %q not found", ownerName)
	}
	if ownerUser.Status != "active" {
		return fmt.Errorf("owner %q has not completed passkey enrollment", ownerName)
	}

	rec, err := st.Sites.Create(ctx, *slug, *name, *description, *locale, ownerUser.ID)
	if err != nil {
		return err
	}
	fmt.Printf("site created: %s (slug %s, owner %s)\n", rec.Site.ID, rec.Site.Slug, ownerName)
	return nil
}
