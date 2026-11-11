-- Site lifecycle: access visibility and directory provisioning state.
-- visibility gates the site's public entry (Caddy mapping); a site is only
-- "ready" once the platform finished provisioning its content/config
-- directory tree — a successful database insert alone is not site creation.
ALTER TABLE sites
    ADD COLUMN visibility text NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('public', 'private')),
    ADD COLUMN provision_state text NOT NULL DEFAULT 'provisioning'
        CHECK (provision_state IN ('provisioning', 'ready', 'failed')),
    ADD COLUMN provision_failure jsonb;
