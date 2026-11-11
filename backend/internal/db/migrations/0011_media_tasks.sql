-- Asynchronous media processing tasks (album EXIF/thumbnail indexing).
-- Persisted so failures carry their reason and restarts recover running rows
-- as failed instead of leaving phantom tasks.
CREATE TABLE site_media_tasks (
    id uuid PRIMARY KEY,
    site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('album-index')),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    failure jsonb,
    requested_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

CREATE INDEX site_media_tasks_site ON site_media_tasks(site_id, created_at DESC);
