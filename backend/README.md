# Fuyao Management Backend

English | [简体中文](./README_ZH.md)

The Go backend of the FuyaoManager content management system: a real PostgreSQL backend with CLI-issued one-time-code authentication, in-browser site creation with on-disk provisioning, theme registry and selection, snapshot builds with private previews, and the publish/rollback pipeline. The React admin console (`frontend`) talks to this API same-origin.

## Status and Boundaries

Verified in this package:

- Real PostgreSQL persistence with SQL migrations (no in-memory or JSON substitutes).
- **One-time-code authentication as the default**: every login is a code printed by the server CLI (`fuyao-admin init-admin` for the first administrator, `fuyao-admin login-code --username` for every later login). Codes carry 80 bits of entropy, are stored only as SHA-256 digests, expire quickly (`FUYAO_ADMIN_AUTH_CODE_TTL`), burn one attempt per presentation (lock after 6), and are redeemed by `POST /api/v1/auth/exchange` in a single transaction — code consumption, purpose enforcement (initialization vs login), first-administrator activation under the enrollment advisory lock, and session creation commit or roll back together, so concurrent redemptions of one code can never both succeed. An in-memory per-address limiter paces the endpoint. Each redemption mints a brand-new session (rotation).
- Passkey (WebAuthn) rows and endpoints remain untouched for local development; see "Passkey compatibility" below for their exact applicability. HTTP logins never depend on them.
- Cookie sessions with server-side revocation, CSRF token enforcement, and strict Origin checking on unsafe methods; plain-HTTP origins are allowed only on loopback or hosts explicitly listed in `FUYAO_ADMIN_TRUSTED_HOSTS`.
- Membership-scoped site access with owner/editor/viewer roles and strong-ETag `If-Match` concurrency control (`428` when missing, `412` when stale or foreign).
- In-browser site creation (`POST /api/v1/sites`): slug validation, optional initial theme selection validated like a theme PUT, visibility defaulting to private, and explicit provisioning state — the per-site content/config directory tree under `FUYAO_ADMIN_SITES_ROOT` must actually be created before a site reports `ready`; failures stay retryable via `POST /api/v1/sites/{siteId}/provision` (idempotent).
- Theme registry, snapshot builds, private previews, publish and rollback as delivered by work packages T3-a/b1/b2 (see `admin/ROADMAP.md`).
- Optional static serving of the built admin console (`FUYAO_ADMIN_UI_DIR`) with a restrictive CSP and SPA fallback.
- **Single content authority** (`internal/content` + `/api/v1/sites/{siteId}/...`): markdown posts CRUD (frontmatter fields exactly as the theme's index generator consumes them, `draft` as the public-output gate, digest-based If-Match, `%2F`-encoded category/slug paths), the friends document, social links, and the visual site.yaml configuration (profile, repository, SEO, services) with the AMap security code as a write-only secret. Production-mode builds consume the per-site content/config/favicon roots — the frozen snapshot reflects API edits, drafts never reach it.
- **Albums and footprints**: photo uploads (type sniffed from bytes, 40 MiB cap, server-controlled `yyyy/mm/dd` paths) with asynchronous EXIF/thumbnail processing through the site theme's own indexer (persisted `site_media_tasks` with failure records and restart recovery), year-album metadata editing (title/description/alt/ordering paired by filename), reference-checked deletion (photos still listed by any live or rollback-able release's frozen metadata are refused with `photo_in_use`), and the footprint points document (bilingual names, GCJ-02 bounds, city/spot types, YYYYMMDD dates, unique ids) backing the map picker in the console.

## Requirements

- Go 1.25 or newer (developed and verified with Go 1.27.3).
- PostgreSQL 14 or newer (verified against PostgreSQL 18).
- Node 22 for theme builds (consumed by the build runner; see `FUYAO_ADMIN_REPO_ROOT`).

## Layout

```text
backend/
├── cmd/fuyao-admin/          CLI entry point
├── internal/cli/             migrate / serve / init-admin / login-code / revoke-sessions / bootstrap-admin / create-site / validate-theme / register-theme
├── internal/config/          environment configuration and validation
├── internal/db/              pool helpers and the SQL migration runner
├── internal/model/           shared domain records
├── internal/siteprovision/   per-site content/config directory provisioning
├── internal/store/           PostgreSQL persistence (one file per aggregate)
├── internal/themecontract/   contracts/theme schema loading and validation
├── internal/themeregistry/   trusted theme-root loading into the catalog (CLI-driven)
├── internal/wa/              WebAuthn relying-party setup and adapters
├── internal/httpapi/         HTTP handlers, middleware, UI hosting, and integration tests
└── internal/testauth/        synthetic authenticator used by tests only
```

The HTTP contract lives at `contracts/openapi/management.yaml` in the repository root and describes exactly the implemented endpoints.

## Configuration

All configuration is read from environment variables. See `config.example.env` for a template without real credentials.

| Variable                                      | Default                                     | Purpose                                                                                                                                                                                                                                                                             |
| --------------------------------------------- | ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_DATABASE_URL`                    | required                                    | PostgreSQL DSN. Never commit a real value.                                                                                                                                                                                                                                          |
| `FUYAO_ADMIN_ADDR`                            | `127.0.0.1:8787`                            | Listen address; loopback always works, any other host must be listed in `FUYAO_ADMIN_TRUSTED_HOSTS`.                                                                                                                                                                                |
| `FUYAO_ADMIN_TRUSTED_HOSTS`                   | unset                                       | Comma-separated hosts allowed for non-loopback binding and plain-HTTP origins (e.g. the server's Tailscale IP).                                                                                                                                                                     |
| `FUYAO_ADMIN_RP_ID`                           | `localhost`                                 | WebAuthn relying-party ID (passkey compatibility only).                                                                                                                                                                                                                             |
| `FUYAO_ADMIN_RP_ORIGINS`                      | `http://localhost:5174`                     | Comma-separated allowed origins (drives CORS, Origin guard, and cookie `Secure`). Plain HTTP is accepted only on loopback or trusted hosts.                                                                                                                                         |
| `FUYAO_ADMIN_RP_DISPLAY_NAME`                 | `Fuyao Admin`                               | Display name in creation options.                                                                                                                                                                                                                                                   |
| `FUYAO_ADMIN_SESSION_TTL`                     | `12h`                                       | Session lifetime.                                                                                                                                                                                                                                                                   |
| `FUYAO_ADMIN_CHALLENGE_TTL`                   | `5m`                                        | WebAuthn challenge lifetime.                                                                                                                                                                                                                                                        |
| `FUYAO_ADMIN_PERMIT_TTL`                      | `10m`                                       | Passkey enrollment permit lifetime (legacy path).                                                                                                                                                                                                                                   |
| `FUYAO_ADMIN_AUTH_CODE_TTL`                   | `10m`                                       | One-time code lifetime (`init-admin`, `login-code`).                                                                                                                                                                                                                                |
| `FUYAO_ADMIN_THEME_CONTRACTS`                 | unset                                       | Path to `contracts/theme` in the repository. When set, the schemas are compiled at startup and theme-configuration saves are validated against each theme's own schema; when unset, saving a selection fails with `503 theme_contracts_unavailable` instead of skipping validation. |
| `FUYAO_ADMIN_REPO_ROOT`                       | working directory                           | Repository root anchoring the build workspace and release roots under `<repo>/site/.fuyao`. Set it explicitly when starting `serve` from elsewhere.                                                                                                                                 |
| `FUYAO_ADMIN_SITES_ROOT`                      | `<repo>/site/.fuyao/admin-sites`            | Root for per-site content/config trees (`<root>/<siteId>/{content,config}`).                                                                                                                                                                                                        |
| `FUYAO_ADMIN_UI_DIR`                          | unset                                       | Built admin console directory; when set, non-`/api` paths serve this static SPA (production topology).                                                                                                                                                                              |
| `FUYAO_ADMIN_PREVIEW_BIND`                    | `127.0.0.1`                                 | Bind address of preview listeners; the private-network address enables remote (Tailscale) previews.                                                                                                                                                                                 |
| `FUYAO_ADMIN_PREVIEW_EXTERNAL_BASE`           | unset                                       | URL base handed to browsers for preview links (e.g. `http://100.117.154.115`); never a loopback literal when remote clients exist.                                                                                                                                                  |
| `FUYAO_ADMIN_EDGE_SNIPPET_DIR`                | unset                                       | Managed Caddy snippet directory; set to activate published-site edge management.                                                                                                                                                                                                    |
| `FUYAO_ADMIN_EDGE_APPLY_CMD`                  | `sudo -n /usr/local/sbin/fuyao-caddy-apply` | Operator-controlled validate+reload command with fixed arguments.                                                                                                                                                                                                                   |
| `FUYAO_ADMIN_EDGE_PRIVATE_BIND`               | `127.0.0.1`                                 | Address private site entries bind and answer on (the Tailscale address in deployment).                                                                                                                                                                                              |
| `FUYAO_ADMIN_EDGE_PUBLIC_BIND`                | `:` (all interfaces)                        | Address public site entries bind.                                                                                                                                                                                                                                                   |
| `FUYAO_ADMIN_EDGE_PUBLIC_HOST`                | unset                                       | URL host announced for public entries (the public IP when bound on all interfaces).                                                                                                                                                                                                 |
| `FUYAO_ADMIN_BUILDS_ROOT`                     | `<repo>/site/.fuyao/admin-builds`           | Persistent build-task root; point outside the program directory in deployments.                                                                                                                                                                                                     |
| `FUYAO_ADMIN_RELEASES_ROOT`                   | `<repo>/site/.fuyao/admin-releases`         | Persistent release root; point outside the program directory in deployments.                                                                                                                                                                                                        |
| `FUYAO_ADMIN_PORT_MIN`/`FUYAO_ADMIN_PORT_MAX` | `20000`/`20100`                             | The operator-approved port range for site entries and preview listeners; invalid or inverted values abort startup.                                                                                                                                                                  |
| `FUYAO_ADMIN_BUILD_MODE`                      | `ci`                                        | Content source for platform-driven builds: `ci` uses the repository fixture content; `production` requires the trusted content-root settings.                                                                                                                                       |
| `FUYAO_ADMIN_REQUIRE_CAPABILITIES`            | `blog`                                      | Trusted site requirements a theme must satisfy before building and publishing. The default only guarantees the blog surface; a site serving the full first theme must set `blog,albums,footprint,friends,pay,favicon`.                                                              |
| `FUYAO_ADMIN_REQUIRE_URL_CONVENTIONS`         | `posts,blog-rss,sitemap`                    | Same as above, for URL conventions; the full first theme additionally needs `release.json,data,media-albums,blog-search`.                                                                                                                                                           |

## Same-Origin Development Topology

The admin front end dev server runs at `http://localhost:5174` and proxies
`/api` to the backend at `127.0.0.1:8787` (see `frontend/vite.config.ts`).
The browser therefore talks only to `http://localhost:5174`, keeping the
session cookie first-party and same-origin. Pointing a `localhost` page at
`http://127.0.0.1:8787` directly is a cross-site context (an IP literal is
never same-site with a hostname) and would drop SameSite cookies; do not use
that topology. In production the Go server itself serves the built console
(`FUYAO_ADMIN_UI_DIR`), which is same-origin by construction.

## Quick Start (local development)

```bash
# from backend
export FUYAO_ADMIN_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/fuyao_admin'  # your local DSN

go run ./cmd/fuyao-admin migrate

# First administrator: CLI-authorized binding + one-time initialization code.
go run ./cmd/fuyao-admin init-admin --username alice --display-name Alice
# prints a one-time initialization code, e.g. MFRG-GZYT-MZWU-4TCO (10 minutes)

# Every later login: a fresh one-time code.
go run ./cmd/fuyao-admin login-code --username alice

# Optional: create a site from the CLI (the UI also creates sites).
go run ./cmd/fuyao-admin create-site --slug blog --name "Fuyao Blog" --locale zh-CN --owner alice

# Register a theme into the catalog (operator-driven; the HTTP API never
# accepts file paths). The recorded source is the theme root itself, after
# full manifest and config-schema validation:
go run ./cmd/fuyao-admin register-theme --contracts ../../contracts/theme ../../site

FUYAO_ADMIN_THEME_CONTRACTS="$PWD/../../contracts/theme" \
  go run ./cmd/fuyao-admin serve   # http://127.0.0.1:8787
```

## Authentication Flow

1. **Uninitialized platform**: an anonymous browser gets `platformInitialized: false` from `GET /api/v1/session` and the login screen shows the exact CLI steps instead of a form. `fuyao-admin init-admin --username <name>` registers the still-pending first administrator and prints a single-use initialization code; the first anonymous visitor can never claim administrator identity.
2. **First sign-in**: the console posts the code to `POST /api/v1/auth/exchange`. The redemption transaction activates the pending user (under the first-administrator advisory lock, refusing if any active administrator already exists) and opens the session. Initialization codes cannot be reused for anything else once an administrator exists.
3. **Every later login**: `fuyao-admin login-code --username <name>` prints a fresh one-time code; the console exchanges it the same way. Codes are never reused; a used code answers `409 code_used`, an expired one `409 code_expired`, and a code that does not match the account state `409 code_purpose_mismatch`.
4. **Site creation**: the freshly signed-in owner creates the first site in the UI (basics, theme pick, visibility defaulting to private); the response reports the provisioning state and a failed setup stays retryable.
5. **Sign-out**: the console calls `POST /api/v1/auth/logout`; the session is revoked server-side. `fuyao-admin revoke-sessions --username <name>` revokes every live session of a user from the CLI.
6. `GET /api/v1/session` reports login state and the platform initialization flag; the CSRF token is stable for the whole session, so multiple tabs stay writable. Mutations require the `X-CSRF-Token` header and an allowed `Origin`.

### Passkey compatibility

The passkey endpoints (`/api/v1/auth/register/*`, `/api/v1/auth/login/*`) and all stored credentials remain available, but they only function where the WebAuthn origin matches `FUYAO_ADMIN_RP_ID`/`FUYAO_ADMIN_RP_ORIGINS` — practically the localhost dev topology or an https deployment. They never gate or block code login, and no code path depends on them. The React console no longer offers a passkey entry; `bootstrap-admin` stays for local passkey development.

## HTTP Cookie Notes

- `fuyao_admin_session` (path `/`, HttpOnly, SameSite=Lax) holds an opaque token; only its SHA-256 digest is stored server-side and sessions are revoked on logout. SameSite=Lax works because the front end reaches the API same-origin through the dev proxy, the served console, or a reverse proxy — never cross-site.
- `fuyao_admin_auth_tx` (path `/api/v1/auth`, HttpOnly, SameSite=Lax) binds the WebAuthn ceremony; challenges are single-use and expire with the cookie.
- In an all-loopback HTTP development configuration the cookies are intentionally sent without the `Secure` attribute because browsers would otherwise drop them. Any `https` (or mixed) origin configuration enables `Secure`. Plain HTTP beyond loopback is accepted only for hosts explicitly listed in `FUYAO_ADMIN_TRUSTED_HOSTS` — never guessed from request headers.

## Testing

Integration tests run against a real PostgreSQL instance and create an isolated, randomly named schema per test (dropped afterwards):

```bash
# from backend
FUYAO_ADMIN_TEST_DSN='postgres://user@/fuyao_mvp_test?host=/path/to/socket&port=5432' go test ./...
```

A present-but-invalid `FUYAO_ADMIN_*_TTL` value aborts startup instead of falling back to a default.

When `FUYAO_ADMIN_TEST_DSN` is unset, the tests look for the repository-local descriptor at `.fuyao/cms-mvp/postgres-test.json` and skip gracefully if it is absent. The tests refuse to run against any database other than `fuyao_mvp_test`. Test sessions are constructed inside `_test.go` files only; the runtime has no equivalent backdoor.

Covered by the suite: anonymous access denial, the full one-time-code lifecycle (initialization probe, init redemption activating the pending administrator, per-login codes, wrong/used/expired codes, purpose separation, concurrent redemption with exactly one winner, replay-stable problem codes, logout invalidation), site creation (provisioning on disk, duplicate slug 409, slug validation, membership isolation, idempotent provision retry), settings/theme/build/release surfaces as documented in `admin/ROADMAP.md`, plus the hardening set: full-ETag If-Match binding, stable CSRF across two concurrent clients, verify-path body limits, CSRF and Origin enforcement, viewer write rejection with cross-site 404, TTL misconfiguration rejection, and trusted-adapter/manifest/schema/path rejection in `internal/themeregistry`.

## Verification

```bash
cd backend
gofmt -l .
go vet ./...
go test ./...
go build ./...
```
