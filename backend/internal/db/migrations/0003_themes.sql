-- 0003_themes: theme catalog and per-site theme selection.
--
-- themes holds one row per registered (theme_id, version) pair. The manifest
-- is stored verbatim as JSONB alongside the SHA-256 digest of the exact
-- theme.json bytes read at registration, so identical re-registration is
-- idempotent while a changed manifest under the same version is rejected.
-- source_path is the server-side trusted theme root consumed by the future
-- build executor; it is never exposed through the management API.
-- config_schema stores the theme's own JSON Schema (2020-12) document, or
-- NULL when the manifest declares no configSchema (only {} is valid then).

CREATE TABLE themes (
    id uuid PRIMARY KEY,
    theme_id text NOT NULL,
    version text NOT NULL,
    adapter text NOT NULL,
    manifest jsonb NOT NULL,
    manifest_digest bytea NOT NULL,
    config_schema jsonb,
    source_path text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT themes_identity UNIQUE (theme_id, version),
    CONSTRAINT themes_theme_id_format CHECK (theme_id ~ '^[a-z][a-z0-9-]*[a-z0-9]$'),
    CONSTRAINT themes_version_nonempty CHECK (char_length(version) BETWEEN 5 AND 64),
    CONSTRAINT themes_adapter_nonempty CHECK (char_length(adapter) BETWEEN 1 AND 64),
    CONSTRAINT themes_digest_size CHECK (octet_length(manifest_digest) = 32),
    CONSTRAINT themes_source_path_length CHECK (char_length(source_path) BETWEEN 1 AND 1024)
);

CREATE INDEX themes_theme_id_idx ON themes(theme_id);

-- site_theme_selections stores each site's selected theme registration and
-- its theme configuration. revision drives the strong-ETag / If-Match
-- contract independently of site_settings.revision, so editing site settings
-- never overwrites a concurrent theme selection and vice versa; revision 0 is
-- the implicit "no selection yet" state expressed only through the API ETag.

CREATE TABLE site_theme_selections (
    site_id uuid PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
    theme_id uuid NOT NULL REFERENCES themes(id),
    theme_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    revision bigint NOT NULL DEFAULT 1,
    selected_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT site_theme_selections_revision_positive CHECK (revision > 0)
);
