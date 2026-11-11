-- One-time CLI-issued authentication codes (initialization and per-login).
-- Only the SHA-256 digest of the code is stored; consumption is a single
-- atomic UPDATE, so concurrent redemptions of the same code cannot both win.
CREATE TABLE auth_codes (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('init', 'login')),
    code_hash bytea NOT NULL UNIQUE,
    attempts int NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX auth_codes_user_idx ON auth_codes(user_id);
