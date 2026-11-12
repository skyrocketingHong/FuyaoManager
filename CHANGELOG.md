English | [简体中文](./CHANGELOG_ZH.md)

# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the
project uses `MAJOR.MINOR` marketing versions (`MAJOR.MINOR.PATCH`
internally).

## 0.1.0 - 2026-11-12

### Added

- Project split: FuyaoManager extracted from FuyaoHomepage with the
  platform development history preserved (`docs/SOURCE_MIGRATION.md`
  records the extraction and the commit mapping).
- Standalone layout: `backend/` (Go module
  `github.com/skyrocketingHong/FuyaoManager/backend`), `frontend/`
  (`fuyao-manager-frontend`, own lockfile), `contracts/` (management
  OpenAPI + authoritative theme contracts), `deploy/fuyao-admin/`, and
  bilingual root documentation.
- Bundled contract default: `serve` resolves `contracts/theme` from the
  repository root when `FUYAO_ADMIN_THEME_CONTRACTS` is unset.
- Runtime data roots default to `<repo>/data/{builds,sites,releases}`
  instead of the former in-repo theme working tree.
- Production builds seed an empty site favicon directory from the theme's
  own `static/favicon` inside the task's verified source copy.
- Bundled contract-valid fixture theme (`backend/internal/httpapi/testdata/theme`)
  powering catalog, selection, registration, publish-pipeline, and
  recovery tests without a theme checkout.
- Cross-repository real-theme integration entry: `FUYAO_TEST_THEME_ROOT`
  activates the six real-Node-build tests; CI gains a manual
  external-theme integration job.

### Changed

- Console branding, OpenAPI title, and documentation renamed to
  FuyaoManager; the `fuyao-admin` CLI binary, `FUYAO_ADMIN_*` variables,
  service name, and deployment paths are unchanged.

### Compatibility

- Database schema and migrations unchanged; theme identity
  (`fuyao-homepage`, `node22-sveltekit-static`) and contract protocol
  identifiers unchanged.
