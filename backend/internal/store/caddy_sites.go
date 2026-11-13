package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

type CaddySite struct {
	SiteID     uuid.UUID       `json:"siteId"`
	SourceKey  string          `json:"sourceKey"`
	Kind       string          `json:"kind"`
	Definition json.RawMessage `json:"definition"`
	Present    bool            `json:"present"`
	SyncedAt   time.Time       `json:"syncedAt"`
}

// SyncCaddySites registers real routes without provisioning directories,
// selecting themes or changing the running Caddy configuration.
func (s *Store) SyncCaddySites(ctx context.Context, operator uuid.UUID, entries []caddyconfig.Site) (int, error) {
	if len(entries) == 0 {
		return 0, fmt.Errorf("no discoverable Caddy sites")
	}
	tx, err := s.beginAccountLifecycle(ctx, operator)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE caddy_sites SET present=false`); err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		protected := false
		for _, host := range entry.Hosts {
			if strings.EqualFold(host, "manager.fuyaoskyrocket.ing") || strings.HasSuffix(strings.ToLower(host), ".manager.fuyaoskyrocket.ing") {
				protected = true
			}
		}
		if protected {
			continue
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `SELECT site_id FROM caddy_sites WHERE source_key=$1`, entry.ID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			id = newUUID()
			name := strings.Join(entry.Hosts, ", ")
			if name == "" {
				name = "Caddy " + strings.Join(entry.Listen, ", ")
			}
			if len([]rune(name)) > 200 {
				name = string([]rune(name)[:200])
			}
			if _, err = tx.Exec(ctx, `INSERT INTO sites(id,slug,visibility,provision_state) VALUES($1,$2,'private','ready')`, id, "caddy-"+id.String()); err != nil {
				return 0, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO site_settings(site_id,name,default_locale) VALUES($1,$2,'zh-CN')`, id, name); err != nil {
				return 0, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO site_memberships(site_id,user_id,role) VALUES($1,$2,'owner')`, id, operator); err != nil {
				return 0, err
			}
		} else if err != nil {
			return 0, err
		}
		definition, err := json.Marshal(entry)
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO caddy_sites(site_id,source_key,kind,definition) VALUES($1,$2,$3,$4)
   ON CONFLICT(source_key) DO UPDATE SET kind=EXCLUDED.kind,definition=EXCLUDED.definition,present=true,synced_at=now()`, id, entry.ID, entry.Kind, definition)
		if err != nil {
			return 0, err
		}
		count++
	}
	if err = commitAudited(ctx, tx, operator, operator, "syncCaddySites"); err != nil {
		return 0, err
	}
	return count, nil
}
func (s *Store) GetCaddySite(ctx context.Context, id, user uuid.UUID) (CaddySite, error) {
	var item CaddySite
	err := s.Pool.QueryRow(ctx, `SELECT c.site_id,c.source_key,c.kind,c.definition,c.present,c.synced_at
 FROM caddy_sites c JOIN site_memberships m ON m.site_id=c.site_id WHERE c.site_id=$1 AND m.user_id=$2`, id, user).Scan(&item.SiteID, &item.SourceKey, &item.Kind, &item.Definition, &item.Present, &item.SyncedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return item, err
}
