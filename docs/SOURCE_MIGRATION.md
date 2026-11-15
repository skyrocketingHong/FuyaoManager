# Source Migration: from FuyaoHomepage to FuyaoManager

FuyaoManager was extracted from the FuyaoHomepage repository
(<https://github.com/skyrocketingHong/FuyaoHomepage>) on 2026-11-12.

## Extraction

- Source branch at extraction: `codex/cms-mvp`, commit
  `215ae47782b40c6922531b0dbe9c15cf3ca03e0f` (preserved in the source
  repository by the local tag `split/baseline-pre-manager-20261112`).
- Method: `git filter-repo` on a fresh temporary clone (never on the source
  working tree), keeping the platform paths and rewriting them to the new
  layout:
  - `admin/backend/` → `backend/`
  - `admin/frontend/` → `frontend/`
  - Historical `admin/ROADMAP*` records were imported and later removed from the current tree.
  - `contracts/` → `contracts/` (management OpenAPI + theme contracts)
  - `deploy/fuyao-admin/` → `deploy/fuyao-admin/`
  - `.github/workflows/ci.yml` (rewritten for the platform-only jobs)
  - `LICENSE` (AGPL-3.0-only, unchanged)
- The source repository's history, branches, tags, and remote were not
  modified; the extraction ran entirely inside the temporary clone.
- Path history within the kept directories was linear (no renames into the
  kept paths), so per-commit path filtering preserves the full development
  history of the platform code.

## Commit mapping

`git filter-repo` rewrote commit hashes. The complete old-hash → new-hash
map is committed at
[docs/source-mapping/commit-map-from-fuyaohomepage.txt](./source-mapping/commit-map-from-fuyaohomepage.txt);
67 source commits map to 48 extracted commits (commits touching only dropped
paths are pruned entirely).

## Identity changes in this repository

- Go module: `github.com/skyrocketingHong/FuyaoHomepage/admin/backend` →
  `github.com/skyrocketingHong/FuyaoManager/backend` (all internal imports
  rewritten).
- npm package: `fuyao-admin-frontend` → `fuyao-manager-frontend`.
- Product naming in the console, OpenAPI title, and docs: FuyaoManager.

## Deliberately unchanged (compatibility)

- CLI binary and service name: `fuyao-admin` (systemd unit
  `fuyao-admin.service`, deployment paths under `/srv/fuyao-admin`).
- All `FUYAO_ADMIN_*` environment variables.
- Theme identity: first theme id `fuyao-homepage`, adapter
  `node22-sveltekit-static`, contract protocol identifiers
  (`fuyao.theme.manifest/v1`, `fuyao.theme.build-input/v1`).
- Database schema and migrations.
- License AGPL-3.0-only and authorship.
