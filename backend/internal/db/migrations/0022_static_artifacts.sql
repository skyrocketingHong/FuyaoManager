ALTER TABLE site_builds ADD COLUMN artifact_state text NOT NULL DEFAULT 'available'
    CHECK (artifact_state IN ('available', 'deleting', 'deleted'));
ALTER TABLE site_releases ADD COLUMN artifact_state text NOT NULL DEFAULT 'available'
    CHECK (artifact_state IN ('available', 'deleting', 'deleted'));

DROP INDEX site_releases_active_build;
CREATE UNIQUE INDEX site_releases_active_build ON site_releases (build_id)
    WHERE status <> 'failed' AND artifact_state <> 'deleted';
