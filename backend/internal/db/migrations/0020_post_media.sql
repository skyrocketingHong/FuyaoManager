CREATE TABLE post_media (
    scope_id uuid NOT NULL REFERENCES content_scopes(id) ON DELETE CASCADE,
    path text NOT NULL,
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp', 'image/gif')),
    content bytea NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 41943040),
    digest bytea NOT NULL CHECK (octet_length(digest) = 32),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (scope_id, path)
);
