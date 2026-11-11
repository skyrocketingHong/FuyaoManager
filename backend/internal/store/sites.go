package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
)

// SiteStore persists sites, their settings, and memberships. Settings updates
// compare the expected revision inside the transaction that bumps it, which
// backs the strong-ETag / If-Match contract. The lifecycle columns track the
// directory provisioning that turns a database row into a usable site.
type SiteStore struct {
	pool *pgxpool.Pool
}

// SiteRecord is a site joined with its settings and the caller's role.
type SiteRecord struct {
	Site     model.Site
	Settings model.SiteSettings
	Role     string
}

const siteJoinColumns = `si.id, si.slug, si.visibility, si.provision_state, si.provision_failure, si.created_at, si.updated_at,
	ss.site_id, ss.name, ss.description, ss.default_locale, ss.revision, ss.updated_at`

// NewSiteParams carries the fields of a site creation request. Visibility
// defaults to private; provisionState starts as provisioning until the
// platform finishes the on-disk setup.
type NewSiteParams struct {
	Slug          string
	Name          string
	Description   string
	DefaultLocale string
	Visibility    string
}

func scanSiteJoin(row pgx.Row, role *string) (SiteRecord, error) {
	var rec SiteRecord
	st := &rec.Settings
	site := &rec.Site
	var err error
	if role != nil {
		err = row.Scan(&site.ID, &site.Slug, &site.Visibility, &site.ProvisionState, &site.ProvisionFailure,
			&site.CreatedAt, &site.UpdatedAt,
			&st.SiteID, &st.Name, &st.Description, &st.DefaultLocale, &st.Revision, &st.UpdatedAt,
			role)
	} else {
		err = row.Scan(&site.ID, &site.Slug, &site.Visibility, &site.ProvisionState, &site.ProvisionFailure,
			&site.CreatedAt, &site.UpdatedAt,
			&st.SiteID, &st.Name, &st.Description, &st.DefaultLocale, &st.Revision, &st.UpdatedAt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteRecord{}, ErrNotFound
	}
	if err != nil {
		return SiteRecord{}, err
	}
	if role != nil {
		rec.Role = *role
	}
	return rec, nil
}

// Create inserts a site with initial settings and the owner membership in one
// transaction. The row starts in the provisioning state.
func (s *SiteStore) Create(ctx context.Context, params NewSiteParams, owner uuid.UUID) (SiteRecord, error) {
	id := newUUID()
	visibility := params.Visibility
	if visibility == "" {
		visibility = model.SiteVisibilityPrivate
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SiteRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO sites (id, slug, visibility) VALUES ($1, $2, $3)`, id, params.Slug, visibility); err != nil {
		return SiteRecord{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO site_settings (site_id, name, description, default_locale) VALUES ($1, $2, $3, $4)`,
		id, params.Name, params.Description, params.DefaultLocale); err != nil {
		return SiteRecord{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'owner')`,
		id, owner); err != nil {
		return SiteRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SiteRecord{}, err
	}
	return s.GetForUser(ctx, id, owner)
}

// UpdateVisibility switches a site's public/private visibility. The caller is
// responsible for re-applying the edge configuration so the entry binding
// actually follows.
func (s *SiteStore) UpdateVisibility(ctx context.Context, siteID uuid.UUID, visibility string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sites SET visibility = $2, updated_at = now() WHERE id = $1`, siteID, visibility)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkProvisioned transitions the provisioning state. An empty failure marks
// the site ready; a non-empty failure records the exact stage that broke (and
// never downgrades an already-ready site).
func (s *SiteStore) MarkProvisioned(ctx context.Context, siteID uuid.UUID, failure model.ProvisionFailure) error {
	var tag pgconn.CommandTag
	var err error
	if failure.Stage == "" {
		tag, err = s.pool.Exec(ctx,
			`UPDATE sites SET provision_state = 'ready', provision_failure = NULL, updated_at = now()
			 WHERE id = $1`, siteID)
	} else {
		tag, err = s.pool.Exec(ctx,
			`UPDATE sites SET provision_state = 'failed', provision_failure = $2, updated_at = now()
			 WHERE id = $1 AND provision_state <> 'ready'`, siteID, failure)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListForUser returns every site the user is a member of, newest first.
func (s *SiteStore) ListForUser(ctx context.Context, userID uuid.UUID) ([]SiteRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+siteJoinColumns+`, m.role
		 FROM sites si
		 JOIN site_settings ss ON ss.site_id = si.id
		 JOIN site_memberships m ON m.site_id = si.id AND m.user_id = $1
		 ORDER BY si.created_at DESC, si.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SiteRecord
	for rows.Next() {
		var rec SiteRecord
		if err := rows.Scan(&rec.Site.ID, &rec.Site.Slug, &rec.Site.Visibility, &rec.Site.ProvisionState,
			&rec.Site.ProvisionFailure, &rec.Site.CreatedAt, &rec.Site.UpdatedAt,
			&rec.Settings.SiteID, &rec.Settings.Name, &rec.Settings.Description,
			&rec.Settings.DefaultLocale, &rec.Settings.Revision, &rec.Settings.UpdatedAt,
			&rec.Role); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetForUser returns one site with the caller's role. Non-members get
// ErrNotFound so site existence is not leaked across tenants.
func (s *SiteStore) GetForUser(ctx context.Context, siteID, userID uuid.UUID) (SiteRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+siteJoinColumns+`, m.role
		 FROM sites si
		 JOIN site_settings ss ON ss.site_id = si.id
		 JOIN site_memberships m ON m.site_id = si.id AND m.user_id = $2
		 WHERE si.id = $1`, siteID, userID)
	var role string
	rec, err := scanSiteJoin(row, &role)
	return rec, err
}

// UpdateSettings applies a partial settings update guarded by the expected
// revision. expectedRevision <= 0 means "current revision" (If-Match: *).
// The comparison and the revision bump happen inside one transaction with the
// row locked, so two racing writers cannot both succeed.
func (s *SiteStore) UpdateSettings(ctx context.Context, siteID uuid.UUID, expectedRevision int64, name, description, defaultLocale *string) (model.SiteSettings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.SiteSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current int64
	err = tx.QueryRow(ctx, `SELECT revision FROM site_settings WHERE site_id = $1 FOR UPDATE`, siteID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.SiteSettings{}, ErrNotFound
	}
	if err != nil {
		return model.SiteSettings{}, err
	}
	if expectedRevision > 0 && current != expectedRevision {
		return model.SiteSettings{}, ErrConflict
	}

	var updated model.SiteSettings
	err = tx.QueryRow(ctx,
		`UPDATE site_settings
		 SET name = COALESCE($2, name),
		     description = COALESCE($3, description),
		     default_locale = COALESCE($4, default_locale),
		     revision = revision + 1,
		     updated_at = now()
		 WHERE site_id = $1
		 RETURNING site_id, name, description, default_locale, revision, updated_at`,
		siteID, name, description, defaultLocale,
	).Scan(&updated.SiteID, &updated.Name, &updated.Description, &updated.DefaultLocale, &updated.Revision, &updated.UpdatedAt)
	if err != nil {
		return model.SiteSettings{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE sites SET updated_at = now() WHERE id = $1`, siteID); err != nil {
		return model.SiteSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.SiteSettings{}, err
	}
	return updated, nil
}
