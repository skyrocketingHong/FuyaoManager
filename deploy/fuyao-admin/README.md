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

## Topology

- The management API + console listen on the server's Tailscale address only (`100.117.154.115:8787`); public-IP connections do not reach the service.
- Private build previews bind the same Tailscale address with platform-allocated ports (default range `20000-20100`); preview URLs carry the Tailscale base, never `127.0.0.1`.
- Published sites are served by the system Caddy through the platform-managed snippet at `/etc/caddy/fuyao-admin/fuyao-admin.caddy` (imported from `/etc/caddy/Caddyfile`). Private entries bind exactly the Tailscale address; public entries bind all interfaces and are announced with the public IP.
- Reload privilege: the service user `fuyao-admin` may run exactly one root command (`/usr/local/sbin/fuyao-caddy-apply`, validate + reload with fixed arguments) through sudoers; the service itself never runs as root.

## Files in this directory

- `fuyao-admin.service` — systemd unit (low-privilege user, cgroup memory limit, NODE_OPTIONS heap tuning for the 2 GB host).
- `config.example.env` — environment template matching the live deployment.
- `fuyao-caddy-apply` — the operator-controlled validate+reload helper installed at `/usr/local/sbin`.

## Deploy / upgrade procedure

1. Build locally from the repository root:
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C admin/backend build -o <stage>/fuyao-admin ./cmd/fuyao-admin
   npm run admin:build && cp -R admin/frontend/dist <stage>/frontend
   rsync -a --exclude node_modules --exclude .fuyao --exclude build --exclude .svelte-kit --exclude .DS_Store site/ <stage>/theme/site/
   cp package.json package-lock.json .npmrc <stage>/theme/
   cp -R contracts/theme <stage>/theme/contracts/theme
   ```
2. Upload to `/srv/fuyao-admin/releases/<sha>/`, `chown -R fuyao-admin:fuyao-admin`, run `npm ci` as the service user (or `chown -R` after a root install).
3. Repoint `current`, `systemctl restart fuyao-admin`.
4. Re-register the first theme (identity-identical registration refreshes the stored source path):
   ```bash
   cd /srv/fuyao-admin/current && . /srv/fuyao-admin/etc/db.env && export FUYAO_ADMIN_DATABASE_URL
   ./fuyao-admin migrate                 # new migrations, if any
   ./fuyao-admin register-theme --contracts theme/contracts/theme theme/site
   ```
5. Verify: `systemctl is-active fuyao-admin`, Tailscale HTTP on the console, public-IP probes fail, `journalctl -u fuyao-admin` shows `edge applied`.

Rollback: repoint `current` to the previous release directory and restart; runtime data and the database schema stay compatible within a migration series (downgrades past an applied migration are not supported).

## Operations

```bash
systemctl status fuyao-admin          # state
journalctl -u fuyao-admin -f          # logs
systemctl restart fuyao-admin         # restart (build executor recovers interrupted tasks)
fuyao-admin login-code --username fuyao   # per-login one-time code (on the server)
fuyao-admin revoke-sessions --username fuyao
```
