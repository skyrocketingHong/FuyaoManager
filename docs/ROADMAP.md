# Management Platform Roadmap (MVP Iteration Ledger)

English | [简体中文](./ROADMAP_ZH.md)

Agile ledger for the content management platform. Each work package lands a running, verified increment; nothing is listed as done before it is implemented and verified.

## Done

1. **Backend MVP (local development)** (`admin/backend`): PostgreSQL + SQL migrations, passkey-only WebAuthn auth (discoverable credentials, UV required), CLI (`migrate`, `serve`, `bootstrap-admin`, `create-site`), cookie sessions with CSRF/Origin enforcement, site list/detail/settings with transactional ETag concurrency. Loopback-only. Contract: `contracts/openapi/management.yaml`.
2. **Hardening pass**: transactional first-enrollment closure (advisory lock; concurrent completions admit exactly one), permit expiry re-checked at verify time, full-ETag If-Match binding (wildcard rejected), stable per-session CSRF token (multi-tab safe), request limits and single-object JSON on verify paths, loud TTL validation, CORS ETag exposure.
3. **Admin frontend MVP** (`admin/frontend`): enrollment, login/logout, site list/detail/settings with dirty-field patches, 412 conflict recovery, and viewer read-only; generated API types with drift check; zh-CN/en-US with Crossfade; day/night themes; same-origin dev proxy topology (port 5174 pinned, loopback only). The non-browser protocol integration (backend serve + dev proxy + cookie/origin flows) is exercised by this package's verification; real platform-authenticator ceremonies and visual acceptance remain with the user.

## Next (in order, one package at a time)

4. Real-browser regression with actual platform authenticators (Touch ID / hardware keys) and visual acceptance, driven by the user.
5. Articles: list, edit, publish; markdown pipeline reuse from `site/` scripts.
6. Pages and layout modules; site-scoped content boundaries.
7. Multi-site fan-out and membership management UI.
8. Server-side secret configuration, GitHub source updates, build/deploy and rollback entries with task state and failure records.
9. Migrate remaining `site/scripts` responsibilities (media processing, validation, watchers, external data cache, versioning, migration, release) into the backend; retire old entry points only after parity acceptance.
10. Tailscale private-network exposure and the 30-minute public gate; production deployment path.

Not planned for MVP: importing production data, migrating real keys, pulling production deployments.
