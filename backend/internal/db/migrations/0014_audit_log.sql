CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor_id uuid,
    actor_name text NOT NULL DEFAULT '',
    operation text NOT NULL CHECK (length(operation) BETWEEN 1 AND 100),
    resource_id text NOT NULL DEFAULT '',
    site_id uuid,
    method text NOT NULL DEFAULT '',
    route text NOT NULL DEFAULT '',
    peer_ip text NOT NULL DEFAULT '',
    status integer NOT NULL CHECK (status BETWEEN 100 AND 599),
    outcome text NOT NULL CHECK (outcome IN ('success', 'failure'))
);
CREATE INDEX audit_events_created_idx ON audit_events(created_at DESC, id DESC);
CREATE INDEX audit_events_actor_idx ON audit_events(actor_id, created_at DESC);
CREATE INDEX audit_events_site_idx ON audit_events(site_id, created_at DESC);
CREATE INDEX audit_events_operation_idx ON audit_events(operation, created_at DESC);
