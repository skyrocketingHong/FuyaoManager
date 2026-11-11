-- 0005_build_source_digest.sql: record the full per-task source-copy
-- identity on each build row. source_digest aggregates every copied source
-- file (path + SHA-256, sorted); source_file_count records the copied tree
-- size. Together with manifest_digest (theme.json only) they distinguish
-- "the registered definition" from "the exact source bytes this build
-- consumed".
ALTER TABLE site_builds
    ADD COLUMN source_digest text,
    ADD COLUMN source_file_count bigint;
