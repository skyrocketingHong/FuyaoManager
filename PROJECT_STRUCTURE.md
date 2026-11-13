English | [简体中文](./PROJECT_STRUCTURE_ZH.md)

# Project Structure and Overview

## Overview

FuyaoManager is the site and content management platform extracted from
FuyaoHomepage (2026-11-12). It manages sites, content, media tasks, theme
registration, frozen-snapshot builds, private previews, publishing,
rollback, and the managed reverse-proxy edge. Themes are external projects
consumed through the versioned contracts under `contracts/theme/`; the
repository builds and tests without any theme checkout present (real-theme
integration tests activate through `FUYAO_TEST_THEME_ROOT`).

## Directory Structure

```text
.
├── .github/workflows/ci.yml   # CI: backend (real PostgreSQL), frontend,
│                              # optional external-theme integration job
├── .gitignore                 # ignores data/, deps, builds, secrets
├── .npmrc                     # npm engine-strict
├── .prettierignore            # formatting ignores (lockfiles, generated types)
├── .prettierrc                # tab/single-quote formatting
├── AGENTS.md                  # development conventions (this repository)
├── CHANGELOG.md               # English changelog
├── CHANGELOG_ZH.md            # Simplified Chinese changelog
├── CONFIGURATION.md           # English environment-variable reference
├── CONFIGURATION_ZH.md        # Simplified Chinese environment-variable reference
├── PROJECT_STRUCTURE.md       # this document
├── PROJECT_STRUCTURE_ZH.md    # Simplified Chinese project structure
├── README.md                  # default English entry
├── README_ZH.md               # Simplified Chinese entry
├── LICENSE                    # AGPL-3.0-only
├── package.json               # root convenience scripts (frontend owns its lockfile)
├── backend/                   # Go management backend
│   ├── cmd/fuyao-admin/       # CLI: migrate / serve / init-admin / login-code /
│   │                          # revoke-sessions / bootstrap-admin / create-site /
│   │                          # validate-theme / register-theme
│   ├── internal/
│   │   ├── cli/               # command wiring + serve composition
│   │   ├── config/            # environment loading and validation
│   │   ├── db/                # pool setup + SQL migrations (0001-0016)
│   │   ├── model/             # shared records
│   │   ├── store/             # users, sessions, passkeys, auth codes, sites,
│   │   │                      # themes, builds, releases, media tasks, network entries, audit
│   │   ├── httpapi/           # management API: auth (codes, passkey binding,
│   │   │                      # session), sites, content, albums, themes, builds,
│   │   │                      # releases, edge status, UI hosting
│   │   │                      # + testdata/theme (bundled fixture theme)
│   │   ├── content/           # per-site jailed content store (ETag, atomic writes)
│   │   ├── siteprovision/     # per-site directory provisioning
│   │   ├── themeregistry/     # operator-driven theme registration
│   │   ├── themecontract/     # contracts/theme loader and validators (Go)
│   │   ├── buildrunner/       # build tasks, trusted adapter, task workspaces,
│   │   │                      # preview origins
│   │   ├── publish/           # release sealing, atomic live pointer, published origins
│   │   ├── mediatask/         # album indexing through the theme's Node script
│   │   ├── caddyconfig/        # Caddy adapter JSON inventory (read-only)
│   │   ├── caddyinventory/     # fixed read capability runner
│   │   ├── edge/              # managed Caddy snippet rendering/applying
│   │   ├── wa/                # WebAuthn bridge (compatibility path)
│   │   └── testauth/          # test-only synthetic authenticator
│   ├── config.example.env     # environment template without real credentials
│   └── go.mod / go.sum        # module github.com/skyrocketingHong/FuyaoManager/backend
├── frontend/                  # React + antd 6 console (own package-lock.json)
│   ├── src/lib/api/           # typed management/admin clients + generated schema.d.ts
│   ├── src/lib/admin/         # cancellable queries, mutation guards, audit labels
│   ├── src/lib/sites/         # site business modules (race-safe detail, editors)
│   ├── src/lib/themes/        # theme-selection store (catalog, If-Match, 412)
│   ├── src/lib/editing/       # EditableDoc state machine + unsaved-changes guard
│   ├── src/lib/navigation/    # single navigation metadata (routes, menu, scope)
│   ├── src/lib/webauthn/      # passkey ceremonies, JSON conversion, error mapping
│   ├── src/lib/i18n/          # zh-CN / en-US dictionaries
│   ├── src/lib/theme/         # persisted light/dark/system mode, accent + semantic tokens
│   ├── src/lib/session/       # session state: anonymous/restricted/full/error
│   ├── src/components/        # AppearanceProvider, PageHeader(+PageTabs), Panel/
│   │                          # TablePanel, DataTable, PageLoading, StatusTag,
│   │                          # RouteErrorPage, admin/UserDrawer, layout/ (AppShell, TopBar,
│   │                          # SidebarInfo/Actions/Nav/Foot)
│   ├── src/styles/            # base / shell / sidebar / statusbar / pages / appearance CSS (semantic
│   │                          # variables)
│   ├── src/pages/             # login, passkey binding, users, logs, account and appearance settings,
│   │                          # themes catalog, sites, per-site surfaces
│   └── tests/                 # vitest suites (jsdom)
├── contracts/
│   ├── openapi/openapi.yaml  # authoritative management HTTP contract
│   └── theme/                   # authoritative theme contracts
│       ├── manifest.schema.json / build-input.schema.json / release.schema.json
│       ├── README.md / README_ZH.md
│       └── examples/            # public-safe positive/negative fixtures
├── deploy/
│   └── fuyao-admin/          # server deployment: bilingual README, systemd unit,
│                             # config sample, Caddy apply helper, public static
│                             # page (static-page/, favicon.svg + same-origin
│                             # branding), public Caddy vhost template (caddy/)
└── docs/
    ├── ROADMAP.md / ROADMAP_ZH.md          # iteration ledger (bilingual)
    ├── CADDY_INTEGRATION.md / CADDY_INTEGRATION_ZH.md
    ├── SOURCE_MIGRATION.md                 # extraction record + commit map pointer
    ├── UI_REFERENCE.md / UI_REFERENCE_ZH.md # reused console modules and boundaries
    ├── ADMINISTRATION.md / ADMINISTRATION_ZH.md # account, audit and appearance workflows
    └── source-mapping/commit-map-from-fuyaohomepage.txt
```

## Runtime data layout

Default runtime data lives under `<repo>/data/` (overridable per root via
`FUYAO_ADMIN_*`): `data/builds/<taskId>/` (snapshot + artifacts + logs),
`data/sites/<siteId>/{content,config}`, `data/releases/<siteId>/<buildId>/`.
Deployments set these to the persistent data directory
(`/srv/fuyao-admin/data/...`); nothing under `data/` is version controlled.
