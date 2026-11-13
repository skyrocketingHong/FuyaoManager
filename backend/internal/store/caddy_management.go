package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrPreconditionFailed = errors.New("the resource revision changed")

type CaddyRevision struct {
	Revision       int64           `json:"revision"`
	Settings       json.RawMessage `json:"settings"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"createdAt"`
	RootRevisionID string          `json:"-"`
}

// Settings contain only the finite public policy fields accepted by the
// root-owned capability, never source text, filesystem paths or secrets.
type CaddyManagement struct {
	Revision          int64           `json:"revision"`
	AppliedRevision   *int64          `json:"appliedRevision"`
	ValidatedRevision *int64          `json:"validatedRevision"`
	SourceDigest      string          `json:"sourceDigest"`
	Settings          json.RawMessage `json:"settings"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	History           []CaddyRevision `json:"history"`
	ValidationID      string          `json:"-"`
	RootRevisionID    string          `json:"-"`
}

func CaddyETag(id uuid.UUID, m *CaddyManagement) string {
	var revision int64
	if m != nil {
		revision = m.Revision
	}
	return fmt.Sprintf(`"%s-caddy-r%d"`, id, revision)
}

type caddyReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readCaddyManagement(ctx context.Context, q caddyReader, id uuid.UUID) (*CaddyManagement, error) {
	var m CaddyManagement
	err := q.QueryRow(ctx, `SELECT revision,applied_revision,validated_revision,source_digest,settings,updated_at,validation_id,root_revision_id FROM caddy_management WHERE site_id=$1`, id).
		Scan(&m.Revision, &m.AppliedRevision, &m.ValidatedRevision, &m.SourceDigest, &m.Settings, &m.UpdatedAt, &m.ValidationID, &m.RootRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT revision,settings,status,created_at,root_revision_id FROM caddy_configuration_revisions WHERE site_id=$1 ORDER BY revision DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m.History = []CaddyRevision{}
	for rows.Next() {
		var r CaddyRevision
		if err = rows.Scan(&r.Revision, &r.Settings, &r.Status, &r.CreatedAt, &r.RootRevisionID); err != nil {
			return nil, err
		}
		m.History = append(m.History, r)
	}
	return &m, rows.Err()
}

func (s *Store) GetCaddyManagement(ctx context.Context, id uuid.UUID) (*CaddyManagement, error) {
	return readCaddyManagement(ctx, s.Pool, id)
}

// MutateCaddyManagement serializes draft changes and capability calls with
// ownership and administrator lifecycle changes. The callback cannot run
// before the current active administrator, site owner and ETag are checked.
func (s *Store) MutateCaddyManagement(ctx context.Context, actor, id uuid.UUID, etag, operation string,
	change func(CaddySite, *CaddyManagement) (*CaddyManagement, string, error)) (*CaddyManagement, error) {
	tx, err := s.beginAccountLifecycle(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var c CaddySite
	var role string
	err = tx.QueryRow(ctx, `SELECT c.site_id,c.source_key,c.kind,c.definition,c.present,c.synced_at,m.role
 FROM caddy_sites c JOIN site_memberships m ON m.site_id=c.site_id WHERE c.site_id=$1 AND m.user_id=$2 FOR UPDATE OF c,m`, id, actor).
		Scan(&c.SiteID, &c.SourceKey, &c.Kind, &c.Definition, &c.Present, &c.SyncedAt, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if role != "owner" {
		return nil, ErrOperatorForbidden
	}
	if !c.Present {
		return nil, ErrConflict
	}
	current, err := readCaddyManagement(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if etag != CaddyETag(id, current) {
		return nil, ErrPreconditionFailed
	}
	next, status, err := change(c, current)
	if err != nil {
		return nil, err
	}
	if next == nil || next.Revision < 1 || !json.Valid(next.Settings) {
		return nil, ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO caddy_management(site_id,revision,applied_revision,validated_revision,source_digest,settings,validation_id,root_revision_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(site_id) DO UPDATE SET revision=EXCLUDED.revision,applied_revision=EXCLUDED.applied_revision,
 validated_revision=EXCLUDED.validated_revision,source_digest=EXCLUDED.source_digest,settings=EXCLUDED.settings,validation_id=EXCLUDED.validation_id,root_revision_id=EXCLUDED.root_revision_id,updated_at=now()`,
		id, next.Revision, next.AppliedRevision, next.ValidatedRevision, next.SourceDigest, next.Settings, next.ValidationID, next.RootRevisionID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO caddy_configuration_revisions(site_id,revision,settings,status,root_revision_id) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(site_id,revision) DO UPDATE SET
 status=CASE WHEN caddy_configuration_revisions.status='applied' THEN 'applied' ELSE EXCLUDED.status END,
 root_revision_id=CASE WHEN EXCLUDED.status='applied' THEN EXCLUDED.root_revision_id ELSE caddy_configuration_revisions.root_revision_id END`, id, next.Revision, next.Settings, status, next.RootRevisionID)
	if err != nil {
		return nil, err
	}
	event := AuditEvent{ActorID: &actor, Operation: operation, ResourceID: id.String(), SiteID: &id, Status: 200}
	req := AuditRequestFrom(ctx)
	if req != nil {
		event = req.Event
		event.ActorID = &actor
		event.Operation = operation
		event.ResourceID = id.String()
		event.SiteID = &id
	}
	if event.ActorName == "" {
		if err = tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, actor).Scan(&event.ActorName); err != nil {
			return nil, err
		}
	}
	if err = appendAudit(ctx, tx, event); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if req != nil {
		req.Committed = true
	}
	return s.GetCaddyManagement(ctx, id)
}
