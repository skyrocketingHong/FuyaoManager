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

// MediaTaskStore persists asynchronous media processing tasks.
type MediaTaskStore struct {
	pool *pgxpool.Pool
}

// MediaTask is one persisted processing job.
type MediaTask struct {
	ID          uuid.UUID
	SiteID      uuid.UUID
	ThemeID     *uuid.UUID
	Kind        string
	Status      string
	Failure     *model.MediaTaskFailure
	RequestedBy uuid.UUID
	CreatedAt   time.Time
	FinishedAt  *time.Time
}

// ClaimNext atomically transitions the oldest queued task to running.
func (s *MediaTaskStore) ClaimNext(ctx context.Context) (MediaTask, bool, error) {
	var t MediaTask
	err := s.pool.QueryRow(ctx,
		`UPDATE site_media_tasks m
		 SET status = 'running'
		 WHERE m.id = (
			SELECT id FROM site_media_tasks WHERE status = 'queued'
			ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
		 )
		 RETURNING m.id, m.site_id, m.kind, m.status, m.requested_by, m.created_at, m.theme_id`,
	).Scan(&t.ID, &t.SiteID, &t.Kind, &t.Status, &t.RequestedBy, &t.CreatedAt, &t.ThemeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaTask{}, false, nil
	}
	if err != nil {
		return MediaTask{}, false, err
	}
	return t, true, nil
}

// MarkSucceeded records success.
func (s *MediaTaskStore) MarkSucceeded(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE site_media_tasks SET status = 'succeeded', finished_at = now() WHERE id = $1`, id)
	return err
}

// MarkFailed records the failure reason.
func (s *MediaTaskStore) MarkFailed(ctx context.Context, id uuid.UUID, failure model.MediaTaskFailure) error {
	raw, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE site_media_tasks SET status = 'failed', failure = $2, finished_at = now() WHERE id = $1`, id, raw)
	return err
}

// RecoverInterrupted marks running rows failed: an in-process worker cannot
// survive a restart.
func (s *MediaTaskStore) RecoverInterrupted(ctx context.Context) error {
	failure, _ := json.Marshal(model.MediaTaskFailure{
		Stage: "interrupted", Message: "The server restarted while this task was running; trigger it again.",
	})
	_, err := s.pool.Exec(ctx,
		`UPDATE site_media_tasks SET status = 'failed', failure = $1, finished_at = now()
		 WHERE status = 'running'`, failure)
	return err
}

// Create enqueues a task.
func (s *MediaTaskStore) Create(ctx context.Context, siteID, userID uuid.UUID, kind string) (MediaTask, error) {
	id := newUUID()
	var t MediaTask
	err := s.pool.QueryRow(ctx,
		`INSERT INTO site_media_tasks (id, site_id, kind, requested_by,theme_id)
		 VALUES ($1, $2, $3, $4,(SELECT theme_id FROM site_theme_selections WHERE site_id=$2))
		 RETURNING id, site_id, kind, status, requested_by, created_at,theme_id`,
		id, siteID, kind, userID,
	).Scan(&t.ID, &t.SiteID, &t.Kind, &t.Status, &t.RequestedBy, &t.CreatedAt, &t.ThemeID)
	if err != nil {
		return MediaTask{}, err
	}
	return t, nil
}

// ListForSite returns the site's tasks, newest first.
func (s *MediaTaskStore) ListForSite(ctx context.Context, siteID, userID uuid.UUID) ([]MediaTask, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.site_id, t.kind, t.status, t.failure, t.requested_by, t.created_at, t.finished_at
		 FROM site_media_tasks t
		 JOIN site_memberships m ON m.site_id = t.site_id AND m.user_id = $2
		 WHERE t.site_id = $1
		 ORDER BY t.created_at DESC, t.id
		 LIMIT 50`, siteID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaTask
	for rows.Next() {
		var t MediaTask
		if err := rows.Scan(&t.ID, &t.SiteID, &t.Kind, &t.Status, &t.Failure, &t.RequestedBy, &t.CreatedAt, &t.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreatePlatform freezes a trusted registered importer, independent of a
// site's theme selection. Both the actor and scope are rechecked in SQL.
func (s *MediaTaskStore) CreatePlatform(ctx context.Context, scopeID, userID, themeID uuid.UUID, kind string) (MediaTask, error) {
	var task MediaTask
	err := s.pool.QueryRow(ctx, `INSERT INTO site_media_tasks(id,site_id,kind,requested_by,theme_id)
 SELECT $1,c.id,$4,u.id,t.id FROM content_scopes c
 JOIN users u ON u.id=$3 AND u.status='active' AND u.platform_role='admin'
 JOIN themes t ON t.id=$5 AND t.adapter='node22-sveltekit-static' AND (t.manifest->'capabilities') ? 'albums'
 WHERE c.id=$2 AND c.kind='platform'
 RETURNING id,site_id,kind,status,requested_by,created_at,theme_id`, newUUID(), scopeID, userID, kind, themeID).
		Scan(&task.ID, &task.SiteID, &task.Kind, &task.Status, &task.RequestedBy, &task.CreatedAt, &task.ThemeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaTask{}, ErrOperatorForbidden
	}
	return task, err
}

func (s *MediaTaskStore) ListForContentScope(ctx context.Context, scopeID, userID uuid.UUID) ([]MediaTask, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id,t.site_id,t.kind,t.status,t.failure,t.requested_by,t.created_at,t.finished_at,t.theme_id
 FROM site_media_tasks t JOIN content_scopes c ON c.id=t.site_id JOIN users u ON u.id=$2 AND u.status='active'
 WHERE c.id=$1 AND (c.kind='platform' OR EXISTS (SELECT 1 FROM site_memberships m WHERE m.site_id=c.site_id AND m.user_id=$2))
 ORDER BY t.created_at DESC,t.id LIMIT 50`, scopeID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MediaTask{}
	for rows.Next() {
		var t MediaTask
		if err := rows.Scan(&t.ID, &t.SiteID, &t.Kind, &t.Status, &t.Failure, &t.RequestedBy, &t.CreatedAt, &t.FinishedAt, &t.ThemeID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
