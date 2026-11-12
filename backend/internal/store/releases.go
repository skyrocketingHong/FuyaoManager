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

// ReleaseStore persists per-site release records and switches the site's
// current-release pointer. Every pointer transition happens inside one
// transaction holding the per-site publish advisory lock, so concurrent
// publishes serialize and a failed attempt never changes what is live.
type ReleaseStore struct {
	pool *pgxpool.Pool
}

// ErrReleaseNotRollbackable marks a rollback request whose target cannot
// become live (missing artifacts or already live).
var ErrReleaseNotRollbackable = errors.New("release cannot become live")

const releaseColumns = `r.id, r.site_id, r.build_id, r.release_dir, r.theme_id, r.theme_version,
	r.manifest_digest, r.input_digests, r.routes, r.reasons, r.status, r.failure, r.created_by,
	r.created_at, r.activated_at, r.superseded_at`

func scanReleaseRow(scan func(dest ...any) error) (model.Release, error) {
	var r model.Release
	var failure []byte
	err := scan(&r.ID, &r.SiteID, &r.BuildID, &r.ReleaseDir, &r.ThemeID, &r.ThemeVersion,
		&r.ManifestDigest, &r.InputDigests, &r.Routes, &r.Reasons, &r.Status, &failure, &r.CreatedBy,
		&r.CreatedAt, &r.ActivatedAt, &r.SupersededAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Release{}, ErrNotFound
	}
	if err != nil {
		return model.Release{}, err
	}
	if len(failure) > 0 {
		var f model.PublishFailure
		if err := json.Unmarshal(failure, &f); err != nil {
			return model.Release{}, err
		}
		r.Failure = &f
	}
	return r, nil
}

// releaseMembershipJoin filters reads to site members; non-members get
// ErrNotFound so site existence is not leaked across tenants.
const releaseMembershipJoin = ` JOIN site_memberships m ON m.site_id = r.site_id AND m.user_id = $2 `

// AcquirePublishLock takes the per-site publish advisory lock on the
// transaction. Holders serialize the whole read-base-state → materialize →
// switch sequence; the lock releases with the transaction.
func (s *ReleaseStore) AcquirePublishLock(ctx context.Context, tx pgx.Tx, siteID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fuyao-publish:' || $1::text, 0))`, siteID)
	return err
}

// BuildHasActiveReleaseWithinTx reports, under the caller's publish lock,
// whether the build already backs a release row that is live or superseded.
// Failed attempts do not count: their history stays, but the build may be
// retried.
func (s *ReleaseStore) BuildHasActiveReleaseWithinTx(ctx context.Context, tx pgx.Tx, siteID, buildID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM site_releases WHERE site_id = $1 AND build_id = $2 AND status <> 'failed'
		)`, siteID, buildID).Scan(&exists)
	return exists, err
}

// ActivateWithinTx inserts the release row as live, supersedes the site's
// previous live release, and switches sites.current_release_id — all inside
// the caller's locked transaction. baseReleaseID reports what was live before
// (zero when this is the first release).
func (s *ReleaseStore) ActivateWithinTx(ctx context.Context, tx pgx.Tx, release model.Release) (baseReleaseID uuid.UUID, err error) {
	// Read the base state under the lock: what is live right now.
	err = tx.QueryRow(ctx,
		`SELECT current_release_id FROM sites WHERE id = $1 FOR UPDATE`, release.SiteID).
		Scan(&baseReleaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}

	routes, err := json.Marshal(release.Routes)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO site_releases (id, site_id, build_id, release_dir, theme_id, theme_version,
			manifest_digest, input_digests, routes, reasons, status, created_by, activated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'live',$11, now())`,
		release.ID, release.SiteID, release.BuildID, release.ReleaseDir, release.ThemeID,
		release.ThemeVersion, release.ManifestDigest, []byte(release.InputDigests), routes,
		release.Reasons, release.CreatedBy); err != nil {
		return uuid.Nil, err
	}
	if baseReleaseID != uuid.Nil {
		if _, err := tx.Exec(ctx,
			`UPDATE site_releases SET status = 'superseded', superseded_at = now()
			 WHERE id = $1 AND site_id = $2`, baseReleaseID, release.SiteID); err != nil {
			return uuid.Nil, err
		}
	}
	tag, err := tx.Exec(ctx,
		`UPDATE sites SET current_release_id = $2, updated_at = now() WHERE id = $1`,
		release.SiteID, release.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if tag.RowsAffected() == 0 {
		return uuid.Nil, ErrNotFound
	}
	return baseReleaseID, nil
}

// RecordFailed persists a failed publish attempt for audit; it never touches
// the current-release pointer.
func (s *ReleaseStore) RecordFailed(ctx context.Context, release model.Release) error {
	failure, err := json.Marshal(*release.Failure)
	if err != nil {
		return err
	}
	routes, err := json.Marshal(release.Routes)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO site_releases (id, site_id, build_id, release_dir, theme_id, theme_version,
			manifest_digest, input_digests, routes, reasons, status, failure, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'failed',$11,$12)`,
		release.ID, release.SiteID, release.BuildID, release.ReleaseDir, release.ThemeID,
		release.ThemeVersion, release.ManifestDigest, []byte(release.InputDigests), routes,
		release.Reasons, failure, release.CreatedBy)
	return err
}

// RollbackWithinTx atomically re-points the site at an existing release whose
// artifacts are still on disk (verified by the caller before this call).
// Returns ErrReleaseNotRollbackable when the target is already live.
func (s *ReleaseStore) RollbackWithinTx(ctx context.Context, tx pgx.Tx, siteID, releaseID uuid.UUID) (model.Release, error) {
	if err := s.AcquirePublishLock(ctx, tx, siteID); err != nil {
		return model.Release{}, err
	}

	var current uuid.UUID
	err := tx.QueryRow(ctx, `SELECT current_release_id FROM sites WHERE id = $1 FOR UPDATE`, siteID).
		Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Release{}, ErrNotFound
	}
	if err != nil {
		return model.Release{}, err
	}
	if current == releaseID {
		return model.Release{}, ErrReleaseNotRollbackable
	}

	// The target must exist on this site and not be a failed attempt.
	row := tx.QueryRow(ctx,
		`SELECT `+releaseColumns+` FROM site_releases r WHERE r.id = $1 AND r.site_id = $2 AND r.status <> 'failed'`,
		releaseID, siteID)
	target, err := scanReleaseRow(row.Scan)
	if err != nil {
		return model.Release{}, err
	}

	if current != uuid.Nil {
		if _, err := tx.Exec(ctx,
			`UPDATE site_releases SET status = 'superseded', superseded_at = now()
			 WHERE id = $1 AND site_id = $2`, current, siteID); err != nil {
			return model.Release{}, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE site_releases SET status = 'live', activated_at = now(), superseded_at = NULL
		 WHERE id = $1 AND site_id = $2`, releaseID, siteID); err != nil {
		return model.Release{}, err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE sites SET current_release_id = $2, updated_at = now() WHERE id = $1`,
		siteID, releaseID)
	if err != nil {
		return model.Release{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.Release{}, ErrNotFound
	}
	target.Status = model.ReleaseStatusLive
	now := time.Now().UTC()
	target.ActivatedAt = &now
	return target, nil
}

// GetForUser returns one release row visible to the caller.
func (s *ReleaseStore) GetForUser(ctx context.Context, releaseID, siteID, userID uuid.UUID) (model.Release, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+releaseColumns+` FROM site_releases r`+releaseMembershipJoin+
			` WHERE r.id = $1 AND r.site_id = $3`, releaseID, userID, siteID)
	return scanReleaseRow(row.Scan)
}

// ListForUser returns the caller's visible releases for one site, newest
// first.
func (s *ReleaseStore) ListForUser(ctx context.Context, siteID, userID uuid.UUID) ([]model.Release, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+releaseColumns+` FROM site_releases r`+releaseMembershipJoin+
			` WHERE r.site_id = $1 ORDER BY r.created_at DESC`, siteID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Release
	for rows.Next() {
		r, err := scanReleaseRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetCurrentForUser returns the site's live release for the caller, or
// ErrNotFound when the site has never published (or the caller is not a
// member).
func (s *ReleaseStore) GetCurrentForUser(ctx context.Context, siteID, userID uuid.UUID) (model.Release, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+releaseColumns+` FROM site_releases r`+releaseMembershipJoin+
			` JOIN sites s ON s.current_release_id = r.id
			 WHERE r.site_id = $1`, siteID, userID)
	return scanReleaseRow(row.Scan)
}

// ReleaseServingTarget resolves what a published origin serves right now: the
// live release's directory name and SPA fallback. The publish server resolves
// this per request, so publish/rollback switch content without restarting
// listeners.
type ReleaseServingTarget struct {
	ReleaseDir  string
	SpaFallback string
}

// GetCurrentTarget resolves the site's current serving target without
// membership filtering (server-side use by the publish origin).
func (s *ReleaseStore) GetCurrentTarget(ctx context.Context, siteID uuid.UUID) (ReleaseServingTarget, error) {
	var target ReleaseServingTarget
	var routes []byte
	err := s.pool.QueryRow(ctx,
		`SELECT r.release_dir, r.routes FROM site_releases r
		 JOIN sites s ON s.current_release_id = r.id
		 WHERE r.site_id = $1 AND r.status = 'live'`, siteID).
		Scan(&target.ReleaseDir, &routes)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReleaseServingTarget{}, ErrNotFound
	}
	if err != nil {
		return ReleaseServingTarget{}, err
	}
	var parsed model.ReleaseRoutes
	if err := json.Unmarshal(routes, &parsed); err != nil {
		return ReleaseServingTarget{}, err
	}
	if parsed.SpaFallback != nil {
		target.SpaFallback = *parsed.SpaFallback
	}
	return target, nil
}
