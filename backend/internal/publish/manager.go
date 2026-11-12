// Package publish implements the local release pipeline: it takes a
// succeeded build's frozen artifacts, validates the release against the theme
// contract (release metadata, required artifacts, capability and URL
// conventions), seals them into an immutable per-site release directory, and
// atomically switches the site's current release inside the per-site publish
// lock. Failures never change what is live; rollback re-points at an existing
// release without rebuilding.
package publish

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// ReleaseContract validates release.json documents against the authoritative
// contract; themecontract.Set implements it.
type ReleaseContract interface {
	ValidateRelease(data []byte) error
}

// Publish error stages, persisted in release failure records.
const (
	StageCapabilityCheck = "capability-check"
	StageArtifacts       = "artifacts"
	StageWorkspace       = "workspace"
	StageReleaseMetadata = "release-metadata"
	StagePersist         = "persist"
)

// EdgeHook couples the database switch to the serving edge (current pointer
// plus managed reverse-proxy entry). edge.Manager implements it; a nil hook
// keeps the purely database-driven behavior.
type EdgeHook interface {
	OnLive(ctx context.Context, siteID uuid.UUID, release model.Release) error
}

// Manager executes publishes and rollbacks against the releases root.
type Manager struct {
	pool         *pgxpool.Pool
	releases     *store.ReleaseStore
	builds       *store.BuildStore
	themes       *store.ThemeStore
	releasesRoot string
	contracts    ReleaseContract
	requirements buildrunner.SiteRequirements
	edge         EdgeHook
	logger       *slog.Logger
}

// Config assembles the manager. ReleasesRoot holds per-site release
// directories; Contracts validates release.json (a server without the theme
// contracts cannot publish and reports 503 at the API layer).
type Config struct {
	Pool         *pgxpool.Pool
	Releases     *store.ReleaseStore
	Builds       *store.BuildStore
	Themes       *store.ThemeStore
	ReleasesRoot string
	Contracts    ReleaseContract
	Requirements buildrunner.SiteRequirements
	Edge         EdgeHook
	Logger       *slog.Logger
}

// NewManager assembles the publish manager.
func NewManager(cfg Config) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		pool:         cfg.Pool,
		releases:     cfg.Releases,
		builds:       cfg.Builds,
		themes:       cfg.Themes,
		releasesRoot: cfg.ReleasesRoot,
		contracts:    cfg.Contracts,
		requirements: cfg.Requirements,
		edge:         cfg.Edge,
		logger:       logger,
	}
}

// publishError carries a terminal, client-safe failure.
type publishError struct {
	failure model.PublishFailure
}

func (e *publishError) Error() string {
	return fmt.Sprintf("%s: %s", e.failure.Stage, e.failure.Message)
}

func fail(stage, message string) error {
	return &publishError{failure: model.PublishFailure{Stage: stage, Message: message}}
}

// validReasons mirrors the release contract's reasons enum.
var validReasons = map[string]bool{
	"manual": true, "posts": true, "albums": true,
	"data": true, "favicon": true, "static": true,
}

// ErrNoContracts marks a server without the theme contracts loaded; the API
// layer maps it to 503.
var ErrNoContracts = errors.New("release contract validation is not configured")

// ErrBuildNotPublishable marks a build that cannot back a release (missing,
// not succeeded, wrong site, or already released).
var ErrBuildNotPublishable = errors.New("build cannot back a release")

// ErrBuildAlreadyReleased marks a publish of a build that already backs an
// active release row; it is a conflict, not a failure, and is never recorded
// as a failed attempt.
var ErrBuildAlreadyReleased = errors.New("build already backs an active release")

// Publish promotes a succeeded build to the site's live release. Steps:
// capability/URL gap check against the trusted site requirements, required
// artifact presence, release.json generation and contract validation, seal
// into ReleasesRoot/<siteID>/<releaseID>, then the locked pointer switch.
// A failed attempt is recorded with its failure and leaves the previous
// release live.
func (m *Manager) Publish(ctx context.Context, siteID, buildID, userID uuid.UUID, reasons []string) (model.Release, error) {
	if m.contracts == nil {
		return model.Release{}, ErrNoContracts
	}
	if len(reasons) == 0 {
		reasons = []string{"manual"}
	}
	for _, reason := range reasons {
		if !validReasons[reason] {
			return model.Release{}, fmt.Errorf("%w: unknown reason %q", ErrBuildNotPublishable, reason)
		}
	}

	build, err := m.builds.Get(ctx, buildID)
	if err != nil || build.SiteID != siteID || build.Status != model.BuildStatusSucceeded {
		return model.Release{}, ErrBuildNotPublishable
	}
	theme, err := m.themes.GetByRowID(ctx, build.ThemeRowID)
	if err != nil {
		return model.Release{}, ErrBuildNotPublishable
	}

	releaseID := build.ID // one release per build; the dir name equals the id
	release := model.Release{
		ID:             uuid.New(),
		SiteID:         siteID,
		BuildID:        build.ID,
		ReleaseDir:     releaseID.String(),
		ThemeID:        build.ThemeID,
		ThemeVersion:   build.ThemeVersion,
		ManifestDigest: build.ManifestDigest,
		InputDigests:   build.InputDigests,
		Reasons:        reasons,
		CreatedBy:      userID,
	}

	// recordFailure persists the failed attempt for history and returns the
	// record for the 422 response. When the persistence itself fails, no
	// phantom record is returned: the caller gets the original error and the
	// API answers with a plain problem instead of an unroutable DTO.
	recordFailure := func(err error) (model.Release, error) {
		var pe *publishError
		if errors.As(err, &pe) {
			failure := pe.failure
			release.Failure = &failure
		} else {
			release.Failure = &model.PublishFailure{Stage: StagePersist, Message: "The publish attempt failed."}
		}
		release.Status = model.ReleaseStatusFailed
		if fErr := m.releases.RecordFailed(context.Background(), release); fErr != nil {
			m.logger.Error("record failed publish", "siteId", siteID, "error", fErr)
			return model.Release{}, err
		}
		return release, err
	}

	// Capability and URL-convention gaps block the publish with the precise
	// missing items — the same trusted server-side requirements the build
	// runner enforces.
	if failure := checkRequirements(m.requirements, theme); failure != nil {
		return recordFailure(failure)
	}
	view, err := model.ParseThemeManifest(theme.Manifest)
	if err != nil {
		return recordFailure(fail(StageArtifacts, "The stored theme manifest is unreadable."))
	}
	release.Routes = model.ReleaseRoutes{
		SpaFallback:    view.Outputs.SpaFallback,
		URLConventions: view.Outputs.URLConventions,
	}

	// The theme's declared required artifacts must exist in the build output.
	var missing []string
	for _, artifact := range view.Outputs.RequiredArtifacts {
		if _, err := os.Stat(filepath.Join(build.ArtifactPath, filepath.FromSlash(artifact))); err != nil {
			missing = append(missing, artifact)
		}
	}
	if len(missing) > 0 {
		return recordFailure(&publishError{failure: model.PublishFailure{
			Stage:            StageArtifacts,
			Message:          "The build output is missing artifacts the theme requires for a release.",
			MissingArtifacts: missing,
		}})
	}

	// release.json is part of the immutable release product; generate and
	// validate it against the authoritative contract before sealing.
	digests, err := parseInputDigests(build.InputDigests)
	if err != nil {
		return recordFailure(fail(StageReleaseMetadata, "The build's input digests are missing or unreadable."))
	}
	releaseDoc := map[string]any{
		"contract":      "fuyao.theme.release/v1",
		"schemaVersion": 1,
		"releaseId":     releaseID.String(),
		"createdAt":     time.Now().UTC().Format(time.RFC3339),
		"reasons":       reasons,
		"theme": map[string]any{
			"id":             build.ThemeID,
			"version":        build.ThemeVersion,
			"manifestDigest": "sha256:" + hex.EncodeToString(build.ManifestDigest),
		},
		"inputs": digests,
		"routes": map[string]any{
			"spaFallback":    view.Outputs.SpaFallback,
			"urlConventions": view.Outputs.URLConventions,
		},
	}
	releaseJSON, err := json.MarshalIndent(releaseDoc, "", "  ")
	if err != nil {
		return recordFailure(fail(StageReleaseMetadata, "Failed to generate the release metadata."))
	}
	if err := m.contracts.ValidateRelease(releaseJSON); err != nil {
		m.logger.Error("release contract validation", "siteId", siteID, "error", err)
		return recordFailure(fail(StageReleaseMetadata, "The generated release metadata does not satisfy the release contract."))
	}

	// Seal into a per-attempt staging directory, never the shared final
	// directory: two concurrent publishes of the same build must not be able
	// to delete each other's sealed artifacts. The staging name carries this
	// attempt's release row id, so its owner is unambiguous and cleanup on
	// failure can only ever remove this attempt's own tree.
	siteRoot := filepath.Join(m.releasesRoot, siteID.String())
	stagingRoot := filepath.Join(siteRoot, ".staging")
	stagingDir := filepath.Join(stagingRoot, release.ID.String())
	finalDir := filepath.Join(siteRoot, releaseID.String())
	// 0755 so the reverse proxy (a separate user) can traverse into the
	// frozen, world-readable release directories; staged trees stay private.
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return recordFailure(fail(StageWorkspace, "Failed to create the release staging directory."))
	}
	if err := buildrunner.SealTree(build.ArtifactPath, stagingDir); err != nil {
		_ = cleanupDir(stagingDir)
		return recordFailure(fail(StageWorkspace, "Failed to seal the release artifacts."))
	}
	metaPath := filepath.Join(stagingDir, "release.json")
	if err := writeIntoFrozenTree(metaPath, releaseJSON); err != nil {
		m.logger.Error("write release.json", "siteId", siteID, "error", err)
		_ = cleanupDir(stagingDir)
		return recordFailure(fail(StageWorkspace, "Failed to write the release metadata."))
	}

	// Locked activation: read the base state, place the final directory,
	// insert live, supersede the previous live, re-point the site — one
	// transaction holding the per-site publish lock.
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		_ = cleanupDir(stagingDir)
		return recordFailure(fail(StagePersist, "Failed to open the publish transaction."))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := m.releases.AcquirePublishLock(ctx, tx, siteID); err != nil {
		_ = cleanupDir(stagingDir)
		return recordFailure(fail(StagePersist, "Failed to acquire the publish lock."))
	}

	// Republishing a build that already backs an active release is a
	// conflict, not a failure: the existing release keeps serving and no
	// failed row is recorded.
	released, err := m.releases.BuildHasActiveReleaseWithinTx(ctx, tx, siteID, build.ID)
	if err != nil {
		_ = cleanupDir(stagingDir)
		return recordFailure(fail(StagePersist, "Failed to check the build's release history."))
	}
	if released {
		_ = cleanupDir(stagingDir)
		return model.Release{}, ErrBuildAlreadyReleased
	}

	// Place the final directory under the lock. A leftover final directory
	// here is unreferenced by definition (an active row would have tripped
	// the check above; failed rows never reference a required directory), so
	// replacing it is safe. The staging tree moves with an atomic same-volume
	// rename; after the rename the result is committed to the database.
	if _, err := os.Stat(finalDir); err == nil {
		if err := cleanupDir(finalDir); err != nil {
			_ = cleanupDir(stagingDir)
			return recordFailure(fail(StageWorkspace, "Failed to replace an unreferenced release directory."))
		}
	}
	// Renaming a directory requires the directory itself to be writable on
	// some platforms (the ".." entry inside it is updated), so thaw the
	// staging root, rename, and re-freeze — contents stay frozen throughout,
	// and the whole exchange happens under the publish lock.
	if err := os.Chmod(stagingDir, 0o755); err != nil {
		m.logger.Error("thaw staging root", "siteId", siteID, "error", err)
		_ = cleanupDir(stagingDir)
		return recordFailure(fail(StageWorkspace, "Failed to move the release into place."))
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		m.logger.Error("move release into place", "siteId", siteID, "error", err)
		_ = cleanupDir(stagingDir)
		return recordFailure(fail(StageWorkspace, "Failed to move the release into place."))
	}
	if err := os.Chmod(finalDir, 0o555); err != nil {
		m.logger.Error("re-freeze release root", "siteId", siteID, "error", err)
		return recordFailure(fail(StageWorkspace, "Failed to freeze the release directory."))
	}

	if _, err := m.releases.ActivateWithinTx(ctx, tx, release); err != nil {
		_ = cleanupDir(stagingDir)
		if errors.Is(err, store.ErrConflict) {
			// The unique active-build index fired under the lock: another
			// row already backs this build. The outcome of this attempt is
			// uncertain from here — never delete a final directory that may
			// back a committed row; a later retry replaces it only under the
			// lock and only while unreferenced.
			return model.Release{}, ErrBuildAlreadyReleased
		}
		// Result of the switch is uncertain (not a conflict): the final
		// directory stays in place — a retry under the lock replaces it only
		// when still unreferenced; a committed row's directory is never
		// touched by cleanup.
		return recordFailure(fail(StagePersist, "Failed to switch the live release."))
	}
	if err := tx.Commit(ctx); err != nil {
		// Commit outcome uncertain: keep the final directory; never delete a
		// directory that may already back a committed release row.
		return recordFailure(fail(StagePersist, "Failed to commit the publish transaction."))
	}
	now := time.Now().UTC()
	release.Status = model.ReleaseStatusLive
	release.ActivatedAt = &now
	if m.edge != nil {
		// The database switch is committed; the edge (current pointer plus
		// managed entry) follows. A failure here is surfaced — the release is
		// live but the entry may lag — and Reconcile or the next publish
		// repairs it. The release itself stays live and rollback-able.
		if err := m.edge.OnLive(ctx, siteID, release); err != nil {
			m.logger.Error("edge apply after publish", "siteId", siteID, "releaseId", releaseID, "error", err)
			return release, fmt.Errorf("release is live but the site entry update failed: %w", err)
		}
	}
	m.logger.Info("release published", "siteId", siteID, "releaseId", releaseID,
		"theme", release.ThemeID+"@"+release.ThemeVersion)
	return release, nil
}

// Rollback re-points the site at an existing release. Artifacts are reused
// as-is (no rebuild) and nothing outside the pointer — settings, selections,
// builds, unpublished edits — is touched.
func (m *Manager) Rollback(ctx context.Context, siteID, releaseID, userID uuid.UUID) (model.Release, error) {
	target, err := m.releases.GetForUser(ctx, releaseID, siteID, userID)
	if err != nil {
		return model.Release{}, err
	}
	if target.Status == model.ReleaseStatusFailed {
		return model.Release{}, store.ErrReleaseNotRollbackable
	}
	// The release directory with its metadata must still be on disk.
	metaPath := filepath.Join(m.releasesRoot, siteID.String(), target.ReleaseDir, "release.json")
	if _, err := os.Stat(metaPath); err != nil {
		return model.Release{}, store.ErrReleaseNotRollbackable
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return model.Release{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rolled, err := m.releases.RollbackWithinTx(ctx, tx, siteID, releaseID)
	if err != nil {
		return model.Release{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Release{}, err
	}
	if m.edge != nil {
		if err := m.edge.OnLive(ctx, siteID, rolled); err != nil {
			m.logger.Error("edge apply after rollback", "siteId", siteID, "releaseId", releaseID, "error", err)
			return rolled, fmt.Errorf("rollback is live but the site entry update failed: %w", err)
		}
	}
	m.logger.Info("release rollback", "siteId", siteID, "releaseId", releaseID)
	return rolled, nil
}

// checkRequirements blocks publishes whose theme cannot serve the site's
// required capabilities and URL conventions; the requirements are trusted
// server configuration, never the theme's own declaration.
func checkRequirements(requirements buildrunner.SiteRequirements, theme model.Theme) error {
	view, err := model.ParseThemeManifest(theme.Manifest)
	if err != nil {
		return fail(StageCapabilityCheck, "The stored theme manifest is unreadable.")
	}
	have := map[string]bool{}
	for _, c := range view.Capabilities {
		have[c] = true
	}
	var missingCaps []string
	for _, want := range requirements.Capabilities {
		if !have[want] {
			missingCaps = append(missingCaps, want)
		}
	}
	haveURL := map[string]bool{}
	for _, c := range view.Outputs.URLConventions {
		haveURL[c] = true
	}
	var missingURL []string
	for _, want := range requirements.URLConventions {
		if !haveURL[want] {
			missingURL = append(missingURL, want)
		}
	}
	if len(missingCaps) == 0 && len(missingURL) == 0 {
		return nil
	}
	return &publishError{failure: model.PublishFailure{
		Stage:                 StageCapabilityCheck,
		Message:               "The theme cannot serve this site's required capabilities or URL conventions.",
		MissingCapabilities:   missingCaps,
		MissingURLConventions: missingURL,
	}}
}

// parseInputDigests extracts the three digests as the release contract's
// inputs block.
func parseInputDigests(raw json.RawMessage) (map[string]string, error) {
	var digests model.ReleaseInputDigests
	if err := json.Unmarshal(raw, &digests); err != nil {
		return nil, err
	}
	if digests.ContentDigest == "" || digests.SiteConfigDigest == "" || digests.ThemeConfigDigest == "" {
		return nil, errors.New("input digests incomplete")
	}
	return map[string]string{
		"contentDigest":     digests.ContentDigest,
		"siteConfigDigest":  digests.SiteConfigDigest,
		"themeConfigDigest": digests.ThemeConfigDigest,
	}, nil
}

// writeIntoFrozenTree creates the release metadata file inside a sealed
// read-only tree: unfreeze the containing directory, write the file frozen,
// re-freeze the directory.
func writeIntoFrozenTree(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o444); err != nil {
		return err
	}
	return os.Chmod(dir, 0o555)
}

// cleanupDir unfreezes and removes a partial release directory.
func cleanupDir(dir string) error {
	if err := buildrunner.MakeTreeWritable(dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
