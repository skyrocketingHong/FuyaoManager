# Fuyao Admin Frontend (Local Development MVP)

English | [简体中文](./README_ZH.md)

The React 18 + TypeScript + Vite + Ant Design admin console of the FuyaoManager management system (brand icons via Simple Icons). The console covers: first-run passkey enrollment, passkey login/logout, the site list, site detail, site settings editing with optimistic-concurrency conflict handling, per-site theme selection, build-preview tasks with private preview links, and the publish/rollback release history. There are deliberately no pages for articles, modules, or key/source operations yet; see [admin/ROADMAP.md](../ROADMAP.md).

## First-Time Setup

The console keeps its own lockfile, so the repository-root `npm ci` does not
install it. Once per checkout:

```bash
cd frontend
npm ci
```

### 1. Create a dedicated development database

Use a PostgreSQL instance you control and create a database dedicated to
local development (do not reuse the integration-test database; the throwaway
task descriptor under `.fuyao/cms-mvp/` exists for tests only and is not a
deployment configuration):

```bash
createdb fuyao_admin_dev   # example; any local PostgreSQL works
export FUYAO_ADMIN_DATABASE_URL='postgres://user@localhost:5432/fuyao_admin_dev'
```

### 2. Migrate, enroll the first administrator, register the theme

```bash
cd backend
go run ./cmd/fuyao-admin migrate
go run ./cmd/fuyao-admin bootstrap-admin --username admin
#   prints a single-use enrollment permit; keep it for the browser step
go run ./cmd/fuyao-admin register-theme   -contracts "$PWD/../../contracts/theme" "$PWD/../../site"
```

### 3. Run the backend and the console

Two terminals (the backend needs the environment variables from step 1 plus
the contract and repository-root settings):

```bash
# terminal 1: backend on 127.0.0.1:8787
cd backend
FUYAO_ADMIN_DATABASE_URL=... \
FUYAO_ADMIN_THEME_CONTRACTS="$PWD/../../contracts/theme" \
FUYAO_ADMIN_REPO_ROOT="$PWD/../.." \
FUYAO_ADMIN_REQUIRE_CAPABILITIES='blog,albums,footprint,friends,pay,favicon' \
FUYAO_ADMIN_REQUIRE_URL_CONVENTIONS='release.json,posts,data,media-albums,sitemap,blog-rss,blog-search' \
go run ./cmd/fuyao-admin serve

# terminal 2: console on http://localhost:5174 (from the repository root)
npm run admin:dev
```

`FUYAO_ADMIN_THEME_CONTRACTS` enables theme validation and the publish
pipeline; `FUYAO_ADMIN_REPO_ROOT` anchors build and release directories under
`<repo>/site/.fuyao`. The two `FUYAO_ADMIN_REQUIRE_*` variables are optional:
their defaults only guarantee the blog surface — set the full lists above
when the site actually serves the whole first theme.

### 4. First login and first site

1. Open `http://localhost:5174` — must be `localhost`, not `127.0.0.1`
   (WebAuthn binds to the origin `http://localhost:5174`).
2. Follow "first time", enter the permit from step 2, and enroll your
   passkey.
3. Create the first site (CLI, in `backend` with the DSN exported):
   `go run ./cmd/fuyao-admin create-site --slug blog --name "My Site"`.
4. In the console: select the theme, build, preview, publish, roll back.

### Default URL, failures, and stopping

- Default URL is `http://localhost:5174` (pinned port, loopback only). If the
  port is occupied the dev server exits with "Port 5174 is already in use";
  free the port rather than letting the origin drift.
- If the console reports a network error, the backend is not reachable:
  check terminal 1, `FUYAO_ADMIN_DATABASE_URL`, and that `fuyao-admin serve`
  is listening on 127.0.0.1:8787.
- Stop both services with Ctrl-C in their terminals. When the backend was
  started with `nohup ... &`, stop it by port instead:
  `kill $(lsof -tnP -iTCP:8787 -sTCP:LISTEN)`.

## Topology

The dev server runs at `http://localhost:5174` and proxies `/api` to the backend at `127.0.0.1:8787` (`vite.config.ts`). The browser only ever talks to `http://localhost:5174`, keeping the WebAuthn origin and session cookies same-origin and first-party. Never point a `localhost` page directly at `http://127.0.0.1:8787`: an IP literal is cross-site from `localhost`, and SameSite cookies would be dropped.

## Commands

This package keeps its own isolated lockfile so site dependency resolution
never moves (see First-Time Setup below).

```bash
# from the repository root
npm run admin:dev            # start the dev server on http://localhost:5174 (pinned port, loopback only)
npm run admin:check          # tsc --noEmit (types)
npm run admin:test           # vitest unit tests (API client, editor state machine, theme selection, i18n, mounted views)
npm run admin:build          # production build to build/
npm run admin:api:generate   # regenerate src/lib/api/schema.d.ts from contracts/openapi/openapi.yaml
npm run admin:api:check      # fail if the committed schema.d.ts drifted from the contract
npm run admin:contract:lint  # semantic OpenAPI lint (redocly)
```

The dev port is pinned with `strictPort`: if 5174 is occupied, startup fails
loudly instead of drifting to another origin where passkeys and cookies would
break.

`src/lib/api/schema.d.ts` is generated by [openapi-typescript](https://github.com/openapi-ts/openapi-typescript) and committed; change the contract first, then regenerate, so API types cannot drift from the authoritative OpenAPI file.

## Layout

```text
frontend/src/
├── App.tsx                       # route table; hierarchy mirrors paths
├── main.tsx                      # ConfigProvider (antd locale + day/night algorithm) and router
├── app.css                       # layout shell + prefers-reduced-motion overrides
├── lib/                          # framework-agnostic business modules (no JSX)
│   ├── api/
│   │   ├── client.ts             # same-origin fetch wrapper: CSRF, If-Match, problem+json, ETag
│   │   ├── management.ts         # typed endpoints over the generated schema
│   │   ├── passkey.ts            # WebAuthn ceremonies via @simplewebauthn/browser (no casts)
│   │   └── schema.d.ts           # generated from contracts/openapi/openapi.yaml
│   ├── sites/                    # site-detail loading; settings-editor state machine
│   ├── themes/                   # theme-selection store (catalog, If-Match save, 412 sync)
│   ├── i18n/                     # zh-CN / en-US dictionaries + external store
│   ├── theme/                    # day/night theme state (antd algorithm)
│   └── session/                  # session state
├── components/
│   ├── layout/AppShell.tsx       # top bar: locale, day/night, user, GitHub mark (Simple Icons)
│   └── sites/SiteNav.tsx         # per-site sub-navigation
└── pages/                        # route pages keyed on siteId; hierarchy mirrors paths
    ├── LoginPage.tsx             # passkey login
    ├── BootstrapPage.tsx         # first-run permit enrollment
    ├── SitesPage.tsx             # site list (membership-scoped)
    └── sites/
        ├── SiteDetailPage.tsx    # site overview
        ├── SettingsPage.tsx      # settings editor
        ├── ThemePage.tsx         # theme selection + configuration
        ├── BuildsPage.tsx        # build tasks, polling, private preview links
        └── ReleasesPage.tsx      # current release, history, publish, rollback
```

## Behavior Notes

- Settings editing is guarded by the strong ETag from `GET .../settings`: saves send `If-Match` and submit only the fields the user actually changed, so concurrent edits to other fields are never clobbered. On `412`, syncing adopts the server values for untouched fields, keeps genuinely edited fields dirty (with an explicit conflict notice), and never discards unsaved input. Values typed while a save is in flight stay marked unsaved. Viewer-role members get a read-only form.
- Site pages key their whole composition on `siteId` and loads are race-guarded, so switching between sites (including rapid back-and-forth or out-of-order responses) can never show or write another site's data.
- All UI text comes from the i18n dictionaries; day/night themes switch the antd algorithm, and `prefers-reduced-motion` collapses every animation and transition (including antd's).
- Passkeys are the only login method; there is no password, development admin, or anonymous write access anywhere in the stack.
