-- Platform roles and account lifecycle for the management surface.
-- Compatibility: every account that existed before this migration is the
-- bootstrap administrator, so the backfill grants platform_role='admin' to
-- all pending/active rows; accounts created afterwards default to 'member'
-- (the store layer inserts the value explicitly).
ALTER TABLE users
    ADD COLUMN platform_role text NOT NULL DEFAULT 'member';

UPDATE users SET platform_role = 'admin';

-- Lifecycle extension: the CHECK constraint grows to disabled (deactivated,
-- sessions and codes revoked, login refused) and deleted (soft delete — the
-- row and its history stay, the account can never log in again). deleted_at
-- records when the soft delete happened.
ALTER TABLE users
    ADD COLUMN deleted_at timestamptz;

ALTER TABLE users DROP CONSTRAINT users_status_values;
ALTER TABLE users
    ADD CONSTRAINT users_status_values
    CHECK (status IN ('pending', 'active', 'disabled', 'deleted'));

ALTER TABLE users
    ADD CONSTRAINT users_platform_role_values
    CHECK (platform_role IN ('admin', 'member'));

CREATE INDEX users_platform_role_idx ON users(platform_role);
CREATE INDEX users_status_idx ON users(status);
