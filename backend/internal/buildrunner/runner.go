// Package buildrunner executes persisted build-preview tasks: it composes the
// frozen input snapshot through the theme's own Node preparation flow (with
// the database-saved site settings and theme configuration injected), runs the
// single trusted adapter command sequence inside the registered theme root,
// harvests an immutable per-task artifact directory, and serves private
// preview origins on separate loopback-only listeners. Task state lives in
// PostgreSQL; only the process holding the executor advisory lock recovers
// and runs tasks, so a second server instance never marks the first one's
// active builds failed.
package buildrunner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// SiteRequirements is the trusted server-side statement of what this site type
// needs from a theme. It is deliberately independent of any theme's own
// manifest declaration; the gap check compares the two and blocks the build
// when the theme cannot serve the site's real routes and feeds.
type SiteRequirements struct {
	Capabilities   []string
	URLConventions []string
}

// BuildStore is the persistence surface the runner needs.
type BuildStore interface {
	Get(ctx context.Context, buildID uuid.UUID) (model.Build, error)
	MarkRunning(ctx context.Context, buildID uuid.UUID) (bool, error)
	MarkSucceeded(ctx context.Context, buildID uuid.UUID, artifactPath string, inputDigests json.RawMessage) error
	MarkFailed(ctx context.Context, buildID uuid.UUID, failure model.BuildFailure) error
	RecoverInterrupted(ctx context.Context) (int64, error)
	ListQueued(ctx context.Context) ([]uuid.UUID, error)
	UpdateSourceDigest(ctx context.Context, buildID uuid.UUID, aggregateDigest string, fileCount int) error
}

// ThemeStore resolves a build's frozen theme identity to its catalog row.
type ThemeStore interface {
	GetByRowID(ctx context.Context, rowID uuid.UUID) (model.Theme, error)
}

// Config configures the runner. RepoRoot is the platform repository root
// (audit logging only); every stage of a build runs inside the registered
// theme root captured on the task row. BuildsRoot holds per-task directories
// (snapshot + artifacts + logs). Mode selects the snapshot content source
// ("ci" uses the repository sample content, "production" requires the three
// trusted content-root settings).
type Config struct {
	BuildStore BuildStore
	ThemeStore ThemeStore
	Pool       *pgxpool.Pool
	RepoRoot   string
	BuildsRoot string
	Mode       string
	// ContentRoots resolves the per-site production content roots (content,
	// config, favicon) consumed by prepare-build-inputs in production mode.
	// nil is only valid for ci mode.
	ContentRoots func(siteID uuid.UUID) (contentRoot, configRoot, faviconRoot string, err error)
	// AlbumLock serializes the prepare step's album metadata regeneration
	// against human edits of the same files (same lock the media indexer
	// runs under). nil disables coordination (ci fixture mode).
	AlbumLock    func(siteID uuid.UUID, fn func() error) error
	Requirements SiteRequirements
	BuildTimeout time.Duration
	ShutdownWait time.Duration
	Logger       *slog.Logger
}

// advisoryLockKey distinguishes the build executor's lock from every other
// advisory lock user in the platform.
const advisoryLockKey = 0x46594c42 // "FYLB"

// Runner serializes whole builds. Exactly one process may hold the executor
// role: it owns the shared theme working directories (snapshot staging and
// build output), so the serialization covers the whole
// prepare-consume-build-harvest sequence, never a single sub-step.
type Runner struct {
	cfg      Config
	logger   *slog.Logger
	queue    chan uuid.UUID
	sem      chan struct{}
	wg       sync.WaitGroup
	stop     chan struct{}
	stopOnce sync.Once
	// buildCtx is cancelled by Stop so an in-flight build's commands die
	// with the server instead of outliving it.
	buildCtx context.Context
	cancel   func()

	mu       sync.Mutex
	executor bool
	lockConn *pgxpool.Conn // held while executor
	started  bool
}

// NewRunner assembles the runner. It does not start workers; call Start.
func NewRunner(cfg Config) *Runner {
	if cfg.BuildTimeout <= 0 {
		cfg.BuildTimeout = 15 * time.Minute
	}
	if cfg.ShutdownWait <= 0 {
		cfg.ShutdownWait = 30 * time.Second
	}
	if cfg.Mode == "" {
		cfg.Mode = "ci"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		cfg:      cfg,
		logger:   logger,
		queue:    make(chan uuid.UUID, 1024),
		sem:      make(chan struct{}, 1),
		stop:     make(chan struct{}),
		buildCtx: ctx,
		cancel:   cancel,
	}
}

// Start begins executor arbitration. The process tries to take the
// cluster-wide executor advisory lock; whichever process holds it runs
// recovery (running rows of a dead executor are marked failed) and the build
// worker. A process that cannot take the lock serves API reads only and keeps
// retrying, so it picks the executor role up if the holder goes away.
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("runner already started")
	}
	r.started = true
	r.mu.Unlock()

	r.wg.Add(1)
	go r.arbitrate()
	return nil
}

// arbitrate loops on acquiring the executor lock. On becoming executor it
// runs recovery once, then serves the queue until Stop.
func (r *Runner) arbitrate() {
	defer r.wg.Done()
	for {
		if r.becomeExecutor() {
			r.runAsExecutor()
			return
		}
		select {
		case <-r.stop:
			return
		case <-time.After(15 * time.Second):
		}
	}
}

// becomeExecutor tries to take the advisory lock on a dedicated connection.
func (r *Runner) becomeExecutor() bool {
	if r.cfg.Pool == nil {
		// Test double without a pool: in-process arbitration only.
		r.mu.Lock()
		r.executor = true
		r.mu.Unlock()
		return true
	}
	ctx, cancel := context.WithTimeout(r.buildCtx, 5*time.Second)
	conn, err := r.cfg.Pool.Acquire(ctx)
	cancel()
	if err != nil {
		r.logger.Warn("build executor arbitration acquire failed", "error", err)
		return false
	}
	var ok bool
	if err := conn.QueryRow(context.Background(), `SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&ok); err != nil || !ok {
		conn.Release()
		if err != nil {
			r.logger.Warn("build executor lock query failed", "error", err)
		}
		return false
	}
	r.mu.Lock()
	r.executor = true
	r.lockConn = conn
	r.mu.Unlock()
	r.logger.Info("build executor role acquired")
	return true
}

// runAsExecutor performs startup recovery exactly once for this executor
// generation, then runs the worker and sweeper until Stop. Only the executor
// may mark interrupted running rows failed — those rows belong to processes
// that held this same lock and are therefore dead when a new holder starts.
func (r *Runner) runAsExecutor() {
	defer r.releaseExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	failed, err := r.cfg.BuildStore.RecoverInterrupted(ctx)
	cancel()
	if err != nil {
		r.logger.Error("recover interrupted builds", "error", err)
	} else if failed > 0 {
		r.logger.Warn("marked interrupted builds failed", "count", failed)
	}
	queued, err := r.cfg.BuildStore.ListQueued(r.buildCtx)
	if err != nil {
		r.logger.Error("list queued builds", "error", err)
	} else {
		for _, id := range queued {
			r.Enqueue(id)
		}
	}

	// Hold the executor role until the worker and sweeper exit; releasing it
	// any earlier would let another process arbitrate while this one is
	// still mid-build.
	r.wg.Add(2)
	done := make(chan struct{})
	go func() {
		defer r.wg.Done()
		r.worker()
		close(done)
	}()
	go func() { defer r.wg.Done(); r.sweeper() }()
	<-done
}

// releaseExecutor drops the advisory lock connection.
func (r *Runner) releaseExecutor() {
	r.mu.Lock()
	conn := r.lockConn
	r.lockConn = nil
	r.executor = false
	r.mu.Unlock()
	if conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
		cancel()
		conn.Release()
	}
}

// Stop halts the runner: no new tasks start, in-flight builds are cancelled
// (their command process groups are killed by the context plumbing), and the
// worker drains within ShutdownWait. Recovery of the interrupted row happens
// on the next executor's startup.
func (r *Runner) Stop() {
	r.stopOnce.Do(func() {
		close(r.stop)
		r.cancel()
	})
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(r.cfg.ShutdownWait):
		r.logger.Warn("build runner shutdown wait exceeded; abandoning in-flight build")
	}
}

// IsExecutor reports whether this process currently holds the executor role.
func (r *Runner) IsExecutor() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.executor
}

// Enqueue schedules a build id. Duplicate ids are fine: MarkRunning makes
// execution claim-once. On a non-executor process this is a no-op — the
// executor's sweeper picks queued rows up.
func (r *Runner) Enqueue(id uuid.UUID) {
	if !r.IsExecutor() {
		return
	}
	select {
	case r.queue <- id:
	default:
		r.logger.Error("build queue full; sweeper will retry", "buildId", id)
	}
}

func (r *Runner) worker() {
	for {
		select {
		case <-r.stop:
			return
		case id := <-r.queue:
			r.execute(id)
		}
	}
}

func (r *Runner) sweeper() {
	// First sweep runs immediately: rows created before this executor was
	// ready (or whose Enqueue raced arbitration) are picked up within
	// moments instead of waiting a full tick.
	sweep := func() {
		ctx, cancel := context.WithTimeout(r.buildCtx, 10*time.Second)
		defer cancel()
		queued, err := r.cfg.BuildStore.ListQueued(ctx)
		if err != nil {
			if ctx.Err() == nil {
				r.logger.Error("sweeper list queued builds", "error", err)
			}
			return
		}
		for _, id := range queued {
			r.Enqueue(id)
		}
	}
	sweep()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			sweep()
		}
	}
}

// execute claims and runs one build under the executor's single slot. The
// process-global semaphore plus the executor role together guarantee one
// build at a time, so the shared theme working directories are never touched
// concurrently.
func (r *Runner) execute(id uuid.UUID) {
	select {
	case r.sem <- struct{}{}:
	case <-r.stop:
		return
	}
	defer func() { <-r.sem }()

	// The build context carries Stop cancellation; the timeout bounds it.
	ctx, cancel := context.WithTimeout(r.buildCtx, r.cfg.BuildTimeout)
	defer cancel()

	build, err := r.cfg.BuildStore.Get(ctx, id)
	if err != nil {
		r.logger.Error("load build", "buildId", id, "error", err)
		return
	}
	if build.Status != model.BuildStatusQueued {
		return
	}
	claimed, err := r.cfg.BuildStore.MarkRunning(ctx, id)
	if err != nil {
		r.logger.Error("claim build", "buildId", id, "error", err)
		return
	}
	if !claimed {
		return
	}
	if err := r.runBuild(ctx, build); err != nil {
		failure := model.BuildFailure{Stage: "internal", Message: err.Error()}
		var be *buildError
		if errors.As(err, &be) {
			failure = be.failure
		}
		// Persist against a fresh context: the run context may be cancelled.
		if fErr := r.cfg.BuildStore.MarkFailed(context.Background(), id, failure); fErr != nil {
			r.logger.Error("persist build failure", "buildId", id, "error", fErr)
		}
		r.logger.Warn("build failed", "buildId", id, "stage", failure.Stage, "message", failure.Message)
		return
	}
}

// buildError carries a terminal, client-safe failure into MarkFailed. Its
// message never contains server paths.
type buildError struct {
	failure model.BuildFailure
}

func (e *buildError) Error() string {
	return fmt.Sprintf("%s: %s", e.failure.Stage, e.failure.Message)
}

func fail(stage, message string) error {
	return &buildError{failure: model.BuildFailure{Stage: stage, Message: message}}
}

// runBuild executes the full trusted sequence for one claimed build row.
// Every writing step happens inside the task's private working copy of the
// registered theme root (taskDir/theme): snapshot preparation, dependency
// caches resolved from the repository tree, and build output all stay there,
// and the registered source is read-only from first byte to last. The copy
// carries a full per-file source manifest, so the recorded source identity
// covers the whole tree, not just theme.json.
func (r *Runner) runBuild(ctx context.Context, build model.Build) error {
	theme, err := r.cfg.ThemeStore.GetByRowID(ctx, build.ThemeRowID)
	if err != nil {
		return fail("theme-lookup", "The theme registration used by this build is no longer available.")
	}
	if hex.EncodeToString(theme.ManifestDigest) != hex.EncodeToString(build.ManifestDigest) {
		return fail("theme-changed", "The registered theme changed after this build was requested; trigger a new build.")
	}
	if err := r.checkRequirements(theme); err != nil {
		return err
	}

	// Resolve and verify the on-disk theme source against the registered
	// identity: disk bytes must still hash to the stored manifest digest and
	// the declared configuration schema must match. Comparing database rows
	// alone cannot see a source tree edited after registration.
	themeRoot, err := filepath.EvalSymlinks(theme.SourcePath)
	if err != nil || dirMissing(themeRoot) {
		return fail("theme-root", "The registered theme root is no longer present on disk.")
	}
	if err := verifyThemeSource(themeRoot, theme); err != nil {
		return fail("theme-source-changed", "The theme source on disk no longer matches its registered identity; re-register the theme.")
	}

	taskDir := filepath.Join(r.cfg.BuildsRoot, build.ID.String())
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return fail("workspace", "Failed to create the build workspace.")
	}
	if err := r.writeInput(taskDir, "site-overrides.json", build.SiteSettings); err != nil {
		return fail("workspace", "Failed to stage the site settings snapshot.")
	}
	if err := r.writeInput(taskDir, "theme-config.json", build.ThemeConfig); err != nil {
		return fail("workspace", "Failed to stage the theme configuration.")
	}

	// Materialize the task's private source copy inside a workspace layout
	// that anchors dependency resolution at the registered source, and
	// record its full per-file provenance before any command runs.
	wsRoot := filepath.Join(taskDir, "ws")
	themeCopy := filepath.Join(wsRoot, "site")
	manifest, err := copyThemeSource(themeCopy, themeRoot)
	if err != nil {
		r.logger.Error("task source copy failed", "buildId", build.ID, "themeRoot", themeRoot, "error", err)
		return fail("workspace", "Failed to materialize the task source copy.")
	}
	depRoot := FindWorkspaceRoot(themeRoot)
	if err := assembleWorkspace(wsRoot, themeRoot, depRoot); err != nil {
		return fail("workspace", "Failed to anchor the task dependency references.")
	}
	if err := writeSourceManifest(taskDir, manifest); err != nil {
		return fail("workspace", "Failed to record the task source manifest.")
	}
	if err := r.cfg.BuildStore.UpdateSourceDigest(ctx, build.ID, manifest.Aggregate, manifest.FileCount); err != nil {
		r.logger.Error("record source digest", "buildId", build.ID, "error", err)
	}

	// The theme's own Node flow prepares the snapshot (digests, permissions,
	// contract self-checks) inside the task copy; the database-saved settings
	// and configuration ride along as JSON files, and the authority contract
	// schemas are handed in as an absolute path so the copy stays
	// self-contained. Failure keeps the working copy for inspection.
	prepareEnv := []string{"FUYAO_THEME_CONTRACTS=" + filepath.Join(r.cfg.RepoRoot, "contracts", "theme")}
	seedFavicon := func() error { return nil }
	if r.cfg.Mode == "production" {
		if r.cfg.ContentRoots == nil {
			return fail("prepare", "Production builds require per-site content roots; configure FUYAO_ADMIN_SITES_ROOT.")
		}
		contentRoot, configRoot, faviconRoot, err := r.cfg.ContentRoots(build.SiteID)
		if err != nil {
			return fail("prepare", "Failed to resolve the site's content roots.")
		}
		// A fresh site's favicon directory is empty; production builds seed
		// it from the theme's own default set (the task's verified copy, so
		// the bytes match the recorded source manifest) before prepare runs,
		// letting the theme's favicon validation pass on first build.
		seedFavicon = func() error {
			if serr := seedFaviconRoot(faviconRoot, filepath.Join(themeCopy, "static", "favicon")); serr != nil {
				r.logger.Error("favicon seeding failed", "buildId", build.ID, "faviconRoot", faviconRoot, "error", serr)
				return serr
			}
			return nil
		}
		prepareEnv = append(prepareEnv,
			"FUYAO_CONTENT_ROOT="+contentRoot,
			"FUYAO_CONFIG_ROOT="+configRoot,
			"FUYAO_FAVICON_ROOT="+faviconRoot,
			// The script's production lock defaults to a server-only path;
			// each platform task is an isolated workspace, so the lock
			// belongs inside it.
			"FUYAO_DEPLOY_LOCK="+filepath.Join(taskDir, "deploy.lock"))
	}
	runPrepare := func() (string, error) {
		return r.runCommand(ctx, build, "prepare", themeCopy,
			[]string{"node", "$THEME/scripts/prepare-build-inputs.js",
				"--mode=" + r.cfg.Mode,
				"--site-overrides=" + filepath.Join(taskDir, "site-overrides.json"),
				"--theme-config=" + filepath.Join(taskDir, "theme-config.json")},
			prepareEnv)
	}
	var prepareLog string
	var err2 error
	if r.cfg.AlbumLock != nil && r.cfg.Mode == "production" {
		// The production prepare regenerates album metadata; hold the same
		// album lock the media indexer and human editors use so a build
		// never reads half-written metadata nor overwrites a concurrent
		// human edit.
		err2 = r.cfg.AlbumLock(build.SiteID, func() error {
			if serr := seedFavicon(); serr != nil {
				return serr
			}
			var inner error
			prepareLog, inner = runPrepare()
			return inner
		})
		err = err2
	} else {
		if serr := seedFavicon(); serr != nil {
			return fail("prepare", "Failed to seed the site favicon directory from the theme defaults.")
		}
		prepareLog, err = runPrepare()
	}
	if err != nil {
		return r.commandFailure("prepare", prepareLog, err)
	}
	snapshotWork := filepath.Join(themeCopy, ".fuyao", "build-inputs", "current")
	snapshotDir := filepath.Join(taskDir, "snapshot")
	if err := SealTree(snapshotWork, snapshotDir); err != nil {
		return fail("snapshot", "Failed to seal the build snapshot into its task directory.")
	}

	// Build CLIs (vite and friends) resolve from the source's real install:
	// the workspace root's and the source's node_modules/.bin go first on
	// PATH, ahead of whatever the task directory's own ancestry would offer.
	binPaths := []string{
		filepath.Join(depRoot, "node_modules", ".bin"),
		filepath.Join(themeRoot, "node_modules", ".bin"),
	}
	pathValue, _ := os.LookupEnv("PATH")
	buildEnv := []string{"FUYAO_FROZEN_INPUT=" + snapshotDir, "FUYAO_SKIP_LOCK=1",
		"FUYAO_THEME_CONTRACTS=" + filepath.Join(r.cfg.RepoRoot, "contracts", "theme")}
	for _, bin := range binPaths {
		if dirExists(bin) {
			pathValue = bin + string(os.PathListSeparator) + pathValue
		}
	}
	buildEnv = append(buildEnv, "PATH="+pathValue)

	buildLog, err := r.runCommand(ctx, build, "build", themeCopy,
		[]string{"npm", "run", "build"}, buildEnv)
	if err != nil {
		return r.commandFailure("build", buildLog, err)
	}

	buildOut := filepath.Join(themeCopy, "build")
	artifacts := filepath.Join(taskDir, "artifacts")
	if err := SealTree(buildOut, artifacts); err != nil {
		return fail("harvest", "No build output was produced to harvest.")
	}

	digests, err := readInputDigests(snapshotDir)
	if err != nil {
		return fail("harvest", "The snapshot input metadata is missing or unreadable.")
	}
	if err := r.cfg.BuildStore.MarkSucceeded(ctx, build.ID, artifacts, digests); err != nil {
		return fmt.Errorf("persist success: %w", err)
	}
	// The working copy has served its purpose; the sealed snapshot and
	// artifacts are the durable record. Removing it keeps task directories
	// from growing by a full source tree each.
	if err := MakeTreeWritable(themeCopy); err == nil {
		_ = os.RemoveAll(themeCopy)
	}
	r.logger.Info("build succeeded", "buildId", build.ID, "theme", theme.ThemeID+"@"+theme.Version,
		"sourceFiles", manifest.FileCount)
	return nil
}

// verifyThemeSource hashes the on-disk theme.json and, when declared, the
// configuration schema document, and compares them to the registered catalog
// row. A source tree edited after registration fails the build instead of
// silently becoming this task's input.
func verifyThemeSource(themeRoot string, theme model.Theme) error {
	manifestBytes, err := os.ReadFile(filepath.Join(themeRoot, "theme.json"))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(manifestBytes)
	if hex.EncodeToString(digest[:]) != hex.EncodeToString(theme.ManifestDigest) {
		return errors.New("theme.json digest drift")
	}
	view, err := model.ParseThemeManifest(manifestBytes)
	if err != nil {
		return err
	}
	if view.ConfigSchema != nil {
		schemaBytes, err := os.ReadFile(filepath.Join(themeRoot, filepath.FromSlash(view.ConfigSchema.File)))
		if err != nil {
			return err
		}
		if !jsonEqualValue(schemaBytes, theme.ConfigSchema) {
			return errors.New("config schema drift")
		}
	}
	return nil
}

// jsonEqualValue compares two JSON documents by parsed value; storage
// (jsonb) re-serializes documents, so byte equality is too strict.
func jsonEqualValue(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(ab) == string(bb)
}

// checkRequirements blocks builds whose theme cannot serve the site's real
// capabilities and URL conventions. The site's needs come from trusted server
// configuration, never from the theme's own declaration.
func (r *Runner) checkRequirements(theme model.Theme) error {
	view, err := model.ParseThemeManifest(theme.Manifest)
	if err != nil {
		return fail("theme-manifest", "The stored theme manifest is unreadable.")
	}
	have := map[string]bool{}
	for _, c := range view.Capabilities {
		have[c] = true
	}
	var missingCaps []string
	for _, want := range r.cfg.Requirements.Capabilities {
		if !have[want] {
			missingCaps = append(missingCaps, want)
		}
	}
	haveURL := map[string]bool{}
	for _, c := range view.Outputs.URLConventions {
		haveURL[c] = true
	}
	var missingURL []string
	for _, want := range r.cfg.Requirements.URLConventions {
		if !haveURL[want] {
			missingURL = append(missingURL, want)
		}
	}
	if len(missingCaps) == 0 && len(missingURL) == 0 {
		return nil
	}
	return &buildError{failure: model.BuildFailure{
		Stage:                 "capability-check",
		Message:               "The theme cannot serve this site's required capabilities or URL conventions.",
		MissingCapabilities:   missingCaps,
		MissingURLConventions: missingURL,
	}}
}

// readInputDigests extracts the three input digests recorded by the Node flow.
func readInputDigests(snapshotDir string) (json.RawMessage, error) {
	data, err := os.ReadFile(filepath.Join(snapshotDir, "private", "input.json"))
	if err != nil {
		return nil, err
	}
	var input struct {
		Inputs json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, err
	}
	if len(input.Inputs) == 0 {
		return nil, errors.New("input.json carries no digests")
	}
	return input.Inputs, nil
}

// NewPreviewToken issues the random URL token for a preview link.
func NewPreviewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
