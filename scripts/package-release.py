#!/usr/bin/env python3
"""Build the immutable Linux release consumed by the independent updater."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def command(*args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    parser.add_argument("--development", action="store_true")
    args = parser.parse_args()
    dirty = command("git", "status", "--porcelain")
    if dirty and not args.development:
        raise SystemExit("CI release inputs must be committed and clean")
    commit = command("git", "rev-parse", "HEAD")
    branch = os.environ.get("GITHUB_REF_NAME", command("git", "branch", "--show-current"))
    version = json.loads((ROOT / "package.json").read_text())["version"]
    built_at = datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds")
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="fuyao-package-") as temporary:
        stage = Path(temporary) / "release"
        stage.mkdir()
        shutil.copytree(ROOT / "frontend/dist", stage / "frontend/dist")
        shutil.copytree(ROOT / "contracts", stage / "contracts")
        (stage / "backend").mkdir()
        shutil.copy2(ROOT / "backend/go.mod", stage / "backend/go.mod")
        shutil.copy2(ROOT / "package.json", stage / "package.json")
        shutil.copy2(ROOT / "LICENSE", stage / "LICENSE")
        shutil.copytree(ROOT / "deploy/fuyao-admin", stage / "deploy/fuyao-admin")
        environment = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
        package = "github.com/skyrocketingHong/FuyaoManager/backend/internal/buildinfo"
        flags = f"-s -w -X {package}.Version={version} -X {package}.Commit={commit} -X {package}.BuiltAt={built_at}"
        for name in ("fuyao-admin", "fuyao-manager-updater", "fuyao-caddy-control", "fuyao-caddy-inventory", "fuyao-caddy-register"):
            subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", flags,
                            "-o", str(stage / name), f"./cmd/{name}"], cwd=ROOT / "backend", env=environment, check=True)
        files = {p.relative_to(stage).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
                 for p in sorted(stage.rglob("*")) if p.is_file()}
        migrations = ROOT / "backend/internal/db/migrations"
        manifest = {"format": 1, "version": version, "commit": commit, "branch": branch,
                    "runId": int(os.environ.get("GITHUB_RUN_ID", "0")),
                    "attempt": int(os.environ.get("GITHUB_RUN_ATTEMPT", "1")),
                    "repository": os.environ.get("GITHUB_REPOSITORY", "skyrocketingHong/FuyaoManager"),
                    "architecture": "linux/amd64", "databaseSchema": max(int(p.name.split("_")[0]) for p in migrations.glob("*.sql")),
                    "createdAt": built_at, "url": os.environ.get("GITHUB_SERVER_URL", "https://github.com") + "/" + os.environ.get("GITHUB_REPOSITORY", "skyrocketingHong/FuyaoManager") + "/actions/runs/" + os.environ.get("GITHUB_RUN_ID", "0"), "files": files}
        (stage / "release.json").write_text(json.dumps(manifest, indent=2) + "\n")
        archive = output / "fuyao-manager-linux-amd64.tar.gz"
        with tarfile.open(archive, "w:gz", format=tarfile.USTAR_FORMAT) as packed:
            for p in sorted(stage.rglob("*")):
                if p.is_file():
                    packed.add(p, arcname=p.relative_to(stage).as_posix(), recursive=False)
        shutil.copy2(stage / "release.json", output / "release.json")
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (output / "SHA256SUMS").write_text(f"{digest}  {archive.name}\n")
        print(json.dumps({"archive": str(archive), "sha256": digest, "commit": commit, "version": version}))


if __name__ == "__main__":
    main()
