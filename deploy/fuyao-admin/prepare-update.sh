#!/bin/sh
# Installed once as root: environment files and the command cannot be changed
# by the application account. CI archives never supply root shell commands.
set -eu
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
candidate=${1:?candidate release required}
case "$candidate" in /srv/fuyao-admin/releases/*) ;; *) exit 2 ;; esac
[ "$(dirname "$candidate")" = /srv/fuyao-admin/releases ]
[ ! -L "$candidate" ]
[ "$(stat -c '%u:%a' "$candidate")" = 0:755 ]
cd /srv/fuyao-admin
set -a
. /srv/fuyao-admin/etc/db.env
. /srv/fuyao-admin/etc/config.env
set +a
export FUYAO_ADMIN_REPO_ROOT="$candidate"
export FUYAO_ADMIN_THEME_CONTRACTS="$candidate/contracts/theme"
export FUYAO_ADMIN_UI_DIR="$candidate/frontend/dist"
backup="/srv/fuyao-admin/backups/update-$(date +%Y%m%d-%H%M%S)-$$"
install -d -m 0700 "$backup"
export PGDATABASE="$FUYAO_ADMIN_DATABASE_URL"
pg_dump --format=custom --file="$backup/database.dump"
cp -a /srv/fuyao-admin/etc "$backup/etc"
readlink -f /srv/fuyao-admin/current > "$backup/previous"
# Run the candidate CLI with service privileges. No deployment package code
# is executed with root privileges during preparation.
runuser -u fuyao-admin -- "$candidate/fuyao-admin" version
runuser -u fuyao-admin -- "$candidate/fuyao-manager-updater" version
runuser -u fuyao-admin -- "$candidate/fuyao-admin" migrate
psql -XAt -v ON_ERROR_STOP=1 \
  -c 'SELECT source_path FROM themes ORDER BY theme_id,version' > "$backup/theme-sources"
while IFS= read -r theme; do
  case "$theme" in /srv/fuyao-admin/themes/*) ;; *) exit 2 ;; esac
  runuser -u fuyao-admin -- "$candidate/fuyao-admin" register-theme \
    --contracts "$candidate/contracts/theme" "$theme"
done < "$backup/theme-sources"
