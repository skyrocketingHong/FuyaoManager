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
		 RETURNING m.id, m.site_id, m.kind, m.status, m.requested_by, m.created_at`,
	).Scan(&t.ID, &t.SiteID, &t.Kind, &t.Status, &t.RequestedBy, &t.CreatedAt)
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
		`INSERT INTO site_media_tasks (id, site_id, kind, requested_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, site_id, kind, status, requested_by, created_at`,
		id, siteID, kind, userID,
	).Scan(&t.ID, &t.SiteID, &t.Kind, &t.Status, &t.RequestedBy, &t.CreatedAt)
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
