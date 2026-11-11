-- 0006_site_releases.sql: per-site release records and the atomic
-- current-release pointer. Each row is one publish attempt of a succeeded
-- build: the frozen theme identity (id, version, manifest digest), the three
-- input digests, the routing metadata the server serves with, the request
-- reasons, and — for failed attempts — the persisted failure record. The
-- sites.current_release_id pointer switches only inside the publish/rollback
-- transaction that also holds the per-site advisory lock, so a failed
-- publish never changes what is live.
CREATE TABLE site_releases (
    id uuid PRIMARY KEY,
    site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    build_id uuid NOT NULL REFERENCES site_builds(id),
    release_dir text NOT NULL,
    theme_id text NOT NULL,
    theme_version text NOT NULL,
    manifest_digest bytea NOT NULL,
    input_digests jsonb NOT NULL DEFAULT '{}'::jsonb,
    routes jsonb NOT NULL,
    reasons text[] NOT NULL,
    status text NOT NULL DEFAULT 'publishing',
    failure jsonb,
    created_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    activated_at timestamptz,
    superseded_at timestamptz,
    CONSTRAINT site_releases_status CHECK (status IN ('publishing', 'live', 'superseded', 'failed')),
    CONSTRAINT site_releases_dir_format CHECK (release_dir ~ '^[A-Za-z0-9._-]+$' AND release_dir !~ '^\.\.?$')
);

-- One release per build: republishing the same build is a rollback, not a
-- second row.
CREATE UNIQUE INDEX site_releases_build ON site_releases (build_id);
CREATE INDEX site_releases_site_created ON site_releases (site_id, created_at DESC);

ALTER TABLE sites
    ADD COLUMN current_release_id uuid REFERENCES site_releases(id) ON DELETE SET NULL;
