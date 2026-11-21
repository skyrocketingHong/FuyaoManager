package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// ThemeStore persists the theme catalog and per-site theme selections.
// Registration is idempotent per (theme id, version, manifest digest); a
// changed manifest under an already-registered version is a conflict.
// Selection updates compare the expected revision inside the transaction that
// bumps it, mirroring the site-settings ETag contract on an independent
// counter so the two edit paths cannot overwrite each other.
type ThemeStore struct {
	pool *pgxpool.Pool
}

const themeColumns = `id, theme_id, version, adapter, manifest, manifest_digest, config_schema, source_path, created_at, updated_at`

func scanThemeRow(scan func(dest ...any) error) (model.Theme, error) {
	var t model.Theme
	err := scan(&t.ID, &t.ThemeID, &t.Version, &t.Adapter, &t.Manifest, &t.ManifestDigest,
		&t.ConfigSchema, &t.SourcePath, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Theme{}, ErrNotFound
	}
	if err != nil {
		return model.Theme{}, err
	}
	return t, nil
}

// Register inserts a catalog row. Re-registering the same (theme id, version)
// is idempotent only when the whole registration identity matches: manifest
// digest and configuration schema document, including the declared/omitted
// distinction. A matching re-registration refreshes the source path and
// updated_at and reports created=false; any identity change is ErrConflict,
// never a silent adoption of the old definition. The caller is expected to
// have validated the manifest and config schema already.
func (s *ThemeStore) Register(ctx context.Context, theme model.Theme) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`INSERT INTO themes (id, theme_id, version, adapter, manifest, manifest_digest, config_schema, source_path)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (theme_id, version) DO NOTHING`,
		theme.ID, theme.ThemeID, theme.Version, theme.Adapter, []byte(theme.Manifest), theme.ManifestDigest,
		[]byte(theme.ConfigSchema), theme.SourcePath)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, tx.Commit(ctx)
	}

	var existingDigest, existingSchema []byte
	err = tx.QueryRow(ctx,
		`SELECT manifest_digest, config_schema FROM themes WHERE theme_id = $1 AND version = $2`,
		theme.ThemeID, theme.Version).Scan(&existingDigest, &existingSchema)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(existingDigest, theme.ManifestDigest) || !jsonEqualValue(existingSchema, theme.ConfigSchema) {
		return false, ErrConflict
	}
	if _, err := tx.Exec(ctx,
		`UPDATE themes SET source_path = $3, updated_at = now() WHERE theme_id = $1 AND version = $2`,
		theme.ThemeID, theme.Version, theme.SourcePath); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

// jsonEqualValue compares two JSON documents by value, not bytes: the
// jsonb column re-serializes documents (key order, whitespace) on storage,
// so a byte comparison would reject identical re-registrations. Both nil
// (omitted schema) and empty documents compare equal only to each other.
func jsonEqualValue(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// List returns the whole catalog ordered by theme id and version.
func (s *ThemeStore) List(ctx context.Context) ([]model.Theme, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+themeColumns+` FROM themes ORDER BY theme_id, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Theme
	for rows.Next() {
		t, err := scanThemeRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get returns one catalog row by theme id and version.
func (s *ThemeStore) Get(ctx context.Context, themeID, version string) (model.Theme, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+themeColumns+` FROM themes WHERE theme_id = $1 AND version = $2`, themeID, version)
	return scanThemeRow(row.Scan)
}

// GetByRowID returns one catalog row by its surrogate key.
func (s *ThemeStore) GetByRowID(ctx context.Context, id uuid.UUID) (model.Theme, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+themeColumns+` FROM themes WHERE id = $1`, id)
	return scanThemeRow(row.Scan)
}

// SelectionRecord is a site's selection joined with the caller's access view:
// the membership role and the selected catalog row (zero Theme and nil
// Selection when no selection exists).
type SelectionRecord struct {
	Role      string
	Selection *model.SiteThemeSelection
	Theme     model.Theme
}

// GetSelectionForUser returns the caller's view of a site's theme selection.
// Non-members get ErrNotFound so site existence is not leaked across tenants.
func (s *ThemeStore) GetSelectionForUser(ctx context.Context, siteID, userID uuid.UUID) (SelectionRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT m.role, sel.theme_id, sel.theme_config, sel.revision, sel.selected_by, sel.created_at, sel.updated_at
		 FROM sites si
		 JOIN site_memberships m ON m.site_id = si.id AND m.user_id = $2
		 LEFT JOIN site_theme_selections sel ON sel.site_id = si.id
		 WHERE si.id = $1`, siteID, userID)

	var rec SelectionRecord
	var sel struct {
		ThemeRowID  *uuid.UUID
		ThemeConfig []byte
		Revision    *int64
		SelectedBy  *uuid.UUID
		CreatedAt   *time.Time
		UpdatedAt   *time.Time
	}
	err := row.Scan(&rec.Role, &sel.ThemeRowID, &sel.ThemeConfig, &sel.Revision, &sel.SelectedBy,
		&sel.CreatedAt, &sel.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SelectionRecord{}, ErrNotFound
	}
	if err != nil {
		return SelectionRecord{}, err
	}
	if sel.ThemeRowID == nil {
		return rec, nil
	}
	theme, err := s.GetByRowID(ctx, *sel.ThemeRowID)
	if err != nil {
		return SelectionRecord{}, err
	}
	rec.Theme = theme
	rec.Selection = &model.SiteThemeSelection{
		SiteID:      siteID,
		ThemeID:     *sel.ThemeRowID,
		ThemeConfig: append(json.RawMessage(nil), sel.ThemeConfig...),
		Revision:    *sel.Revision,
		SelectedBy:  *sel.SelectedBy,
		CreatedAt:   derefTime(sel.CreatedAt),
		UpdatedAt:   derefTime(sel.UpdatedAt),
	}
	return rec, nil
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// SaveSelection creates or replaces a site's theme selection guarded by the
// expected revision. expectedRevision 0 means "no selection yet" (the t0
// ETag); any other stale value yields ErrConflict. The referenced theme row
// must exist; its FK violation surfaces as ErrNotFound.
func (s *ThemeStore) SaveSelection(ctx context.Context, siteID, themeRowID, userID uuid.UUID, themeConfig []byte, expectedRevision int64) (model.SiteThemeSelection, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.SiteThemeSelection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current int64
	err = tx.QueryRow(ctx,
		`SELECT revision FROM site_theme_selections WHERE site_id = $1 FOR UPDATE`, siteID).Scan(&current)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if expectedRevision != 0 {
			return model.SiteThemeSelection{}, ErrConflict
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO site_theme_selections (site_id, theme_id, theme_config, revision, selected_by)
			 VALUES ($1, $2, $3, 1, $4)`,
			siteID, themeRowID, themeConfig, userID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				// A concurrent writer created the selection first: the t0
				// precondition is stale. An unknown theme row is a not-found.
				switch pgErr.Code {
				case "23505":
					return model.SiteThemeSelection{}, ErrConflict
				case "23503":
					return model.SiteThemeSelection{}, ErrNotFound
				}
			}
			return model.SiteThemeSelection{}, err
		}
	case err != nil:
		return model.SiteThemeSelection{}, err
	default:
		if current != expectedRevision {
			return model.SiteThemeSelection{}, ErrConflict
		}
		if _, err := tx.Exec(ctx,
			`UPDATE site_theme_selections
			 SET theme_id = $2, theme_config = $3, revision = revision + 1, selected_by = $4, updated_at = now()
			 WHERE site_id = $1`,
			siteID, themeRowID, themeConfig, userID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				return model.SiteThemeSelection{}, ErrNotFound
			}
			return model.SiteThemeSelection{}, err
		}
	}

	var updated model.SiteThemeSelection
	err = tx.QueryRow(ctx,
		`SELECT revision, created_at, updated_at FROM site_theme_selections WHERE site_id = $1`,
		siteID).Scan(&updated.Revision, &updated.CreatedAt, &updated.UpdatedAt)
	if err != nil {
		return model.SiteThemeSelection{}, err
	}
	updated.SiteID = siteID
	updated.ThemeID = themeRowID
	updated.ThemeConfig = append([]byte(nil), themeConfig...)
	updated.SelectedBy = userID
	if _, err := tx.Exec(ctx, `UPDATE sites SET updated_at = now() WHERE id = $1`, siteID); err != nil {
		return model.SiteThemeSelection{}, err
	}
	return updated, tx.Commit(ctx)
}

// RegisterImportedTheme commits prepared GitHub packages without repointing a
// version that a site may already depend on. The operator can be revoked while
// a download runs, so authorization is rechecked at catalog commit.
func (s *Store) RegisterImportedTheme(ctx context.Context, actor uuid.UUID, theme model.Theme) error {
	tx, err := s.beginAccountLifecycle(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	tag, err := tx.Exec(ctx, `INSERT INTO themes
		(id, theme_id, version, adapter, manifest, manifest_digest, config_schema, source_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (theme_id, version) DO NOTHING`,
		newUUID(), theme.ThemeID, theme.Version, theme.Adapter, []byte(theme.Manifest), theme.ManifestDigest,
		[]byte(theme.ConfigSchema), theme.SourcePath)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var digest, schema []byte
		var path string
		if err := tx.QueryRow(ctx, `SELECT manifest_digest,config_schema,source_path FROM themes WHERE theme_id=$1 AND version=$2`,
			theme.ThemeID, theme.Version).Scan(&digest, &schema, &path); err != nil {
			return err
		}
		if path != theme.SourcePath || !bytes.Equal(digest, theme.ManifestDigest) || !jsonEqualValue(schema, theme.ConfigSchema) {
			return ErrConflict
		}
	}
	return tx.Commit(ctx)
}
