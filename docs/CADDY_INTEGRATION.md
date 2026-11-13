English | [简体中文](./CADDY_INTEGRATION_ZH.md)

# Caddy sites and preview entry

An operator-selected fixed read capability runs Caddy's own adapter. The platform parses the real JSON and registers sites without changing website directories, theme sources or existing Caddy rules. Newly discovered sites belong to the synchronizing administrator; other accounts use explicit site memberships. Manager/control hostnames are excluded.

Inventory classifies static, redirect, proxy and other configuration, retaining hosts, listeners, recognizable document roots and redirect targets. Complex matchers, rewrites, authentication and file hiding remain read-only; composite configurations are never reduced to a lossy template. Missing source routes are marked absent rather than deleting authorization/history. Imported private/ready values are registration metadata, not network reachability or health claims.

Theme workflows retain content, theme parameters, builds and publishing. External static sites show standard configuration, and redirects show their targets. Backend guards also reject content/build calls for non-theme sites. A Caddy file_server does not identify a registered theme.

The manager runs as an ordinary user. Only root-owned, argument-free read/apply helpers are granted; actual commands run as caddy, without a general root shell. The Caddy admin API uses a filesystem-protected Unix socket. Synchronization requires a full admin session and CSRF, with an in-transaction operator recheck. Raw configuration and certificates are not exposed over HTTP.

Preview URLs use `https://preview.manager.fuyaoskyrocket.ing/formal-host/original-path?lease=short-capability`, falling back to the site slug when no formal hostname exists. This separate HTTPS host receives no manager-session cookie. A controlled wrapper embeds unchanged artifacts on a separate HTTPS port, preserving production routing semantics. JavaScript is not rewritten and publishing base paths are not changed. An operator-configured port offset separates TLS listeners from site publication ports.

Closing cancels pending issuance, revokes grants, stops backend listeners and releases allocations. Reload only reauthorizes the same active incarnation; closed wrappers return 410 and cannot recreate it. Caddy maps closed upstreams to non-cacheable 410. Preview access logging is skipped and default logs filter request URIs and headers so capabilities are not retained.

HTTPS session/auth-transaction cookies use `__Host-`, Secure, HttpOnly, Path=/ and no Domain. Legacy names and duplicate authentication cookies are rejected. Existing users sign in again after upgrade; account and passkey records remain intact. HTTP localhost development stays compatible.

The API page reads authenticated `GET /api/v1/openapi`, backed by `contracts/openapi/openapi.yaml`. It displays only declared version/authorization metadata.
