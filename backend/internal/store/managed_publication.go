package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

type ManagedPublication struct {
	Tx                  pgx.Tx
	SiteID              uuid.UUID
	SourceKey           string
	CurrentReleaseID    uuid.UUID
	PublishedReleaseDir string
	Management          *CaddyManagement
}

// BeginManagedPublication takes the lifecycle lock before the publish and
// site-row locks. A non-Caddy site returns nil and keeps its legacy pipeline.
func (s *Store) BeginManagedPublication(ctx context.Context, siteID, actor uuid.UUID) (*ManagedPublication, error) {
	return s.beginManagedPublication(ctx, siteID, &actor)
}

// Recovery is a continuation of an already-authorized activation, not a new
// user write. It still serializes with account, Caddy and publish changes.
func (s *Store) BeginManagedPublicationRecovery(ctx context.Context, siteID uuid.UUID) (*ManagedPublication, error) {
	return s.beginManagedPublication(ctx, siteID, nil)
}
func (s *Store) beginManagedPublication(ctx context.Context, siteID uuid.UUID, actor *uuid.UUID) (*ManagedPublication, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = tx.Rollback(context.Background())
		}
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AccountLifecycleLockKey); err != nil {
		return nil, err
	}
	var present bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM caddy_sites WHERE site_id=$1)`, siteID).Scan(&present); err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	if err = s.Releases.AcquirePublishLock(ctx, tx, siteID); err != nil {
		return nil, err
	}
	state := &ManagedPublication{Tx: tx, SiteID: siteID}
	var current *uuid.UUID
	var kind string
	var definition []byte
	var themed bool
	err = tx.QueryRow(ctx, `SELECT s.current_release_id,c.source_key,c.kind,c.present,c.definition,
 EXISTS(SELECT 1 FROM site_theme_selections t WHERE t.site_id=s.id)
 FROM sites s JOIN caddy_sites c ON c.site_id=s.id WHERE s.id=$1 FOR UPDATE OF s,c`, siteID).
		Scan(&current, &state.SourceKey, &kind, &present, &definition, &themed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if actor != nil {
		var role, status, platformRole string
		err = tx.QueryRow(ctx, `SELECT m.role,u.status,u.platform_role FROM site_memberships m JOIN users u ON u.id=m.user_id WHERE m.site_id=$1 AND m.user_id=$2 FOR UPDATE OF m`, siteID, *actor).Scan(&role, &status, &platformRole)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if role != "owner" || status != "active" || platformRole != "admin" {
			return nil, ErrOperatorForbidden
		}
		var site caddyconfig.Site
		if !present || !themed || kind != "static" || json.Unmarshal(definition, &site) != nil || site.Root == "" || site.RootHasPlaceholder {
			return nil, ErrConflict
		}
	}
	if current != nil {
		state.CurrentReleaseID = *current
		if err = tx.QueryRow(ctx, `SELECT release_dir FROM site_releases WHERE id=$1 AND site_id=$2 AND status='live'`, *current, siteID).Scan(&state.PublishedReleaseDir); err != nil {
			return nil, err
		}
	}
	if state.Management, err = readCaddyManagement(ctx, tx, siteID); err != nil {
		return nil, err
	}
	if state.Management == nil {
		return nil, ErrConflict
	}
	keep = true
	return state, nil
}

func (s *Store) UpdateManagedPublicationRoot(ctx context.Context, state *ManagedPublication, rootID, digest string) error {
	if _, err := state.Tx.Exec(ctx, `UPDATE caddy_management SET root_revision_id=$2,source_digest=$3,validated_revision=NULL,validation_id='',updated_at=now() WHERE site_id=$1`, state.SiteID, rootID, digest); err != nil {
		return err
	}
	_, err := state.Tx.Exec(ctx, `UPDATE caddy_configuration_revisions r SET root_revision_id=$2 FROM caddy_management m WHERE m.site_id=$1 AND r.site_id=m.site_id AND r.revision=m.applied_revision`, state.SiteID, rootID)
	return err
}
func (s *Store) AuditManagedPublication(ctx context.Context, tx pgx.Tx, actor, siteID, releaseID uuid.UUID, operation string) error {
	event := AuditEvent{ActorID: &actor, Operation: operation, SiteID: &siteID, ResourceID: releaseID.String(), Status: 200}
	if request := AuditRequestFrom(ctx); request != nil {
		event = request.Event
		event.ActorID = &actor
		event.Operation = operation
		event.SiteID = &siteID
		event.ResourceID = releaseID.String()
	}
	if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, actor).Scan(&event.ActorName); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		event.ActorID = nil
		event.ActorName = "system"
	}
	return appendAudit(ctx, tx, event)
}
