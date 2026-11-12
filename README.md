**English** | [简体中文](./README_ZH.md)

# FuyaoManager

<div align="center">

[![License](https://img.shields.io/badge/license-AGPL--3.0--only-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8.svg)](https://go.dev/)
[![React](https://img.shields.io/badge/React-18-61DAFB.svg)](https://react.dev/)
[![Ant Design](https://img.shields.io/badge/Ant_Design-5-1677ff.svg)](https://ant.design/)

</div>

FuyaoManager is a self-hosted site and content management platform: it owns
sites, content, media tasks, theme registration, frozen-snapshot builds,
private previews, publishing, rollback, and the managed reverse-proxy edge
for the static sites it produces. Themes are external projects consumed
through versioned contracts — the first theme is
[FuyaoHomepage](https://github.com/skyrocketingHong/FuyaoHomepage)
(theme id `fuyao-homepage`, adapter `node22-sveltekit-static`).

The repository was extracted from FuyaoHomepage on 2026-11-12 with the
platform development history preserved; see
[docs/SOURCE_MIGRATION.md](./docs/SOURCE_MIGRATION.md).

## Features

- **Authentication without passwords**: every login is a one-time code issued
  by the local CLI (`fuyao-admin login-code`); sessions are cookie-based
  with CSRF and Origin enforcement. Passkey rows and endpoints remain as a
  domain-RP-ID compatibility path.
- **In-browser site creation**: per-site content, config, and favicon trees
  provisioned on disk with explicit state and idempotent retry.
- **Content authority**: posts (frontmatter-aware, draft-gated), friends,
  social links, visual site configuration with a write-only AMap security
  code, footprint points — all writes go through one jailed content store
  with digest ETags and atomic writes.
- **Albums and media tasks**: uploads with server-controlled paths, the
  selected theme's own Node indexer executed read-only against the site's
  album directories, year metadata edited per filename, deletes guarded by
  release references.
- **Theme registry**: operator-driven registration
  (`fuyao-admin register-theme`) validating theme.json and its config schema
  against the authoritative contracts; per-site selection with If-Match.
- **Builds**: frozen input snapshots prepared by the theme itself, builds in
  task-private source copies (never writing the registered theme), full
  per-file source manifests, capability and URL-convention checks.
- **Private previews**: per-build loopback origins with one-time entry
  tokens; close revokes tokens, stops the listener, and frees the port.
- **Publish and rollback**: contract-validated release metadata, immutable
  sealed artifacts, atomic live-pointer switching under the per-site publish
  lock, retry-safe failures, rollback without rebuilding.
- **Managed edge**: rendered Caddy snippet from database state, per-site
  public/private visibility with explicit interface binding, apply through a
  fixed operator command with last-good restore.
- **Console**: React 18 + official Ant Design, zh-CN/en-US, day/night
  themes, grouped desktop-app shell.

## Tech stack

| Layer     | Choice                                                             |
| --------- | ------------------------------------------------------------------ |
| Backend   | Go 1.27, PostgreSQL 18 (pgx), JSON Schema draft 2020-12            |
| Console   | TypeScript, React 18, Vite, Ant Design 5, Simple Icons             |
| Contracts | OpenAPI 3.0 (management API), JSON Schema (theme contracts)        |
| Edge      | Caddy (managed snippet, rendered — never executed from user input) |

## Requirements

- Go 1.27+
- Node.js 22+ (console build)
- PostgreSQL 18 (local or remote; migrations applied by the CLI)
- A theme project to register (e.g. a FuyaoHomepage checkout with
  dependencies installed) — the platform itself starts and reports an empty
  theme catalog without one.

## Quick start

```bash
# Console (frontend/)
cd frontend && npm ci
npm run api:generate   # types from ../contracts/openapi/management.yaml
npm run build

# Backend (backend/)
cd ../backend
export FUYAO_ADMIN_DATABASE_URL='postgres://fuyao:...@127.0.0.1:5432/fuyao_admin?sslmode=disable'
go run ./cmd/fuyao-admin migrate
go run ./cmd/fuyao-admin init-admin --username admin --display-name Admin   # prints a one-time code
go run ./cmd/fuyao-admin serve
# console dev server: cd frontend && npm run dev  (proxies /api to 127.0.0.1:8787)

# Register a theme (operator path; the theme root must pass the contracts)
go run ./cmd/fuyao-admin register-theme \
  --db "$FUYAO_ADMIN_DATABASE_URL" \
  --contracts ../contracts/theme \
  --root /path/to/FuyaoHomepage/site
```

Open the printed one-time code URL flow in the console, create a site,
select the theme, write content, build, preview, publish.

## Commands

Root `package.json` forwards to the isolated frontend package:

| Command                                           | Purpose                                       |
| ------------------------------------------------- | --------------------------------------------- |
| `npm run frontend:dev`                            | Vite dev server (port 5174, proxy to the API) |
| `npm run frontend:build`                          | Typecheck + production build                  |
| `npm run frontend:check` / `frontend:test`        | Typecheck / vitest                            |
| `npm run api:generate` / `api:check` / `api:lint` | OpenAPI types, drift check, redocly lint      |

Backend (run inside `backend/`):

| Command                       | Purpose                                                          |
| ----------------------------- | ---------------------------------------------------------------- |
| `fuyao-admin migrate`         | Apply all SQL migrations                                         |
| `fuyao-admin serve`           | Start the management API (+ console if `FUYAO_ADMIN_UI_DIR` set) |
| `fuyao-admin init-admin`      | First-run binding; prints a one-time init code                   |
| `fuyao-admin login-code`      | One-time login code for an existing admin                        |
| `fuyao-admin revoke-sessions` | Revoke a user's sessions                                         |
| `fuyao-admin register-theme`  | Register/refresh a theme in the catalog                          |
| `fuyao-admin validate-theme`  | Validate a theme root against the contracts                      |

## Verification

```bash
# Backend (needs a PostgreSQL test database; see backend/README.md)
cd backend
gofmt -l . && go vet ./... && go build ./...
FUYAO_ADMIN_TEST_DSN='postgres://fuyao_test@/fuyao_mvp_test?host=/tmp/pgsock&port=55432&sslmode=disable' \
  go test ./... -count=1

# Real-theme integration (optional; without it the six real-build tests skip)
FUYAO_TEST_THEME_ROOT=/path/to/FuyaoHomepage/site \
  go test ./internal/httpapi -count=1 -run 'TestBuildPreviewClosedLoop|TestPublishRollbackClosedLoop'
```

## Architecture

```
Console (React) ──/api──▶ Go management API ──▶ PostgreSQL (sites, themes, builds, releases, tasks)
                              │
                              ├─ content store (per-site trees: content/, config/, favicon/)
                              ├─ build runner (task copies of the registered theme root,
                              │   frozen snapshots, sealed artifacts)
                              ├─ preview origins (loopback, per-build, token-gated)
                              ├─ publish manager (sealed releases, atomic live pointer)
                              ├─ media tasks (theme's Node indexer, read-only)
                              └─ edge manager (renders + applies the managed Caddy snippet)
```

Authoritative contracts live in `contracts/`: `openapi/management.yaml`
(the HTTP surface, with generated TypeScript types and a route-coverage
test) and `theme/` (manifest, build-input, and release JSON Schemas plus
public fixtures, consumed identically by the Go validator and the theme's
Node tooling).

## Project structure

See [PROJECT_STRUCTURE.md](./PROJECT_STRUCTURE.md). The iteration ledger is
[docs/ROADMAP.md](./docs/ROADMAP.md) (bilingual under `docs/`).

## Deployment

Server templates (systemd unit, environment samples, Caddy apply helper)
live in [deploy/fuyao-admin/](./deploy/fuyao-admin/); the bilingual README
there documents the low-memory build constraints and the managed-snippet
topology. The currently deployed server still runs the pre-split build; the
switch plan is documented in the roadmap.

## License

[AGPL-3.0-only](./LICENSE). Theme projects keep their own licenses.
