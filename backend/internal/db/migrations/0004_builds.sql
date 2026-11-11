-- 0004_builds.sql: persistent per-site build-preview tasks.
-- Each row freezes the exact version view it was created from: the site
-- settings snapshot and revision, the theme selection revision, and the
-- selected catalog identity (row id, theme id, version, manifest digest,
-- theme config). The executor consumes only these stored values, never a
-- re-read of the live selection.

CREATE TABLE site_builds (
    id                       uuid PRIMARY KEY,
    site_id                  uuid NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    requested_by             uuid NOT NULL REFERENCES users (id),
    theme_row_id             uuid NOT NULL REFERENCES themes (id),
    theme_id                 text NOT NULL,
    theme_version            text NOT NULL,
    manifest_digest          bytea NOT NULL,
    theme_config             jsonb NOT NULL,
    site_settings            jsonb NOT NULL,
    site_settings_revision   bigint NOT NULL,
    theme_selection_revision bigint NOT NULL,
    status                   text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    failure                  jsonb,
    artifact_path            text,
    input_digests            jsonb,
    created_at               timestamptz NOT NULL DEFAULT now(),
    started_at               timestamptz,
    finished_at              timestamptz
);

CREATE INDEX site_builds_site_created_idx ON site_builds (site_id, created_at DESC);
