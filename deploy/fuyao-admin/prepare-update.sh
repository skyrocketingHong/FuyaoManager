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
python3 - "$backup/database.dump" "$backup/theme-sources" <<'PY'
import os
import subprocess
import sys
from urllib.parse import parse_qsl, unquote, urlsplit


def connection_env(uri, inherited):
    parsed = urlsplit(uri)
    if parsed.scheme not in ('postgres', 'postgresql') or not parsed.hostname or not parsed.username or not parsed.path.strip('/'):
        raise ValueError('a complete PostgreSQL URI is required')
    result = {key: value for key, value in inherited.items() if not key.startswith('PG')}
    result.update(PGHOST=unquote(parsed.hostname), PGPORT=str(parsed.port or 5432),
                  PGUSER=unquote(parsed.username), PGPASSWORD=unquote(parsed.password or ''),
                  PGDATABASE=unquote(parsed.path[1:]))
    options = {'sslmode': 'PGSSLMODE', 'sslrootcert': 'PGSSLROOTCERT', 'sslcert': 'PGSSLCERT',
               'sslkey': 'PGSSLKEY', 'connect_timeout': 'PGCONNECT_TIMEOUT', 'application_name': 'PGAPPNAME'}
    for key, value in parse_qsl(parsed.query):
        if key not in options:
            raise ValueError('unsupported PostgreSQL connection option')
        result[options[key]] = value
    return result


pg_env = connection_env(os.environ['FUYAO_ADMIN_DATABASE_URL'], os.environ)
subprocess.run(['pg_dump', '--format=custom', '--file', sys.argv[1]], env=pg_env, check=True)
with open(sys.argv[2], 'w') as output:
    subprocess.run(['psql', '-XAt', '-v', 'ON_ERROR_STOP=1', '-c',
                    'SELECT source_path FROM themes ORDER BY theme_id,version'],
                   env=pg_env, stdout=output, check=True)
PY
cp -a /srv/fuyao-admin/etc "$backup/etc"
readlink -f /srv/fuyao-admin/current > "$backup/previous"
# Run the candidate CLI with service privileges. No deployment package code
# is executed with root privileges during preparation.
runuser -u fuyao-admin -- "$candidate/fuyao-admin" version
runuser -u fuyao-admin -- "$candidate/fuyao-manager-updater" version
runuser -u fuyao-admin -- "$candidate/fuyao-admin" migrate
while IFS= read -r theme; do
  case "$theme" in /srv/fuyao-admin/themes/*|/srv/fuyao-admin/data/themes/github/*) ;; *) exit 2 ;; esac
  runuser -u fuyao-admin -- "$candidate/fuyao-admin" register-theme \
    --contracts "$candidate/contracts/theme" "$theme"
done < "$backup/theme-sources"
