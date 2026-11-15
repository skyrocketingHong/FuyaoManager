// Package cli implements the fuyao-admin commands: migrate, serve,
// init-admin, login-code, revoke-sessions, bootstrap-admin, create-site,
// validate-theme, and register-theme.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildinfo"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/config"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentbind"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentmigration"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/edge"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/httpapi"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/managedpublish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/mediatask"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/siteprovision"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/wa"
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
	case "version":
		err = json.NewEncoder(os.Stdout).Encode(buildinfo.Current())
	case "migrate":
		err = runMigrate(ctx)
	case "sync-caddy":
		err = runSyncCaddy(ctx, args[1:])
	case "import-content":
		err = runImportContent(ctx, args[1:])
	case "serve":
		err = runServe(ctx)
	case "init-admin":
		err = runInitAdmin(ctx, args[1:])
	case "login-code":
		err = runLoginCode(ctx, args[1:])
	case "revoke-sessions":
		err = runRevokeSessions(ctx, args[1:])
	case "bootstrap-admin":
		err = runBootstrapAdmin(ctx, args[1:])
	case "create-site":
		err = runCreateSite(ctx, args[1:])
	case "validate-theme":
		err = runValidateTheme(args[1:])
	case "register-theme":
		err = runRegisterTheme(ctx, args[1:])
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
  version                 Print the product version and exact build commit as JSON.
  sync-caddy --username X  Register the real Caddy inventory without changing website files.
  import-content          Copy real website data into the platform library and an independent site workspace.
  migrate                 Apply pending SQL migrations and exit.
  serve                   Run the management API (loopback or a trusted host).
  init-admin              Register the first administrator and print a
                          single-use initialization code for the admin
                          console (CLI-authorized first-run binding).
  login-code              Print a single-use login code for an active
                          administrator (one code per login).
  revoke-sessions         Revoke every live session of an administrator.
  bootstrap-admin         Register the first administrator and print a
                          single-use passkey enrollment permit (legacy
                          passkey path; requires an https or localhost
                          WebAuthn origin).
  create-site             Create a site owned by an active administrator.
  validate-theme          Validate a theme root (theme.json) against the
                          theme contracts, optionally with a theme config.
  register-theme          Validate a theme root and record it in the theme
                          catalog (operator-controlled registration entry).

Configuration is read from the environment; see README.md for the variable
reference. validate-theme and register-theme read only files given on their
command line.
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// WebAuthn requires a domain-string RP ID; an IP-literal deployment (the
	// plain-HTTP private topology) cannot satisfy it. Passkeys are the legacy
	// compatibility path, never a dependency of one-time-code login, so the
	// server starts and the passkey endpoints answer 503 instead of the whole
	// service failing to boot.
	var webAuthn *webauthnlib.WebAuthn
	webAuthn, err = wa.New(wa.Params{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPDisplayName,
		RPOrigins:     cfg.RPOrigins,
		ChallengeTTL:  cfg.ChallengeTTL,
	})
	if err != nil {
		logger.Warn("passkey authentication unavailable; one-time-code login remains active", "error", err)
		webAuthn = nil
	}

	// Deterministic repo root: an explicitly configured REPO_ROOT is verified
	// against this repository's markers, otherwise the root is discovered
	// upward from the working directory (starting `go -C backend` resolves
	// the repository, not backend/). Nothing is guessed — a missing root
	// fails loudly instead of adopting another project or website.
	repoRoot, err := resolveRepoRoot(cfg.RepoRoot)
	if err != nil {
		return err
	}

	// The theme-contract validator is optional: without it the server still
	// serves the existing MVP surface, while saving a theme selection fails
	// with an explicit 503 naming the missing configuration. The bundled
	// contracts/theme of the resolved repository is the default;
	// FUYAO_ADMIN_THEME_CONTRACTS overrides it (a broken explicit directory
	// still fails loudly).
	var themeContracts httpapi.ThemeConfigValidator
	contractsDir := cfg.ThemeContractsDir
	if contractsDir == "" {
		if bundled := filepath.Join(repoRoot, "contracts", "theme"); dirExists(bundled) {
			contractsDir = bundled
		}
	}
	if contractsDir != "" {
		set, err := themecontract.Load(contractsDir)
		if err != nil {
			return fmt.Errorf("load theme contracts from %s: %w", contractsDir, err)
		}
		themeContracts = set
	}

	st := store.New(pool)

	// Build-preview executor and the private preview origin. The runner
	// serializes whole builds across the shared site working directories; the
	// preview listener is loopback-only and separate from the API origin.
	// Runtime data lives under <repo>/data by default — outside any theme
	// source tree — and every root stays overridable through the environment.
	dataRoot := filepath.Join(repoRoot, "data")
	buildsRoot := cfg.BuildsRoot
	if buildsRoot == "" {
		buildsRoot = filepath.Join(dataRoot, "builds")
	}
	sitesRoot := cfg.SitesRoot
	if sitesRoot == "" {
		sitesRoot = filepath.Join(dataRoot, "sites")
	}
	releasesRoot := cfg.ReleasesRoot
	if releasesRoot == "" {
		releasesRoot = filepath.Join(dataRoot, "releases")
	}
	platformRoot := os.Getenv("FUYAO_ADMIN_PLATFORM_CONTENT_ROOT")
	if platformRoot == "" {
		platformRoot = filepath.Join(filepath.Dir(sitesRoot), "platform-content")
	}
	// The three runtime roots default to three independent directories under
	// <repo>/data. The real-path overlap check runs BEFORE any directory is
	// created (missing tails resolve against their deepest existing
	// ancestors): an alias or nesting between sites, builds, and releases
	// would let one subsystem write into another's tree, and a rejected
	// configuration must leave the disk untouched.
	if err := ensureDistinctRoots(
		[]string{"sites", "builds", "releases", "platform-content"},
		[]string{sitesRoot, buildsRoot, releasesRoot, platformRoot},
	); err != nil {
		return err
	}
	for _, dir := range []string{buildsRoot, sitesRoot, releasesRoot} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("prepare runtime root %s: %w", dir, err)
		}
	}
	provisioner, err := siteprovision.New(sitesRoot)
	if err != nil {
		return fmt.Errorf("prepare sites root: %w", err)
	}
	contentStore, err := content.New(sitesRoot)
	if err != nil {
		return fmt.Errorf("prepare content store: %w", err)
	}
	if err = contentmigration.InitializePlatform(platformRoot); err != nil {
		return err
	}
	platformContent, err := content.New(platformRoot)
	if err != nil {
		return err
	}
	boundContent := &contentbind.Store{Store: contentStore, Platform: platformContent, Resolve: st.GetContentSources}
	scopeContent := func(id uuid.UUID) *content.Store {
		if id == content.PlatformScopeID {
			return platformContent
		}
		return contentStore
	}
	var platformMediaTheme uuid.UUID
	if raw := os.Getenv("FUYAO_ADMIN_PLATFORM_MEDIA_THEME_ROW_ID"); raw != "" {
		platformMediaTheme, err = uuid.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid platform media theme row")
		}
	} else if catalog, listErr := st.Themes.List(ctx); listErr == nil {
		for _, theme := range catalog {
			if theme.ThemeID == "fuyao-homepage" {
				platformMediaTheme = theme.ID
			}
		}
	}
	runner := buildrunner.NewRunner(buildrunner.Config{
		BuildStore: st.Builds,
		ThemeStore: st.Themes,
		Pool:       pool,
		RepoRoot:   repoRoot,
		BuildsRoot: buildsRoot,
		Mode:       cfg.BuildMode,
		ContentRoots: func(siteID uuid.UUID) (string, string, string, error) {
			return contentStore.ContentRoot(siteID), contentStore.ConfigRoot(siteID), contentStore.FaviconRoot(siteID), nil
		},
		MaterializeContent: func(ctx context.Context, build model.Build, target string) (string, string, string, error) {
			sources := store.IndependentSources()
			if len(build.ContentSources) > 0 {
				if err := json.Unmarshal(build.ContentSources, &sources); err != nil {
					return "", "", "", err
				}
			}
			albums, scope := contentStore, build.SiteID
			if sources.Albums == "platform" {
				albums, scope = platformContent, content.PlatformScopeID
			}
			if albums == nil {
				return "", "", "", content.ErrNotFound
			}
			var contentRoot, configRoot, faviconRoot string
			err := albums.WithAlbumMetaLock(ctx, scope, func() error {
				var err error
				contentRoot, configRoot, faviconRoot, err = contentbind.MaterializeWithMedia(ctx, contentStore, platformContent, st.PostMedia, build.SiteID, sources, target)
				return err
			})
			return contentRoot, configRoot, faviconRoot, err
		},
		Requirements: buildrunner.SiteRequirements{Capabilities: cfg.RequireCaps, URLConventions: cfg.RequireURLs},
		Logger:       logger,
	})
	if err := runner.Start(ctx); err != nil {
		return fmt.Errorf("start build runner: %w", err)
	}
	defer runner.Stop()

	// Preview origins are per-build listeners created on demand by Issue;
	// Close tears them all down with the service. Ports come from the
	// platform allocation table so a port never silently switches builds,
	// and the bind address reaches remote (private-network) browsers in
	// deployment while staying loopback in local development.
	managerOrigin := ""
	if strings.HasPrefix(cfg.RPOrigins[0], "https://") {
		managerOrigin = cfg.RPOrigins[0]
	}
	previews := buildrunner.NewPreviewServer(buildrunner.PreviewConfig{
		ManagerOrigin:      managerOrigin,
		ExternalPortOffset: cfg.PreviewPortOffset,
		Target:             st.Builds,
		BuildsRoot:         buildsRoot,
		BindAddr:           cfg.PreviewBind,
		ExternalBase:       cfg.PreviewExternalBase,
		Ports:              st.NetworkEntries,
		PortMin:            cfg.PortMin,
		PortMax:            cfg.PortMax,
		FrozenMediaRoot:    previewFrozenMediaRoot(cfg.BuildMode, buildsRoot),
	})
	defer previews.Close()

	// The published-site edge: per-site current pointers plus the managed
	// reverse-proxy snippet. Always constructed (EntryURL rendering works
	// without snippet management); snippet application activates only when
	// the operator configured a snippet directory AND the deployment switch
	// is on — run isolation (the default) never reconciles or applies Caddy.
	edgeMgr := edge.New(edge.Config{
		Pool:              pool,
		Entries:           st.NetworkEntries,
		SnippetDir:        cfg.EdgeSnippetDir,
		ApplyCmd:          cfg.EdgeApplyCmd,
		PrivateBind:       cfg.EdgePrivateBind,
		PublicBind:        cfg.EdgePublicBind,
		PublicHost:        cfg.EdgePublicHost,
		ReleasesRoot:      releasesRoot,
		SitesRoot:         sitesRoot,
		PortMin:           cfg.PortMin,
		PortMax:           cfg.PortMax,
		Logger:            logger,
		DeploymentEnabled: cfg.DeploymentEnabled,
	})
	if cfg.DeploymentEnabled {
		go func() {
			if err := edgeMgr.Reconcile(context.Background()); err != nil {
				logger.Error("edge reconcile at startup", "error", err)
			}
		}()
	}

	// Asynchronous media processing (album EXIF + thumbnails) reuses the
	// site theme's own Node scripts against the site content tree.
	mediaRunner := mediatask.NewRunner(mediatask.Config{
		Tasks:  st.MediaTasks,
		Themes: st.Themes,
		ContentRoots: func(siteID uuid.UUID) (string, string, string, error) {
			contentRoot := scopeContent(siteID).ContentRoot(siteID)
			return filepath.Join(contentRoot, "albums", "photos"),
				filepath.Join(contentRoot, "albums", "thumbnails"),
				filepath.Join(contentRoot, "albums", "metadata"), nil
		},
		AlbumLock: func(siteID uuid.UUID, fn func() error) error {
			return scopeContent(siteID).WithAlbumMetaLock(context.Background(), siteID, fn)
		},
		Logger: logger,
	})
	if err := mediaRunner.Start(ctx); err != nil {
		return fmt.Errorf("start media task runner: %w", err)
	}
	defer mediaRunner.Stop()

	// The publish pipeline seals succeeded builds into per-site release
	// directories and atomically switches the live release; published origins
	// are per-site loopback listeners serving the current release. Publishing
	// requires the theme contracts (release.json validation), mirroring the
	// theme-selection save behavior, and only starts when the deployment
	// switch is on: run isolation keeps builds and previews working while no
	// publish server exists and no edge application ever runs.
	var publisher httpapi.Publisher
	var publishedOrigins httpapi.PublishedOrigins
	var control func(context.Context, caddymanage.Request) (caddymanage.Result, error)
	var controlCommand []string
	for _, part := range strings.Split(os.Getenv("FUYAO_ADMIN_CADDY_CONTROL_CMD"), ",") {
		if part = strings.TrimSpace(part); part != "" {
			controlCommand = append(controlCommand, part)
		}
	}
	if len(controlCommand) > 0 {
		control = func(ctx context.Context, request caddymanage.Request) (caddymanage.Result, error) {
			return caddymanage.Execute(ctx, controlCommand, request)
		}
	}
	if cfg.DeploymentEnabled || cfg.CaddyManagementEnabled {
		if set, ok := themeContracts.(*themecontract.Set); ok {
			manager := publish.NewManager(publish.Config{
				Pool:         pool,
				Releases:     st.Releases,
				Builds:       st.Builds,
				Themes:       st.Themes,
				ReleasesRoot: releasesRoot,
				Contracts:    set,
				Requirements: buildrunner.SiteRequirements{Capabilities: cfg.RequireCaps, URLConventions: cfg.RequireURLs},
				Edge:         edgeMgr,
				Activation:   managedpublish.New(managedpublish.Config{Store: st, Control: control, Enabled: cfg.CaddyManagementEnabled, ReleasesRoot: releasesRoot, BuildsRoot: buildsRoot}),
				Logger:       logger,
			})
			publisher = manager
			if cfg.DeploymentEnabled {
				origins := publish.NewServer(st.Releases, releasesRoot)
				publishedOrigins = origins
				defer origins.Close()
			}
		}
	}

	contractData, err := os.ReadFile(filepath.Join(repoRoot, "contracts", "openapi", "openapi.yaml"))
	if err != nil {
		return err
	}
	var document map[string]any
	if err = yaml.Unmarshal(contractData, &document); err != nil {
		return err
	}
	contractJSON, err := json.Marshal(document)
	if err != nil {
		return err
	}
	var caddyCommand []string
	if raw := os.Getenv("FUYAO_ADMIN_CADDY_READ_CMD"); raw != "" {
		for _, arg := range strings.Split(raw, ",") {
			if arg = strings.TrimSpace(arg); arg != "" {
				caddyCommand = append(caddyCommand, arg)
			}
		}
	}
	var trafficSite uuid.UUID
	if raw := os.Getenv("FUYAO_ADMIN_TRAFFIC_STATS_SITE_ID"); raw != "" {
		trafficSite, err = uuid.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid traffic statistics site id")
		}
	}
	handler := httpapi.New(st, webAuthn, httpapi.Config{
		UpdaterSocket:          os.Getenv("FUYAO_ADMIN_UPDATER_SOCKET"),
		CaddyReadCmd:           caddyCommand,
		CaddyControl:           control,
		CaddyManagementEnabled: cfg.CaddyManagementEnabled,
		PlatformContent:        platformContent,
		PlatformContentThemeID: platformMediaTheme,
		TrafficStatsSiteID:     trafficSite,
		TrafficStatsURL:        os.Getenv("FUYAO_ADMIN_TRAFFIC_STATS_URL"),
		OpenAPIJSON:            contractJSON,
		RPID:                   cfg.RPID,
		RPOrigins:              cfg.RPOrigins,
		CookieSecure:           cfg.CookieSecure,
		SessionTTL:             cfg.SessionTTL,
		ChallengeTTL:           cfg.ChallengeTTL,
		RegistrationTTL:        cfg.RegistrationTTL,
		ThemeContracts:         themeContracts,
		BuildEnqueuer:          runner,
		Previews:               previews,
		Publisher:              publisher,
		PublishedOrigins:       publishedOrigins,
		Provisioner:            provisioner,
		Edge:                   edgeMgr,
		Content:                boundContent,
		MediaTasks:             st.MediaTasks,
		ReleasesRoot:           releasesRoot,
		DeploymentEnabled:      cfg.DeploymentEnabled,
		PortMin:                cfg.PortMin,
		PortMax:                cfg.PortMax,
	}, logger)

	if cfg.AdminUIDir != "" {
		if _, err := os.Stat(filepath.Join(cfg.AdminUIDir, "index.html")); err != nil {
			return fmt.Errorf("admin UI directory %s has no index.html: %w", cfg.AdminUIDir, err)
		}
		previewHost := ""
		if parsed, err := url.Parse(cfg.PreviewExternalBase); err == nil && parsed.Scheme == "https" {
			previewHost = parsed.Hostname()
		}
		handler = httpapi.WithUI(handler, cfg.AdminUIDir, previewHost)
	}

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

// runInitAdmin registers the first administrator and prints a single-use
// initialization code. This is the CLI-authorized first-run binding: the
// first anonymous browser visitor can never claim administrator identity.
func runInitAdmin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init-admin", flag.ContinueOnError)
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

	code, expiresAt, err := store.New(pool).InitAdmin(ctx, *username, *displayName, cfg.AuthCodeTTL)
	if err != nil {
		return err
	}
	// The code is a secret: printed once to the terminal, never logged.
	fmt.Printf("administrator %q registered (pending initialization)\n", *username)
	fmt.Printf("initialization code: %s\n", code)
	fmt.Printf("code expires:        %s\n", expiresAt.Local().Format(time.RFC3339))
	fmt.Println("Open the admin console in a browser and enter this code once to activate the account and sign in.")
	return nil
}

// runLoginCode prints a single-use login code for an active administrator.
// Each login needs a fresh code; codes are never reused.
func runLoginCode(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login-code", flag.ContinueOnError)
	username := fs.String("username", "", "administrator username")
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

	code, expiresAt, err := store.New(pool).IssueLoginCode(ctx, *username, cfg.AuthCodeTTL)
	if err != nil {
		return err
	}
	fmt.Printf("login code for %q: %s\n", *username, code)
	fmt.Printf("code expires:       %s\n", expiresAt.Local().Format(time.RFC3339))
	fmt.Println("Enter this code in the admin console to sign in. It can be used once.")
	return nil
}

// runRevokeSessions revokes every live session of an administrator, for
// example after a suspected leak or before handing over a machine.
func runRevokeSessions(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("revoke-sessions", flag.ContinueOnError)
	username := fs.String("username", "", "administrator username")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("--username is required")
	}

	pool, _, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	revoked, err := store.New(pool).RevokeUserSessions(ctx, *username)
	if err != nil {
		return err
	}
	fmt.Printf("revoked %d session(s) of %q\n", revoked, *username)
	return nil
}

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

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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

	rec, err := st.Sites.Create(ctx, store.NewSiteParams{
		Slug:          *slug,
		Name:          *name,
		Description:   *description,
		DefaultLocale: *locale,
	}, ownerUser.ID)
	if err != nil {
		return err
	}
	fmt.Printf("site created: %s (slug %s, owner %s)\n", rec.Site.ID, rec.Site.Slug, ownerName)
	return nil
}
