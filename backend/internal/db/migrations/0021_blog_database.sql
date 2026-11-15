-- Blog content is owned by PostgreSQL. Legacy documents are retained in the
-- database for lossless frontmatter preservation and one-time import auditing.
CREATE TABLE blog_migration_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    complete boolean NOT NULL DEFAULT false
);
INSERT INTO blog_migration_state(singleton, complete) VALUES(true, false);
CREATE TABLE blog_scopes (
    scope_id uuid PRIMARY KEY REFERENCES content_scopes(id) ON DELETE CASCADE,
    imported_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE blog_posts (
    id uuid PRIMARY KEY,
    scope_id uuid NOT NULL REFERENCES blog_scopes(scope_id) ON DELETE CASCADE,
    slug text NOT NULL,
    legacy_path text NOT NULL,
    document jsonb NOT NULL,
    original bytea NOT NULL,
    digest text NOT NULL,
    UNIQUE (scope_id, slug),
    UNIQUE (scope_id, legacy_path)
);
CREATE TABLE blog_categories (
    scope_id uuid NOT NULL REFERENCES blog_scopes(scope_id) ON DELETE CASCADE,
    slug text NOT NULL,
    publication_path text NOT NULL,
    title text NOT NULL,
    aliases text[] NOT NULL DEFAULT '{}',
    original bytea NOT NULL DEFAULT ''::bytea,
    digest text NOT NULL,
    PRIMARY KEY (scope_id, slug),
    UNIQUE (scope_id, publication_path)
);
CREATE TABLE blog_vocabulary (
    scope_id uuid PRIMARY KEY REFERENCES blog_scopes(scope_id) ON DELETE CASCADE,
    document jsonb NOT NULL,
    digest text NOT NULL
);
