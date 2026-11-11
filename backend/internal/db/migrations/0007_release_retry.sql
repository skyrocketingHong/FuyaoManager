-- 0007_release_retry.sql: allow retrying a publish whose attempt failed.
-- The previous UNIQUE(build_id) index counted failed rows, so a single
-- failed attempt permanently locked the build out of publishing. Failed
-- attempts stay in the history; the uniqueness now applies only to rows that
-- actually back an active release (live or superseded), so retrying a valid
-- build works while republishing an already-released build stays a conflict.
DROP INDEX site_releases_build;

CREATE UNIQUE INDEX site_releases_active_build
    ON site_releases (build_id)
    WHERE status <> 'failed';
