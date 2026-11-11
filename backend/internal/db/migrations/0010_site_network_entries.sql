-- Platform-managed network entries: the IP:port pairs the platform assigns
-- to published sites (served by the managed Caddy edge) and to private build
-- previews (served by the management process itself). Ports live in one
-- operator-approved range, are unique across kinds, and persist across
-- restarts so a recycled OS port can never silently switch sites.
CREATE TABLE site_network_entries (
    id uuid PRIMARY KEY,
    site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('site', 'preview')),
    build_id uuid REFERENCES site_builds(id) ON DELETE CASCADE,
    port int NOT NULL CHECK (port BETWEEN 1024 AND 65535),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT site_network_entries_port_uniq UNIQUE (port),
    CONSTRAINT site_network_entries_kind_target CHECK (
        (kind = 'site' AND build_id IS NULL)
        OR (kind = 'preview' AND build_id IS NOT NULL)
    )
);

-- At most one edge entry per site and one preview entry per build.
CREATE UNIQUE INDEX site_network_entries_site_edge
    ON site_network_entries(site_id) WHERE kind = 'site';
CREATE UNIQUE INDEX site_network_entries_preview_build
    ON site_network_entries(build_id) WHERE kind = 'preview';

CREATE INDEX site_network_entries_site_idx ON site_network_entries(site_id);
