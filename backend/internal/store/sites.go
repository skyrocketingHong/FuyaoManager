package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
)

// SiteStore persists sites, their settings, and memberships. Settings updates
// compare the expected revision inside the transaction that bumps it, which
// backs the strong-ETag / If-Match contract.
type SiteStore struct {
	pool *pgxpool.Pool
}

// SiteRecord is a site joined with its settings and the caller's role.
type SiteRecord struct {
	Site     model.Site
	Settings model.SiteSettings
	Role     string
}

const siteJoinColumns = `si.id, si.slug, si.created_at, si.updated_at,
	ss.site_id, ss.name, ss.description, ss.default_locale, ss.revision, ss.updated_at`

func scanSiteJoin(row pgx.Row, role *string) (SiteRecord, error) {
	var rec SiteRecord
	st := &rec.Settings
	site := &rec.Site
	var err error
	if role != nil {
		err = row.Scan(&site.ID, &site.Slug, &site.CreatedAt, &site.UpdatedAt,
			&st.SiteID, &st.Name, &st.Description, &st.DefaultLocale, &st.Revision, &st.UpdatedAt,
			role)
	} else {
		err = row.Scan(&site.ID, &site.Slug, &site.CreatedAt, &site.UpdatedAt,
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
// transaction.
func (s *SiteStore) Create(ctx context.Context, slug, name, description, defaultLocale string, owner uuid.UUID) (SiteRecord, error) {
	id := newUUID()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SiteRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO sites (id, slug) VALUES ($1, $2)`, id, slug); err != nil {
		return SiteRecord{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO site_settings (site_id, name, description, default_locale) VALUES ($1, $2, $3, $4)`,
		id, name, description, defaultLocale); err != nil {
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
		if err := rows.Scan(&rec.Site.ID, &rec.Site.Slug, &rec.Site.CreatedAt, &rec.Site.UpdatedAt,
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
