CREATE TABLE site_content_sources (
 site_id uuid PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
 sources jsonb NOT NULL DEFAULT '{"posts":"independent","albums":"independent","footprints":"independent","friends":"independent"}',
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(jsonb_typeof(sources)='object')
);
ALTER TABLE site_builds ADD COLUMN content_sources jsonb NOT NULL DEFAULT '{"posts":"independent","albums":"independent","footprints":"independent","friends":"independent"}';
