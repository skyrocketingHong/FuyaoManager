-- 0012: passkey binding restoration.
--
-- passkeys.rp_id records the relying-party ID a credential was registered
-- under. Historical rows predate this column; the only RP ID that ever
-- served a ceremony before this migration is "localhost" (IP-literal
-- deployments answered 503 on the passkey routes and produced no
-- credentials), so the backfill is verifiable and no historical credential
-- is deleted.
ALTER TABLE passkeys ADD COLUMN rp_id text NOT NULL DEFAULT '';
UPDATE passkeys SET rp_id = 'localhost' WHERE rp_id = '';

-- sessions.kind distinguishes full business sessions from short-lived
-- restricted registration sessions. Accounts without any credential must
-- not keep pre-existing full sessions that would bypass the binding
-- requirement, so their live sessions are downgraded here.
ALTER TABLE sessions ADD COLUMN kind text NOT NULL DEFAULT 'full';
ALTER TABLE sessions ADD CONSTRAINT sessions_kind_values CHECK (kind IN ('full', 'restricted'));
UPDATE sessions SET kind = 'restricted'
 WHERE user_id IN (
     SELECT u.id FROM users u
     WHERE NOT EXISTS (SELECT 1 FROM passkeys p WHERE p.user_id = u.id)
 );

-- auth_states gains the "bind" ceremony kind and an optional owning session
-- so a registration challenge is bound to the exact restricted session that
-- requested it.
ALTER TABLE auth_states DROP CONSTRAINT auth_states_kind_values;
ALTER TABLE auth_states ADD CONSTRAINT auth_states_kind_values CHECK (kind IN ('register', 'login', 'bind'));
ALTER TABLE auth_states ADD COLUMN session_id uuid REFERENCES sessions(id) ON DELETE CASCADE;
