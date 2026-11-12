package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// BuildStore persists build-preview tasks. Creation freezes the version view
// (settings + selection + theme identity) inside one transaction; the executor
// then only mutates status/failure/artifact columns.
type BuildStore struct {
	pool *pgxpool.Pool
}

const buildColumns = `b.id, b.site_id, b.requested_by, b.theme_row_id, b.theme_id, b.theme_version,
	b.manifest_digest, b.theme_config, b.site_settings, b.site_settings_revision,
	b.theme_selection_revision, b.status, b.failure, b.artifact_path, b.input_digests,
	b.source_digest, b.source_file_count, b.created_at, b.started_at, b.finished_at`

func scanBuildRow(scan func(dest ...any) error) (model.Build, error) {
	var b model.Build
	var failure []byte
	var artifactPath *string
	var sourceDigest *string
	var sourceFileCount *int64
	err := scan(&b.ID, &b.SiteID, &b.RequestedBy, &b.ThemeRowID, &b.ThemeID, &b.ThemeVersion,
		&b.ManifestDigest, &b.ThemeConfig, &b.SiteSettings, &b.SiteSettingsRevision,
		&b.ThemeSelectionRevision, &b.Status, &failure, &artifactPath, &b.InputDigests,
		&sourceDigest, &sourceFileCount, &b.CreatedAt, &b.StartedAt, &b.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Build{}, ErrNotFound
	}
	if err != nil {
		return model.Build{}, err
	}
	if artifactPath != nil {
		b.ArtifactPath = *artifactPath
	}
	if sourceDigest != nil {
		b.SourceDigest = *sourceDigest
	}
	if sourceFileCount != nil {
		b.SourceFileCount = *sourceFileCount
	}
	if len(failure) > 0 {
		var f model.BuildFailure
		if err := json.Unmarshal(failure, &f); err != nil {
			return model.Build{}, err
		}
		b.Failure = &f
	}
	return b, nil
}

// ErrNoThemeSelection marks a build request for a site that has not selected a
// theme yet.
var ErrNoThemeSelection = errors.New("site has no theme selection")

// CreateFromSiteSnapshot opens one transaction, reads the site settings and
// theme selection together, and inserts the queued build row carrying that
// exact version view. Role is the requester's membership role (also checked by
// the handler); it is re-verified inside the transaction via the join.
func (s *BuildStore) CreateFromSiteSnapshot(ctx context.Context, siteID, userID uuid.UUID) (model.Build, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Build{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One deterministic version view: both rows read under the same
	// transaction with row locks so a concurrent selection or settings save
	// serializes against build creation.
	var settings model.SiteSettings
	err = tx.QueryRow(ctx,
		`SELECT name, description, default_locale, revision
		 FROM site_settings WHERE site_id = $1 FOR SHARE`, siteID).
		Scan(&settings.Name, &settings.Description, &settings.DefaultLocale, &settings.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Build{}, ErrNotFound
	}
	if err != nil {
		return model.Build{}, err
	}

	var (
		themeRowID   uuid.UUID
		themeConfig  []byte
		selRevision  int64
		themeID      string
		themeVersion string
		manifestDig  []byte
	)
	err = tx.QueryRow(ctx,
		`SELECT sel.theme_id, sel.theme_config, sel.revision, t.theme_id, t.version, t.manifest_digest
		 FROM site_theme_selections sel
		 JOIN themes t ON t.id = sel.theme_id
		 WHERE sel.site_id = $1 FOR SHARE OF sel, t`, siteID).
		Scan(&themeRowID, &themeConfig, &selRevision, &themeID, &themeVersion, &manifestDig)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Build{}, ErrNoThemeSelection
	}
	if err != nil {
		return model.Build{}, err
	}

	settingsJSON, err := json.Marshal(model.SiteSettingsSnapshot{
		Site: model.SiteSettingsOverlay{
			Name:          settings.Name,
			Description:   settings.Description,
			DefaultLocale: settings.DefaultLocale,
		},
	})
	if err != nil {
		return model.Build{}, err
	}

	build := model.Build{
		ID:                     uuid.New(),
		SiteID:                 siteID,
		RequestedBy:            userID,
		ThemeRowID:             themeRowID,
		ThemeID:                themeID,
		ThemeVersion:           themeVersion,
		ManifestDigest:         manifestDig,
		ThemeConfig:            append(json.RawMessage(nil), themeConfig...),
		SiteSettings:           settingsJSON,
		SiteSettingsRevision:   settings.Revision,
		ThemeSelectionRevision: selRevision,
		Status:                 model.BuildStatusQueued,
		CreatedAt:              time.Now().UTC(),
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO site_builds (id, site_id, requested_by, theme_row_id, theme_id, theme_version,
			manifest_digest, theme_config, site_settings, site_settings_revision, theme_selection_revision, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		build.ID, build.SiteID, build.RequestedBy, build.ThemeRowID, build.ThemeID, build.ThemeVersion,
		build.ManifestDigest, []byte(build.ThemeConfig), []byte(build.SiteSettings),
		build.SiteSettingsRevision, build.ThemeSelectionRevision, build.Status)
	if err != nil {
		return model.Build{}, err
	}
	return build, tx.Commit(ctx)
}

// membershipJoin filters reads to site members; non-members get ErrNotFound so
// site existence is not leaked across tenants.
const membershipJoin = ` JOIN site_memberships m ON m.site_id = b.site_id AND m.user_id = $2 `

// GetForUser returns one build row visible to the caller.
func (s *BuildStore) GetForUser(ctx context.Context, buildID, siteID, userID uuid.UUID) (model.Build, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+buildColumns+` FROM site_builds b`+membershipJoin+
			` WHERE b.id = $1 AND b.site_id = $3`, buildID, userID, siteID)
	return scanBuildRow(row.Scan)
}

// ListForUser returns the caller's visible builds for one site, newest first.
func (s *BuildStore) ListForUser(ctx context.Context, siteID, userID uuid.UUID) ([]model.Build, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+buildColumns+` FROM site_builds b`+membershipJoin+
			` WHERE b.site_id = $1 ORDER BY b.created_at DESC`, siteID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Build
	for rows.Next() {
		b, err := scanBuildRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MarkRunning transitions queued -> running; it reports false when the row is
// no longer queued (already claimed, recovered, or deleted).
func (s *BuildStore) MarkRunning(ctx context.Context, buildID uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE site_builds SET status = 'running', started_at = now()
		 WHERE id = $1 AND status = 'queued'`, buildID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// MarkSucceeded records success with the artifact path and input digests.
func (s *BuildStore) MarkSucceeded(ctx context.Context, buildID uuid.UUID, artifactPath string, inputDigests json.RawMessage) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE site_builds SET status = 'succeeded', artifact_path = $2, input_digests = $3,
			finished_at = now() WHERE id = $1`, buildID, artifactPath, []byte(inputDigests))
	return err
}

// MarkFailed records the terminal failure; running-or-queued rows only.
func (s *BuildStore) MarkFailed(ctx context.Context, buildID uuid.UUID, failure model.BuildFailure) error {
	failureJSON, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE site_builds SET status = 'failed', failure = $2, finished_at = now()
		 WHERE id = $1 AND status IN ('queued','running')`, buildID, failureJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// RecoverInterrupted marks every running task failed: an in-process executor
// cannot survive a restart, so a row still running at startup means the
// process died mid-build. Queued rows stay queued for re-enqueuement.
func (s *BuildStore) RecoverInterrupted(ctx context.Context) (int64, error) {
	failure, err := json.Marshal(model.BuildFailure{
		Stage:   "interrupted",
		Message: "The server restarted while this build was running; trigger a new build.",
	})
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE site_builds SET status = 'failed', failure = $1, finished_at = now()
		 WHERE status = 'running'`, failure)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListQueued returns build ids still queued (used by startup re-enqueue and
// the periodic sweeper).
func (s *BuildStore) ListQueued(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM site_builds WHERE status = 'queued' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetArtifactPath returns the stored artifact directory of a succeeded build.
func (s *BuildStore) GetArtifactPath(ctx context.Context, buildID uuid.UUID) (string, error) {
	var path string
	err := s.pool.QueryRow(ctx,
		`SELECT artifact_path FROM site_builds WHERE id = $1 AND status = 'succeeded'`, buildID).
		Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return path, err
}

// GetPreviewTarget resolves a succeeded build's artifact directory and the
// theme's declared SPA fallback file for the preview server.
func (s *BuildStore) GetPreviewTarget(ctx context.Context, buildID uuid.UUID) (string, string, error) {
	var artifactPath string
	var manifest []byte
	err := s.pool.QueryRow(ctx,
		`SELECT b.artifact_path, t.manifest FROM site_builds b
		 JOIN themes t ON t.id = b.theme_row_id
		 WHERE b.id = $1 AND b.status = 'succeeded'`, buildID).
		Scan(&artifactPath, &manifest)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	view, err := model.ParseThemeManifest(manifest)
	if err != nil {
		return "", "", err
	}
	spaFallback := ""
	if view.Outputs.SpaFallback != nil {
		spaFallback = *view.Outputs.SpaFallback
	}
	return artifactPath, spaFallback, nil
}

// Get returns one build row regardless of membership; executor-side use only.
func (s *BuildStore) Get(ctx context.Context, buildID uuid.UUID) (model.Build, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+buildColumns+` FROM site_builds b WHERE b.id = $1`, buildID)
	return scanBuildRow(row.Scan)
}

// UpdateSourceDigest records the task's private source-copy identity: the
// aggregate digest over every copied file and the copied file count.
func (s *BuildStore) UpdateSourceDigest(ctx context.Context, buildID uuid.UUID, aggregateDigest string, fileCount int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE site_builds SET source_digest = $2, source_file_count = $3 WHERE id = $1`,
		buildID, aggregateDigest, fileCount)
	return err
}
