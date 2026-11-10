# Fuyao Management Backend (Local Development MVP)

English | [简体中文](./README_ZH.md)

The Go backend of the FuyaoHomepage content management system. This package is the first agile work package: a real PostgreSQL backend with passkey-only authentication (WebAuthn discoverable credentials) and the minimal business loop of site list, site detail, and site settings with optimistic concurrency. The admin front end is not part of this package.

## Status and Boundaries

Verified in this package:

- Real PostgreSQL persistence with SQL migrations (no in-memory or JSON substitutes).
- Real WebAuthn via [go-webauthn](https://github.com/go-webauthn/webauthn): registration with attestation verification, discoverable login with origin, RP ID hash, signature, user-verification, and signature-counter checks. No password, bearer, or development-admin login exists.
- First-run bootstrap through the local CLI with a short-lived, single-use, attempt-limited enrollment permit that only authorizes passkey enrollment. Enrollment completion is one transaction guarded by an advisory lock: permit validity (including expiry after the challenge was issued), first-administrator exclusivity, credential write, and user activation commit or roll back together, so concurrent first enrollments can never both succeed.
- Cookie sessions with server-side revocation, CSRF token enforcement, and strict Origin checking on unsafe methods.
- Membership-scoped site access with owner/editor/viewer roles and strong-ETag `If-Match` concurrency control (`428` when missing, `412` when stale or foreign). The ETag embeds the site UUID, so a tag from another site never validates, and the `*` wildcard form is rejected.

Explicitly not implemented (and not claimed): Tailscale private-network exposure, the 30-minute public gate, articles/pages/modules, multi-site fan-out, key/source build operations, and production deployment. The server refuses to bind any non-loopback address.

## Requirements

- Go 1.25 or newer (developed and verified with Go 1.27.3).
- PostgreSQL 14 or newer (verified against PostgreSQL 18).
- A local admin front end origin for WebAuthn, by default `http://localhost:5174` with relying-party ID `localhost`.

## Layout

```text
admin/backend/
├── cmd/fuyao-admin/          CLI entry point
├── internal/cli/             migrate / serve / bootstrap-admin / create-site
├── internal/config/          environment configuration and validation
├── internal/db/              pool helpers and the SQL migration runner
├── internal/model/           shared domain records
├── internal/store/           PostgreSQL persistence (one file per aggregate)
├── internal/wa/              WebAuthn relying-party setup and adapters
├── internal/httpapi/         HTTP handlers, middleware, and integration tests
└── internal/testauth/        synthetic authenticator used by tests only
```

The HTTP contract lives at `contracts/openapi/management.yaml` in the repository root and describes exactly the implemented endpoints.

## Configuration

All configuration is read from environment variables. See `config.example.env` for a template without real credentials.

| Variable                      | Default                 | Purpose                                                                                                                             |
| ----------------------------- | ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_DATABASE_URL`    | required                | PostgreSQL DSN. Never commit a real value.                                                                                          |
| `FUYAO_ADMIN_ADDR`            | `127.0.0.1:8787`        | Listen address; must be loopback or startup fails.                                                                                  |
| `FUYAO_ADMIN_RP_ID`           | `localhost`             | WebAuthn relying-party ID.                                                                                                          |
| `FUYAO_ADMIN_RP_ORIGINS`      | `http://localhost:5174` | Comma-separated allowed origins. Plain HTTP is accepted only on loopback hosts; https origins enable the cookie `Secure` attribute. |
| `FUYAO_ADMIN_RP_DISPLAY_NAME` | `Fuyao Admin`           | Display name in creation options.                                                                                                   |
| `FUYAO_ADMIN_SESSION_TTL`     | `12h`                   | Session lifetime.                                                                                                                   |
| `FUYAO_ADMIN_CHALLENGE_TTL`   | `5m`                    | WebAuthn challenge lifetime.                                                                                                        |
| `FUYAO_ADMIN_PERMIT_TTL`      | `10m`                   | Enrollment permit lifetime.                                                                                                         |

## Same-Origin Development Topology

The admin front end dev server runs at `http://localhost:5174` and proxies
`/api` to the backend at `127.0.0.1:8787` (see `admin/frontend/vite.config.ts`).
The browser therefore talks only to `http://localhost:5174`, which makes the
WebAuthn origin (`http://localhost:5174`) and the session cookie first-party
and same-origin. Pointing a `localhost` page at `http://127.0.0.1:8787` directly
is a cross-site context (an IP literal is never same-site with a hostname) and
would drop SameSite cookies; do not use that topology.

## Quick Start (local development)

```bash
# from admin/backend
export FUYAO_ADMIN_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/fuyao_admin'  # your local DSN

go run ./cmd/fuyao-admin migrate
go run ./cmd/fuyao-admin bootstrap-admin --username alice --display-name Alice
# prints a one-time permit, e.g. MFRG-GZYT-MZWU-4TCO (valid 10 minutes)
go run ./cmd/fuyao-admin create-site --slug blog --name "Fuyao Blog" --locale zh-CN --owner alice
go run ./cmd/fuyao-admin serve   # http://127.0.0.1:8787
```

First-run flow:

1. `bootstrap-admin` refuses to run once an active administrator exists. It prints the permit code once to the terminal; the code is a secret, is never logged, and only authorizes passkey enrollment. Once the first administrator is registered, all remaining permits are dead: the options endpoint answers `409 bootstrap_closed` and the verify transaction enforces the same invariant atomically.
2. The admin front end (a later work package) posts the permit to `POST /api/v1/auth/register/options`, runs `navigator.credentials.create()`, and completes enrollment at `POST /api/v1/auth/register/verify`, which signs the administrator in.
3. Later visits use `POST /api/v1/auth/login/options` + `POST /api/v1/auth/login/verify` (discoverable passkey, user verification required).
4. `GET /api/v1/session` reports login state; the CSRF token is stable for the whole session, so multiple tabs stay writable. Mutations require the `X-CSRF-Token` header and an allowed `Origin`.

## HTTP Cookie Notes

- `fuyao_admin_session` (path `/`, HttpOnly, SameSite=Lax) holds an opaque token; only its SHA-256 digest is stored server-side and sessions are revoked on logout. SameSite=Lax works because the front end reaches the API same-origin through the dev proxy (or a reverse proxy in deployment), not cross-site.
- `fuyao_admin_auth_tx` (path `/api/v1/auth`, HttpOnly, SameSite=Lax) binds the WebAuthn ceremony; challenges are single-use and expire with the cookie.
- In an all-loopback HTTP development configuration the cookies are intentionally sent without the `Secure` attribute because browsers would otherwise drop them. Any `https` (or mixed) origin configuration enables `Secure`. This exception is strictly limited to loopback origins and is rejected for any other HTTP host.

## Testing

Integration tests run against a real PostgreSQL instance and create an isolated, randomly named schema per test (dropped afterwards):

```bash
# from admin/backend
FUYAO_ADMIN_TEST_DSN='postgres://user@/fuyao_mvp_test?host=/path/to/socket&port=5432' go test ./...
```

A present-but-invalid `FUYAO_ADMIN_*_TTL` value aborts startup instead of falling back to a default.

When `FUYAO_ADMIN_TEST_DSN` is unset, the tests look for the repository-local descriptor at `.fuyao/cms-mvp/postgres-test.json` and skip gracefully if it is absent. The tests refuse to run against any database other than `fuyao_mvp_test`. Test sessions are constructed inside `_test.go` files only; the runtime has no equivalent backdoor.

Covered by the suite: anonymous access denial, full bootstrap/register/login/settings lifecycle, concurrent first enrollments (exactly one winner), permit expiry during an in-flight ceremony, full-ETag If-Match binding (foreign tags, forged suffixes, wildcard rejection, 428/412), stable CSRF across two concurrent clients, verify-path body limits and trailing-JSON rejection, CSRF and Origin enforcement, membership isolation with viewer write rejection and cross-site 404, challenge replay/expiry with signature tampering, permit expiry and single use, TTL misconfiguration rejection, and settings validation.

## Verification

```bash
cd admin/backend
gofmt -l .
go vet ./...
go test ./...
go build ./...
```
