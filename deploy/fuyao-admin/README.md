# fuyao-admin server deployment

English | [简体中文](./README_ZH.md)

Deployment artifacts for the Fuyao management platform on the target server. The deployment is deliberately file-based (build artifacts plus a controlled source package, uploaded over SSH) — no git push, no registry.

## Layout on the server

```text
/srv/fuyao-admin/
├── current -> releases/<commit-sha>     # the deployed version
├── releases/<commit-sha>/               # program: binary + console + theme source
│   ├── fuyao-admin                      # linux/amd64 binary
│   ├── frontend/                        # built React console (vite dist)
│   └── theme/                           # controlled source package: package.json,
│                                        # lockfile, site/ (first theme), contracts/theme/
│       └── node_modules/                # npm ci output (fuyao-admin-owned)
├── data/                                # persistent runtime data (never inside releases/)
│   ├── sites/                           # per-site content/config trees
│   ├── builds/                          # build task workspaces, snapshots, artifacts
│   └── releases/                        # frozen release dirs + per-site `current` pointer
└── etc/
    ├── db.env                           # FUYAO_ADMIN_DATABASE_URL (mode 600)
    └── config.env                       # remaining environment (mode 600)
```

Program and data are separated: replacing `current` never touches content, releases, or the database.

## Topology (target: HTTPS domain)

- The production console lives at `https://manager.fuyaoskyrocket.ing`. The management API listens on loopback only (`127.0.0.1:8787`); Caddy terminates TLS and reverse-proxies.
- The **public** vhost for that domain serves only the standalone static page in `static-page/` — never the console bundle, and no proxying: API and preview paths get an explicit 404. The template is `caddy/manager.public.caddy.example`; it lives outside the platform-managed snippet directory (which the backend fully re-renders).
- The **private** vhost (same hostname, bound to the private-network address) reverse-proxies to the loopback listener; both vhosts share the hostname's automatically issued certificate. Its configuration, the split-DNS setup, and the full cutover runbook are operator-local documents by policy (not committed to the repository); see the deployment runbook kept with the server's ops notes.
- Private build previews keep their own bind/port/URL rules (platform-allocated ports on the private address, default `20000-20100`); the management domain is not a blanket replacement for them.
- Published sites are served by the system Caddy through the platform-managed snippet at `/etc/caddy/fuyao-admin/fuyao-admin.caddy` (imported from `/etc/caddy/Caddyfile`). Private entries bind exactly the private address; public entries bind all interfaces and are announced with the public IP.
- Reload privilege: the service user `fuyao-admin` may run exactly one root command (`/usr/local/sbin/fuyao-caddy-apply`, validate + reload with fixed arguments) through sudoers; the service itself never runs as root.
- Passkeys require the domain relying party (`FUYAO_ADMIN_RP_ID=manager.fuyaoskyrocket.ing`, HTTPS origins); the one-time CLI code is the identity-proofing and recovery entry.

## Files in this directory

- `fuyao-admin.service` — systemd unit (low-privilege user, cgroup memory limit, NODE_OPTIONS heap tuning for the 2 GB host).
- `config.example.env` — production environment template (HTTPS domain topology).
- `fuyao-caddy-apply` — the operator-controlled validate+reload helper installed at `/usr/local/sbin`.
- `static-page/` — the standalone public landing page (deployed to its own directory, e.g. `/srv/fuyao-admin/static-page`; not part of the console bundle).
- `caddy/manager.public.caddy.example` — the public vhost template (static page only, explicit 404 for API/preview paths).

## Deploy / upgrade procedure

1. Build the platform release from this repository's root:
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C backend build -o <stage>/fuyao-admin ./cmd/fuyao-admin
   npm --prefix frontend ci && npm --prefix frontend run build && cp -R frontend/dist <stage>/frontend
   cp -R contracts <stage>/contracts
   ```
2. Add the first theme package from its own repository checkout
   (FuyaoHomepage):
   ```bash
   rsync -a --exclude node_modules --exclude .fuyao --exclude build --exclude .svelte-kit --exclude .DS_Store \
     /path/to/FuyaoHomepage/site/ <stage>/theme/site/
   cp /path/to/FuyaoHomepage/package.json /path/to/FuyaoHomepage/package-lock.json /path/to/FuyaoHomepage/.npmrc <stage>/theme/
   ```
3. Upload to `/srv/fuyao-admin/releases/<sha>/`, `chown -R fuyao-admin:fuyao-admin`, run `npm ci` inside `<stage>/theme` as the service user (or `chown -R` after a root install).
4. Repoint `current`, `systemctl restart fuyao-admin`.
5. Re-register the first theme (identity-identical registration refreshes the stored source path):
   ```bash
   cd /srv/fuyao-admin/current && . /srv/fuyao-admin/etc/db.env && export FUYAO_ADMIN_DATABASE_URL
   ./fuyao-admin migrate                 # new migrations, if any
   ./fuyao-admin register-theme --contracts contracts/theme theme/site
   ```
6. Verify: `systemctl is-active fuyao-admin`; the public domain shows only the static page (API paths 404); the private network reaches the console over HTTPS (passkey bind and sign-in); public-IP probes to the management port fail; `journalctl -u fuyao-admin` shows `edge applied`.

The currently deployed server still runs the pre-split single-repository
release (theme contracts and repo root under `<current>/theme`); switch it
to this layout only when the roadmap's deployment cutover is executed.

Rollback: repoint `current` to the previous release directory and restart; runtime data and the database schema stay compatible within a migration series (downgrades past an applied migration are not supported).

## Operations

```bash
systemctl status fuyao-admin          # state
journalctl -u fuyao-admin -f          # logs
systemctl restart fuyao-admin         # restart (build executor recovers interrupted tasks)
fuyao-admin login-code --username fuyao   # identity-confirmation / recovery code (on the server)
fuyao-admin revoke-sessions --username fuyao
```
