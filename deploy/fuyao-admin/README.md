# fuyao-admin server deployment

English | [简体中文](./README_ZH.md)

Deployment artifacts for the Fuyao management platform on the target server. The deployment is deliberately file-based (build artifacts plus a controlled source package, uploaded over SSH) — no git push, no registry.

## Layout on the server

```text
/srv/fuyao-admin/
├── current -> releases/<commit-sha>     # the deployed version
├── releases/<commit-sha>/               # program: binary + console + contracts
│   ├── fuyao-admin                      # linux/amd64 binary
│   ├── frontend/                        # built React console (vite dist)
│   └── contracts/                       # management HTTP and theme contracts
├── themes/<theme-id>/<version>/         # independent theme package, site/ and deps
├── static-page/                         # public page; no management bundle
├── backups/                             # access-restricted program/DB/config backups
├── data/                                # persistent runtime data (never inside releases/)
│   ├── sites/                           # per-site content/config trees
│   ├── builds/                          # build task workspaces, snapshots, artifacts
│   └── releases/                        # frozen release dirs + per-site `current` pointer
└── etc/
    ├── db.env                           # FUYAO_ADMIN_DATABASE_URL (mode 600)
    └── config.env                       # remaining environment (mode 600)
```

Program and data are separated: replacing `current` never touches content, releases, or the database.

## Topology (deployed: HTTPS domain)

- The production console lives at `https://manager.fuyaoskyrocket.ing`. The management API listens on loopback only (`127.0.0.1:8787`); Caddy terminates TLS and reverse-proxies.
- The **public** vhost for that domain serves only the standalone static page in `static-page/` — never the console bundle, and no proxying: API and preview paths get an explicit 404. The template is `caddy/manager.public.caddy.example`; it lives outside the platform-managed snippet directory (which the backend fully re-renders).
- The **private** vhost (same hostname, bound to the private-network address) reverse-proxies to the loopback listener; both vhosts share the hostname's automatically issued certificate. Its configuration, the split-DNS setup, and the full cutover runbook are operator-local documents by policy (not committed to the repository); see the deployment runbook kept with the server's ops notes.
- Private build previews keep their own bind/port/URL rules (platform-allocated ports on the private address, default `20000-20100`); the management domain is not a blanket replacement for them.
- Published sites are served by the system Caddy through the platform-managed snippet at `/etc/caddy/fuyao-admin/fuyao-admin.caddy` (imported from `/etc/caddy/Caddyfile`). Private entries bind exactly the private address; public entries bind all interfaces and are announced with the public IP.
- Reload privilege: the service user `fuyao-admin` may run exactly one root command (`/usr/local/sbin/fuyao-caddy-apply`, validate + reload with fixed arguments) through sudoers; the service itself never runs as root.
- Passkeys require the domain relying party (`FUYAO_ADMIN_RP_ID=manager.fuyaoskyrocket.ing`, HTTPS origins); the one-time CLI code is the identity-proofing and recovery entry.

## Files in this directory

- `fuyao-admin.service` — systemd unit (low-privilege user, cgroup memory limit, NODE_OPTIONS heap tuning for the 2 GB host).
- `config.example.env` — public production environment template (HTTPS domain topology); private bind addresses and preview URLs come from operator-local configuration and retain their existing values on upgrade.
- `fuyao-caddy-apply` — the operator-controlled validate+reload helper installed at `/usr/local/sbin`.
- `static-page/` — the standalone public landing page (deployed to its own directory, e.g. `/srv/fuyao-admin/static-page`; not part of the console bundle).
- `caddy/manager.public.caddy.example` — the public vhost template (static page only, explicit 404 for API/preview paths).

## Deploy / upgrade procedure

1. Back up the database, current program and configuration, record the current pointer, and build a separate release from the repository root:
   ```bash
   manager_stage="$(mktemp -d "${TMPDIR:-/tmp}/fuyao-manager-release.XXXXXX")"
   mkdir -p "$manager_stage/frontend" "$manager_stage/contracts"
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C backend build -o "$manager_stage/fuyao-admin" ./cmd/fuyao-admin
   npm --prefix frontend run build
   cp -R frontend/dist/. "$manager_stage/frontend/"
   cp -R contracts/. "$manager_stage/contracts/"
   cp -R deploy/fuyao-admin/static-page "$manager_stage/static-page"
   chmod 0755 "$manager_stage/fuyao-admin"
   ```
   Run `npm --prefix frontend install` first for a fresh checkout or changed dependencies. Stop packaging on any failure and keep the live pointer unchanged. Remove the temporary directory when verification is finished.
2. Keep every registered theme version under `/srv/fuyao-admin/themes/<theme-id>/<version>/`, including its source, package.json, lockfile and installed dependencies. Retain versions referenced by sites. Install dependencies as the service user, or restore ownership after a root install.
3. Upload into a new `/srv/fuyao-admin/releases/<sha>/`, verify the binary digest and executable mode, and install the public page into its separate `static-page/` directory. When retaining an old environment file, repoint `FUYAO_ADMIN_THEME_CONTRACTS` to the new release's `contracts/theme`, not the pre-split `theme/contracts/theme`.
4. Stop the management service. From a new-release working directory accessible to the service user, load the operator environment and run the new binary's `migrate`; then run `register-theme` with explicit `--contracts` and each theme package's `site/` path. Only after success, atomically replace `current` and start the service. Do not start a binary requiring unapplied migrations or invoke the service user's CLI from an inaccessible root working directory.
5. Merge the independent entrance configuration, adapt and validate the complete Caddyfile as the actual Caddy reload user, check that published-site listeners are included, then reload. Grant that user only the required read/traverse and default inherited ACLs on the managed directory: an unreadable import glob may otherwise be silently omitted, so a successful root-only check is insufficient. The public IPv4 is NAT-mapped and cannot be used as a local bind address; the private proxy entrance must bind explicitly. Private DNS and full operator commands stay in the local runbook.
6. Verify service readiness, trusted TLS, private authentication state and public isolation before deleting old programs. Relocate registered theme sources and historical workspace dependency links first; stop cleanup if lockfiles or installed-dependency records differ. Preserve site content, snapshots and business releases under `data/`.

Production was switched to application commit `dc0a4ad` on 2026-11-12,
with migration 0012 applied and old program directories removed. See M5
in the roadmap for actual verification and remaining manual acceptance.

Rollback: restore the old program from its restricted backup archive if its directory was removed, then restore the corresponding program pointer, environment and entrance configuration together. A pointer change does not downgrade the database. Check migration and authentication semantics before rolling back; restore the last good configuration rather than repeatedly restarting a failing version.

## Operations

```bash
systemctl status fuyao-admin          # state
journalctl -u fuyao-admin -f          # logs
systemctl restart fuyao-admin         # restart (build executor recovers interrupted tasks)
fuyao-admin login-code --username fuyao   # identity-confirmation / recovery code (on the server)
fuyao-admin revoke-sessions --username fuyao
```
