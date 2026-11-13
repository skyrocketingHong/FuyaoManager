package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ContentSources struct {
	Posts      string `json:"posts"`
	Albums     string `json:"albums"`
	Footprints string `json:"footprints"`
	Friends    string `json:"friends"`
}

func IndependentSources() ContentSources {
	return ContentSources{"independent", "independent", "independent", "independent"}
}
func (s ContentSources) Valid() bool {
	for _, v := range []string{s.Posts, s.Albums, s.Footprints, s.Friends} {
		if v != "platform" && v != "independent" {
			return false
		}
	}
	return true
}
func (s ContentSources) Source(category string) string {
	switch category {
	case "posts":
		return s.Posts
	case "albums":
		return s.Albums
	case "footprints":
		return s.Footprints
	case "friends":
		return s.Friends
	}
	return "independent"
}

type ContentSourceRecord struct {
	Sources  ContentSources `json:"sources"`
	Revision int64          `json:"revision"`
}

func ContentSourcesETag(id uuid.UUID, revision int64) string {
	return fmt.Sprintf(`"%s-content-r%d"`, id, revision)
}
func (s *Store) GetContentSources(ctx context.Context, id uuid.UUID) (ContentSourceRecord, error) {
	return readContentSources(ctx, s.Pool, id)
}

type sourceReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readContentSources(ctx context.Context, q sourceReader, id uuid.UUID) (ContentSourceRecord, error) {
	result := ContentSourceRecord{Sources: IndependentSources()}
	var data []byte
	err := q.QueryRow(ctx, `SELECT sources,revision FROM site_content_sources WHERE site_id=$1`, id).Scan(&data, &result.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(data, &result.Sources); err != nil {
		return result, err
	}
	if !result.Sources.Valid() {
		return result, ErrConflict
	}
	return result, nil
}
func (s *Store) SetContentSources(ctx context.Context, id, actor uuid.UUID, etag string, sources ContentSources) (ContentSourceRecord, error) {
	if !sources.Valid() {
		return ContentSourceRecord{}, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ContentSourceRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AccountLifecycleLockKey); err != nil {
		return ContentSourceRecord{}, err
	}
	var role, status, kind string
	var managed bool
	err = tx.QueryRow(ctx, `SELECT m.role,u.status,COALESCE(c.kind,'theme'),EXISTS(SELECT 1 FROM caddy_management cm WHERE cm.site_id=s.id) FROM site_memberships m JOIN users u ON u.id=m.user_id JOIN sites s ON s.id=m.site_id LEFT JOIN caddy_sites c ON c.site_id=s.id WHERE m.site_id=$1 AND m.user_id=$2 FOR UPDATE OF s,m`, id, actor).Scan(&role, &status, &kind, &managed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContentSourceRecord{}, ErrNotFound
	}
	if err != nil {
		return ContentSourceRecord{}, err
	}
	if role != "owner" || status != "active" {
		return ContentSourceRecord{}, ErrOperatorForbidden
	}
	if kind != "theme" && !(kind == "static" && managed) {
		return ContentSourceRecord{}, ErrConflict
	}
	current, err := readContentSources(ctx, tx, id)
	if err != nil {
		return current, err
	}
	if etag != ContentSourcesETag(id, current.Revision) {
		return current, ErrPreconditionFailed
	}
	data, _ := json.Marshal(sources)
	next := ContentSourceRecord{sources, current.Revision + 1}
	_, err = tx.Exec(ctx, `INSERT INTO site_content_sources(site_id,sources,revision) VALUES($1,$2,$3) ON CONFLICT(site_id) DO UPDATE SET sources=EXCLUDED.sources,revision=EXCLUDED.revision,updated_at=now()`, id, data, next.Revision)
	if err != nil {
		return current, err
	}
	event := AuditEvent{ActorID: &actor, SiteID: &id, ResourceID: id.String(), Operation: "setContentSources", Status: 200}
	req := AuditRequestFrom(ctx)
	if req != nil {
		event = req.Event
		event.SiteID = &id
		event.ResourceID = id.String()
		event.Operation = "setContentSources"
		event.Status = 200
	}
	if event.ActorName == "" {
		if err = tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, actor).Scan(&event.ActorName); err != nil {
			return current, err
		}
	}
	if err = appendAudit(ctx, tx, event); err != nil {
		return current, err
	}
	if err = tx.Commit(ctx); err != nil {
		return current, err
	}
	if req != nil {
		req.Committed = true
	}
	return next, nil
}
