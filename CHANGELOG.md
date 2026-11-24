English | [简体中文](./CHANGELOG_ZH.md)

# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the
product uses the `Y.M.P` version scheme. API and theme-contract versions
are maintained separately.

## 0.10.1 - 2026-11-24

- The sidebar follows the robot platform: the card with site counts, and with a site's slug and role, is gone, and a site scope heads its menu with the site name. Row height, icon slot, glyph and brand row match the platform.
- The icon rail is the expanded sidebar's first 52px, so entries no longer move between the collapsed sidebar, the expanded sidebar and the floating menu.

## 0.10.0 - 2026-11-24

- Desktop sidebar modes: expanded (208px), icon rail (52px) and hidden, saved per browser. The rail keeps icons and group titles of up to two lines; hovering or focusing it floats the complete menu, and Escape dismisses it. The hidden mode restores from the left end of the operation bar.
- Display settings offer a personal content background: 90% of the theme background mixed with 10% of the chosen color.
- Shared controls follow the robot platform at commit `7ddbaf0e`: status tags wrap and mix into the surface behind them, loading states carry a label by default, the primary button has a press scale, interface headings use a 20px and 16px scale, and workspace and table scroll fades ease in and stop under forced colors.
- A page change cross-fades from the outgoing page to the incoming one; tab and filter changes, replace navigations and reduced motion switch instantly.
- Inputs and selects are 34px high on desktop screens, and form grids keep at most two columns with a 20px gap.
- The top bar's version bubble morphs open from its capsule and back on close; Escape closes it and returns focus to the capsule.
- From a 720px container the operation bar is a single row: the page area scrolls sideways under a vertical wheel or trackpad scroll, with a fade at each edge that still hides content, while navigation and the session controls stay in place. Narrower containers keep two rows.
- The sign-in and passkey pages carry a continuous aurora background that can be paused and stays still under reduced motion.
- Frontend relative imports point only at the same or a lower layer, and a test keeps the page registry aligned with the navigation metadata.
- A page that fails to render reports the error inside the workspace with a retry, while the sidebar, operation bar and session stay mounted.
- Signing out with unsaved changes asks for confirmation before the session is cleared.

## 0.9.4 - 2026-11-22

- Browse albums in a photo grid, sortable list, or horizontal gallery. Switch years, search photos, preview thumbnails and upload several originals at once.
- Set covers and edit photo details from the gallery. Keep local edits when switching views or merging concurrent changes. Album thumbnails follow the existing content permissions. Photo cards add a staggered entrance and CSS book turn. The horizontal gallery supports keyboard navigation and a static grid for reduced motion.

## 0.9.2 - 2026-11-21

- Share batch loading across posts, user management, and operation logs. Scope automatic loading to each table, ignore expired observer callbacks, and make table scrolling keyboard-accessible.

## 0.9.1 - 2026-11-16

- Resolve frontend dependencies from compatible version ranges without an npm lockfile; update the installation, CI and version synchronization workflows.

## 0.9.0 - 2026-11-15

- Store blog articles, categories, tags and images in PostgreSQL. The first startup imports existing content and preserves publication paths and unmodeled frontmatter; builds generate private Markdown and media snapshots.
- Use a stable article ID and an editable, scope-unique slug. The article list displays its categories and slug; category and tag management have dedicated routes.
- Rename or remove tags and their article references in one transaction, including Unicode names. The split editor and preview now have matching heights.
- Requires database migration `0021` and a backup before the operator-controlled upgrade. Application rollback to the previous file-based version is unavailable; published website releases remain unchanged.

## 0.8.1 - 2026-11-15

- Fixed the Markdown editor width and synchronized split-view scrolling and cursor position. Expanded the formatting toolbar.
- Unified refresh and retry actions and made the recovery-code form optional on initialized installations.

## 0.8.0 - 2026-11-15

- Added a responsive blog editor with Markdown preview, formatting controls, in-page anchors, a generated table of contents and a unified taxonomy workspace.
- Stored blog images in PostgreSQL, adopting existing scoped images automatically and freezing them at their public paths for builds. Schema migration `0020` is required; database backups include these images.
- Added UTC article timestamp storage, configurable display time zones and formats, continuous table loading, and three-state sorting. The top bar now groups page controls and update checks use notifications.

## 0.7.0 - 2026-11-15

### Added

- GitHub CI release bundles containing the Linux application, frontend and independent update supervisor, with exact commit/run identity and per-file SHA-256 verification.
- Administrator platform updates page: check successful builds, deploy a selected version, switch main/dev channels, configure automatic updates and pinning, restore the previous verified program, and follow persistent task receipts and logs.
- Root-owned systemd update supervisor, fixed Unix-socket proxy, database/configuration backup before activation, running-commit health checks, failure rollback and interrupted-switch recovery.
- One version synchronization command for npm manifests and the default Go build version; optional real-theme integration remains independently selectable.

### Changed

- Production application updates consume CI-built packages instead of compiling source on the server. Automatic activation requires an unchanged database schema.

## 0.6.1 - 2026-11-14

### Changed

- Status colors now follow the day/night palettes: the antd success, warning and error base colors derive from the theme palette, so badges, progress and alerts no longer render with light-mode values in dark mode.
- Token cleanup: removed CSS variables that were defined but never consumed (`--good`, `--danger`, `--elevated`, `--primary-fill`, `--motion-fast`, and others), merged `--surface-high` into `--surface`, and routed radii, font sizes and spacing through the existing tokens. Dropped CSS overrides that duplicated antd component tokens (table header and hover backgrounds, Segmented track background) and the hand-written dropdown shadow.
- The top-bar user menu presents identity and passkey state as non-interactive group captions instead of disabled menu items; the user button drops its redundant tooltip.

### Fixed

- Site lists no longer show an unrelated configuration-source string in the visibility and setup columns for Caddy sites; they show a dash instead.
- The releases page consolidates disabled-state explanations on the publish button (viewer role, no publishable build) instead of permanent alert banners; publish-failure toasts map stage names and missing items to readable copy; the release status column uses a dedicated label.
- Builds no longer confirm "new build" through a Popconfirm; failure stages render mapped copy; the ID and actions columns carry proper titles.
- Album alt-text states use themed status tags; move-up, move-down and remove buttons gained accessible names; the edit dialog aligns with the shared form styles; empty states use Empty.
- Friends and footprints remove buttons are unified as "Remove"; footprint field placeholders and labels moved into the locale files so the English UI no longer leaks Chinese text; text inputs gained aria-labels.
- Post slug and body labels and the categories placeholder moved into the locale files; the actions column carries a title.
- The theme catalog shows the registration command itself instead of a hover-only hint; the account page states the username rules once instead of tooltip plus repeated line; API catalog rows no longer duplicate the version note shown in the expanded details.
- The content-sources disclaimer is a quiet line instead of an alert; the overview theme panel is renamed "Theme and delivery"; the site detail page drops the entry-point panel that duplicated the sidebar, folds timestamps into the state panel, and adds a loading state to setup retry.
- Locale cleanup: removed 15 unreferenced keys, aligned status and role label granularity and casing between locales, and rewrote the initialization copy as positive steps.

## 0.6.0 - 2026-11-13

### Added

- Independent platform blogs, albums, places and friends, with per-site platform or independent source selection and real content statistics.
- Directory-backed managed static websites can select registered themes, edit theme parameters and full website configuration, build, preview and publish through explicit workspace capabilities.
- Equivalent Caddy source registration and root-sealed theme publication, preserving complex site rules and coordinating activation with database commit and failure compensation.
- Latest daily Cloudflare statistics from an operator-bound existing read-only source, with explicit cache status and unavailable states.
- Operator real-data import, independent content scopes and frozen media tasks (migrations 0018 and 0019).

### Fixed

- Caddy deep links remain available across content modes. Native Caddy sites display real release status without legacy visibility/edge controls.
- Preview and publication serve the same frozen images and album media; production never falls back to live content. CI previews retain bundled artifacts.
- Website configuration preserves unmodeled YAML fields and exposes previously missing service and URL fields.
- Protected management endpoints are explicitly excluded from website conversion.

## 0.5.0 - 2026-11-13

### Added

- Separate platform-managed websites and Caddy inventory on the home page, with the Simple Icons Caddy brand mark.
- Convert operator-approved static websites with real directories to platform configuration management while preserving their original rules and contents.
- Persisted Caddy drafts and applied history, exact-revision validation, controlled deployment and rollback through an independent root-owned capability. Strong ETags, source-drift checks and administrator/owner authorization protect changes.

### Fixed

- The Caddy page now performs explicit configuration actions instead of only displaying inventory metadata.
- The platform-home sidebar button has symmetric vertical spacing, retaining the reference console's alignment.

## 0.4.0 - 2026-11-13

### Added

- Real Caddy inventory synchronization, per-site type and permission boundaries, and static/redirect configuration views; existing complex source rules stay read-only.
- Platform API directory adapted from the reference console; authoritative contract renamed to `contracts/openapi/openapi.yaml`.
- Independent-host path previews with revocable leases, TLS-port mapping, and hardened HTTPS cookies.

### Fixed

- Closing a preview wins over pending issuance, and old leases cannot reopen it.
- Logs and sidebar reuse reference component calls and CSS; group headings align with icons.

## 0.3.0 - 2026-11-13

### Added

- Full administrator user management: account creation and once-returned registration/recovery codes, profile and platform roles, lifecycle controls, per-site access, active-session management and passkey revocation.
- Persistent operation/authentication audit (migrations 0014 and 0015), with operator, operation, outcome, keyword and time-range filters, pagination and canonical account/change/credential detail. Account changes and their success event commit atomically; secrets and request bodies are excluded.
- Console appearance settings adapted from the reference platform: light/dark/system modes, twelve accent presets, custom colors, reset, legacy-preference migration and synchronized tabs.

### Fixed

- Sidebar actions and menus share icon/text alignment and consistent horizontal insets; site workspaces explicitly label the platform return action.
- Credential revocation rechecks the exact verified key at session insertion, preventing late verified assertions from recreating sessions. Account/query generation guards prevent stale administrative data and codes from appearing after an account switch.
- Closing a once-returned-code dialog immediately unmounts its contents.

## 0.2.1 - 2026-11-13

### Fixed

- Site-configuration loading no longer changes the hook count and crashes with React #310. Header hooks remain unconditional; failed loads and route errors offer recovery actions.
- Layered antd styles restore the reference console's shared CSS precedence. Sidebar regions, top bar, Lucide icons and compact tables reuse the reference implementation; the platform-home action no longer duplicates a menu entry.
- Locale changes update page text without losing drafts. Configuration saves lock editing and install canonical server values. Site-list caches reject previous-account responses.
- Settings-view tests unmount explicitly, canceling delayed antd updates before the test environment closes.

## 0.2.0 - 2026-11-13

### Added

- Account-management backend foundation: platform admin/member roles,
  guarded account lifecycle and onboarding codes, credential revocation,
  and last-administrator and active-site-owner protection. The management
  HTTP/UI and audit-log feature remain pending.
- Route-aware `Fuyao Manager - <page>` titles and a shared SVG favicon for
  the console and independent public entrance.
- Self-service account rename: `PATCH /api/v1/account/profile` renames the
  caller's own account atomically (a collision answers 409
  `username_taken`); an account-settings page is reached from the account
  dropdown, and passkeys and login codes keep working across the rename.
- Run-isolated deployment default: with `FUYAO_ADMIN_DEPLOYMENT_ENABLED`
  off (the default), sites, content, builds, and private previews work
  while publishing, rollback, visibility changes, and Caddy applies answer
  `503 deployment_disabled`; the edge status reports enabled/isolated
  facts and the live-pointer state.
- Deterministic runtime roots: the repository root is discovered upward
  from the working directory (or configured explicitly), the three runtime
  roots must resolve to independent real directories, and provisioning
  never overwrites existing site files.

### Changed

- Platform home and site overview now have separate fixed sidebar actions;
  generic sidebar components and shared surfaces reuse the robot console
  patterns.
- Closing a preview revokes its tokens, stops its listener, releases its
  port, and ends its page lifecycle. Instance-specific probes prevent an
  old page from surviving port reuse; preview service-worker registration
  is blocked.
- Session handling: a business-request 401 expires the stale session at
  the client boundary so routing reaches the login screen; ceremony
  failures and network errors never sign the user out, and generation
  guards keep stale responses from overwriting a newer login.
- Console surfaces were aligned with the shared console patterns (panel
  anatomy, table and control density, single-surface tables), and the
  public static page is retitled "Fuyao Manager - 访问说明" with a
  same-origin favicon.

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
