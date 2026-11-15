English | [简体中文](./CI_UPDATES_ZH.md)

# CI builds and application updates

GitHub Actions validates the backend against PostgreSQL, validates and builds
the frontend once, then cross-compiles the Linux amd64 application and supervisor.
Successful main and dev builds publish an immutable prerelease named
`build-<run-id>-<attempt>`. These build identifiers are independent of the product
version. Pull requests never publish deployable releases. Optional real-theme
integration remains a separate manual input.

The architecture and maintenance flow are adapted from robot-platform's
`deploy/server/manager`, `backend/internal/httpapi/platform_update.go`,
`frontend/lib/platformMaintenance.ts` and `PlatformComponentsPage`.
GitHub replaces GitLab; immutable systemd release directories replace Docker
slots. FuyaoManager keeps one production application and main/dev update channels.
Its PostgreSQL database, themes, site data, published websites and Caddy remain
in their existing persistent locations.

## Components

| Component                           | Responsibility                                                                                        |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `.github/workflows/ci.yml`          | Tests, one frontend build, Linux package and immutable CI prerelease                                  |
| `scripts/package-release.py`        | Cross-compilation, build identity, per-file SHA-256 manifest and archive                              |
| `backend/cmd/fuyao-manager-updater` | Independent root-owned systemd supervisor                                                             |
| `backend/internal/platformupdate`   | GitHub provenance, extraction, admission, task journal, activation and recovery                       |
| `/api/v1/admin/updates`             | Administrator and CSRF protected proxy over a fixed Unix socket                                       |
| `/updates`                          | Version checks, selected deployment, channel, automatic updates, pinning, rollback and task/log views |

## Update behavior

The supervisor selects successful runs of this repository's `ci.yml`, excluding
forks and pull requests. Before installation it rechecks the exact run attempt,
downloads the matching release asset, verifies GitHub's SHA-256 digest and the
manifest's repository, commit, run, attempt, branch and architecture. Extraction
rejects links, duplicate entries, path traversal and oversized archives. Every
listed file must match its SHA-256; executables must be Linux x86-64 ELF files.

The package is staged before stopping the application. The fixed root-installed
preparation command backs up PostgreSQL and configuration, then invokes the
candidate CLI as `fuyao-admin`. The supervisor records its switch journal,
stops `fuyao-admin.service`, atomically switches `current`, starts it and checks
`/api/v1/version` for the exact commit. Failure restores and verifies the previous
program. An interrupted switch is recovered on supervisor restart without
resubmitting the deployment. The supervisor binary is replaced from the same
verified package after application verification.

The UI distinguishes accepted tasks, successful verification, failed preparation,
verified rollback and recovery requiring operator attention. An interrupted HTTP
request is resolved by reading the original task, not automatically submitting
another write. Source and channel changes invalidate checked candidates;
pinning blocks manual and automatic deployment. Automatic updates are off on
initial installation; enabling them checks every five minutes.

Database schema changes require a separate operator migration. Automated updates
and rollback only accept packages with the same schema version as the current
program. Application rollback does not restore a database backup or website
content. A legacy program without build metadata remains available for manual
first-install recovery; it is not presented as a verified rollback candidate.

## Installation and configuration

Initial installation is an operator action. Use a successful CI prerelease,
with Python 3 and PostgreSQL client tools compatible with the server installed.
verify its asset digest, and preserve the old application and a database backup
before switching. The supervisor's `stage` operator command uses the same package
verifier as routine updates:

```text
fuyao-manager-updater stage ARCHIVE NEW_RELEASE_DIR OWNER/REPO COMMIT RUN_ID ATTEMPT BRANCH --verified-download
```

Install `fuyao-manager-updater.service` and the supervisor binary under
`/srv/fuyao-admin/updater/`. Install `prepare-update.sh` as
`/usr/local/libexec/fuyao-manager-prepare`. All those files, their parent
directories, `/srv/fuyao-admin/etc/{db.env,config.env,updater.json}` and release
directories must be owned by root and must not be writable by the application
account. Use `updater.example.json` as the initial root-owned configuration.
Set `FUYAO_ADMIN_UPDATER_SOCKET=/run/fuyao-manager-updater/control.sock` in the
application's existing environment file. After `systemctl daemon-reload`, enable
`fuyao-manager-updater.service` and restart `fuyao-admin.service`.

Only the `fuyao-admin` Unix group can reach the supervisor socket. The supervisor
has no TCP listener, general command endpoint or browser-controlled repository.
The configured GitHub repository and deployment command can only be changed by
an operator. Public release assets work anonymously. An optional read token can
be set or cleared on the maintenance page to increase GitHub API quota; it is
stored separately with mode 0600 and is never returned to the browser or audit
log. Root-only task state and token files live under `/srv/fuyao-admin/update/`.

Check both services and `/api/v1/version` after installation. Verify that the
application cannot write the installation or read the token. Normal updates then
use **Platform settings → Platform updates**. Keep backups and the previous
release until operational verification is complete. Release cleanup is an
operator action and must preserve external theme dependency references.

The three npm version manifests and the default Go version are synchronized
from the root package version with `npm run version:set -- Y.M.P`. Apply the
repository's authorization rules before changing the annual or major segment.
