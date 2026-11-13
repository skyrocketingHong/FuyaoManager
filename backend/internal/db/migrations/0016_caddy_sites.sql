CREATE TABLE caddy_sites (
 site_id uuid PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
 source_key text NOT NULL UNIQUE,
 kind text NOT NULL CHECK(kind IN ('static','redirect','reverse_proxy','unsupported')),
 definition jsonb NOT NULL CHECK(jsonb_typeof(definition)='object'),
 present boolean NOT NULL DEFAULT true,
 synced_at timestamptz NOT NULL DEFAULT now()
);
