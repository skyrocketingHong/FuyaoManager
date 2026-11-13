CREATE TABLE caddy_management (
 site_id uuid PRIMARY KEY REFERENCES caddy_sites(site_id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision > 0),
 applied_revision bigint,
 validated_revision bigint,
 source_digest text NOT NULL,
 validation_id text NOT NULL DEFAULT '',
 root_revision_id text NOT NULL DEFAULT '',
 settings jsonb NOT NULL CHECK(jsonb_typeof(settings)='object'),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE caddy_configuration_revisions (
 site_id uuid NOT NULL REFERENCES caddy_management(site_id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision > 0),
 settings jsonb NOT NULL CHECK(jsonb_typeof(settings)='object'),
 status text NOT NULL CHECK(status IN ('draft','validated','applied')),
 root_revision_id text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(site_id, revision)
);
