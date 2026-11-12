English | [简体中文](./CONFIGURATION_ZH.md)

# Configuration Reference

All configuration is environment-driven. `backend/config.example.env` is
the local-development template; `deploy/fuyao-admin/config.example.env` is
the server template. Real DSNs and secrets never enter the repository.

## Service

| Variable                      | Default                 | Meaning                                                                                                   |
| ----------------------------- | ----------------------- | --------------------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_DATABASE_URL`    | — (required)            | PostgreSQL DSN.                                                                                           |
| `FUYAO_ADMIN_ADDR`            | `127.0.0.1:8787`        | Listen address. Non-loopback hosts must be listed in `FUYAO_ADMIN_TRUSTED_HOSTS`.                         |
| `FUYAO_ADMIN_TRUSTED_HOSTS`   | empty                   | Comma-separated hosts allowed for non-loopback binding and plain-HTTP origins (e.g. a Tailscale address). |
| `FUYAO_ADMIN_RP_ID`           | `localhost`             | WebAuthn relying-party ID (production: `manager.fuyaoskyrocket.ing`; IP-literal deployments answer 503 on passkey endpoints). |
| `FUYAO_ADMIN_RP_DISPLAY_NAME` | `FuyaoManager`          | Relying-party display name.                                                                               |
| `FUYAO_ADMIN_RP_ORIGINS`      | `http://localhost:5174` | Accepted origins; an https origin switches session cookies to `Secure`.                                   |
| `FUYAO_ADMIN_SESSION_TTL`     | `12h`                   | Session lifetime.                                                                                         |
| `FUYAO_ADMIN_CHALLENGE_TTL`   | `5m`                    | WebAuthn challenge lifetime.                                                                              |
| `FUYAO_ADMIN_PERMIT_TTL`      | `10m`                   | Enrollment permit lifetime.                                                                               |
| `FUYAO_ADMIN_AUTH_CODE_TTL`   | `10m`                   | One-time CLI code lifetime (identity proofing and recovery).                                               |
| `FUYAO_ADMIN_REGISTRATION_SESSION_TTL` | `30m` | Restricted registration session lifetime (issued when the account has no passkey for the current RP). |
| `FUYAO_ADMIN_UI_DIR`          | empty                   | Built console directory; non-`/api` paths serve this SPA under a restrictive CSP.                         |

## Paths and contracts

| Variable                      | Default                               | Meaning                                                                                     |
| ----------------------------- | ------------------------------------- | ------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_REPO_ROOT`       | discovered upward by markers          | Anchors the defaults below and the bundled contract directory. An explicit value is operator-owned: any existing directory works (a binary-only installation needs no markers). Without one, the root is discovered upward from the working directory; an undiscoverable root aborts startup instead of guessing. |
| `FUYAO_ADMIN_THEME_CONTRACTS` | `<repo>/contracts/theme` when present | Authoritative theme contracts. An explicitly configured but broken directory fails startup. |
| `FUYAO_ADMIN_SITES_ROOT`      | `<repo>/data/sites`                   | Per-site `content/`, `config/`, `favicon/` trees.                                           |
| `FUYAO_ADMIN_BUILDS_ROOT`     | `<repo>/data/builds`                  | Per-task directories (snapshot + artifacts + logs).                                         |
| `FUYAO_ADMIN_RELEASES_ROOT`   | `<repo>/data/releases`                | Sealed per-site release directories. The three roots must resolve to three independent real directories; provisioning never overwrites existing site files. |

## Builds, previews, edge

| Variable                                        | Default                                     | Meaning                                                                                                                        |
| ----------------------------------------------- | ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `FUYAO_ADMIN_BUILD_MODE`                        | `ci`                                        | `ci` uses the theme's fixture content; `production` requires the per-site content roots (implied by `FUYAO_ADMIN_SITES_ROOT`). |
| `FUYAO_ADMIN_REQUIRE_CAPABILITIES`              | `blog`                                      | Trusted server-side capability requirements a theme must cover.                                                                |
| `FUYAO_ADMIN_REQUIRE_URL_CONVENTIONS`           | `posts,blog-rss,sitemap`                    | Trusted URL-convention requirements.                                                                                           |
| `FUYAO_ADMIN_PREVIEW_BIND`                      | `127.0.0.1`                                 | Preview listener bind address.                                                                                                 |
| `FUYAO_ADMIN_PREVIEW_EXTERNAL_BASE`             | empty                                       | URL base handed to browsers for previews (never a 127.0.0.1 literal when remote clients exist).                                |
| `FUYAO_ADMIN_PORT_MIN` / `FUYAO_ADMIN_PORT_MAX` | `20000` / `20100`                           | Platform-allocated port range for site entries and previews.                                                                   |
| `FUYAO_ADMIN_DEPLOYMENT_ENABLED`                | `false`                                     | Run isolation by default: sites, content, builds, and previews work, but no publish server starts, the Caddy snippet is never applied, and publish/rollback/visibility/edge-reapply answer `503 deployment_disabled`. Historical releases stay readable. |
| `FUYAO_ADMIN_EDGE_SNIPPET_DIR`                  | empty                                       | Enables the managed Caddy snippet (rendered from database state); has no effect while the deployment switch is off.            |
| `FUYAO_ADMIN_EDGE_APPLY_CMD`                    | `sudo -n /usr/local/sbin/fuyao-caddy-apply` | Operator-controlled validate + reload command (fixed arguments).                                                               |
| `FUYAO_ADMIN_EDGE_PRIVATE_BIND`                 | `127.0.0.1`                                 | Exact bind address for private site entries.                                                                                   |
| `FUYAO_ADMIN_EDGE_PUBLIC_BIND`                  | empty                                       | Bind for public entries (empty = all interfaces).                                                                              |
| `FUYAO_ADMIN_EDGE_PUBLIC_HOST`                  | empty                                       | Announced host for public entries.                                                                                             |

## Test-only

| Variable                | Meaning                                                                                                                                                                                                 |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_TEST_DSN`  | Integration-test DSN; the tests refuse any database other than `fuyao_mvp_test`. Without it, a task-local descriptor at `<repo>/.fuyao/cms-mvp/postgres-test.json` is used, else PostgreSQL tests skip. |
| `FUYAO_TEST_THEME_ROOT` | External real-theme root (a FuyaoHomepage `site/` checkout with dependencies installed) enabling the six real-Node-build integration tests; without it they skip with an explicit pointer.              |

## Cookie notes

On plain-HTTP topologies (loopback development and the trusted
private-network address) cookies are never marked `Secure` — browsers
silently drop Secure cookies over plain HTTP. `Secure` activates only when
every configured origin is https.
