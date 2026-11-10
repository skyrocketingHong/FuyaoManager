-- 0002_stable_csrf: replace the per-read rotating CSRF digest with a
-- stable per-session token so multiple tabs share one token. The token is
-- stored in the clear: leaking it does not enable CSRF on its own, since
-- cross-site requests carrying a custom header trigger a preflight that the
-- CORS policy rejects, and the session cookie itself remains hash-only.
--
-- Sessions created under schema 0001 have no stable token. They are removed
-- so their next request resolves as anonymous (GET /api/v1/session returns
-- authenticated:false) and the administrator simply signs in again; no v1
-- session can linger in the authenticated-but-unwritable state.

ALTER TABLE sessions ADD COLUMN csrf_token text NOT NULL DEFAULT '';

ALTER TABLE sessions DROP COLUMN csrf_hash;

DELETE FROM sessions;
