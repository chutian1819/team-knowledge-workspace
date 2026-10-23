#!/usr/bin/env python3
"""Back up the local Langfuse data set separately from the business platform.

Stops only Langfuse writers/storage during its snapshot. No volumes are deleted
or restored. The resulting private archive is checksum-verified, not a claim
that disaster recovery on another host has been rehearsed.
"""
import argparse
import datetime
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import time

ROOT = Path(__file__).resolve().parents[1]
SERVICES = {"langfuse-web": "research-langfuse-web", "langfuse-worker": "research-langfuse-worker",
            "clickhouse": "research-langfuse-clickhouse", "minio": "research-langfuse-minio",
            "langfuse-redis": "research-langfuse-redis"}


def compose(args):
    subprocess.run(["docker", "compose", "--env-file", str(ROOT / ".research-observability.env"),
                    "-f", str(ROOT / "compose.research.observability.yaml")] + args, check=True)


def capture(command, path):
    temporary = path.with_suffix(path.suffix + ".partial")
    with temporary.open("wb") as output:
        subprocess.run(command, stdout=output, check=True)
    temporary.replace(path)


def sha256(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("folder", type=Path)
    args = parser.parse_args()
    folder = args.folder.resolve()
    if folder.exists():
        raise ValueError("Backup destination already exists; left unchanged")
    inspected = {}
    for service, name in SERVICES.items():
        inspected[service] = json.loads(subprocess.check_output(["docker", "inspect", name]))[0]
    running = [key for key, data in inspected.items() if data["State"]["Running"]]
    volumes = {}
    for service, destination in (("clickhouse", "/var/lib/clickhouse"), ("minio", "/data"), ("langfuse-redis", "/data")):
        matches = [m["Name"] for m in inspected[service]["Mounts"] if m["Destination"] == destination and m["Type"] == "volume"]
        if len(matches) != 1:
            raise ValueError("Expected a dedicated named volume for " + service)
        volumes[service] = matches[0]
    folder.mkdir(parents=True, mode=0o700)
    old_mask = os.umask(0o077)
    try:
        compose(["stop", "langfuse-web", "langfuse-worker"])
        compose(["stop", "clickhouse", "minio", "langfuse-redis"])
        capture(["docker", "exec", "context-workspace-postgres", "sh", "-c",
                 'exec pg_dump -U "$POSTGRES_USER" -d research_langfuse -Fc'], folder / "langfuse-postgres.dump")
        with (folder / "langfuse-postgres.dump").open("rb") as source:
            subprocess.run(["docker", "exec", "-i", "context-workspace-postgres", "pg_restore", "--list"],
                           stdin=source, stdout=subprocess.DEVNULL, check=True)
        for service, volume in volumes.items():
            target = folder / (service + ".tar.gz")
            capture(["docker", "run", "--rm", "--network", "none", "--read-only",
                     "--mount", "type=volume,src=" + volume + ",dst=/backup,readonly",
                     "--entrypoint", "tar", inspected["langfuse-redis"]["Image"], "-czf", "-", "-C", "/backup", "."], target)
            # Native ClickHouse symlinks are retained; extraction is not performed.
            with tarfile.open(target, "r:gz") as archive:
                for item in archive:
                    if item.isfile():
                        source = archive.extractfile(item)
                        while source.read(1024 * 1024):
                            pass
            # tar readers can stop before gzip's trailer. Read the compressed
            # stream to EOF as well, so a corrupt CRC/size trailer is detected.
            with gzip.open(target, "rb") as source:
                while source.read(1024 * 1024):
                    pass
        for name in (".research-observability.env", "compose.research.observability.yaml"):
            shutil.copyfile(ROOT / name, folder / name)
        files = sorted(path.name for path in folder.iterdir() if path.is_file())
        manifest = {"schema_version": 1, "created_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                    "images": {key: data["Image"] for key, data in inspected.items()},
                    "volumes": volumes, "sha256": {name: sha256(folder / name) for name in files},
                    "business_platform_backup": "Use scripts/researchctl.py backup separately",
                    "restore_drill_performed": False}
        (folder / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        assert all(sha256(folder / name) == value for name, value in manifest["sha256"].items())
    finally:
        os.umask(old_mask)
        # Restart exact existing containers. Do not apply newer compose config
        # or implicitly start dependencies that were stopped before the backup.
        storage = [key for key in ("clickhouse", "minio", "langfuse-redis") if key in running]
        writers = [key for key in ("langfuse-worker", "langfuse-web") if key in running]
        try:
            if storage:
                subprocess.run(["docker", "start"] + [inspected[key]["Id"] for key in storage],
                               check=True, stdout=subprocess.DEVNULL)
                deadline = time.monotonic() + 60
                while time.monotonic() < deadline:
                    states = [json.loads(subprocess.check_output(["docker", "inspect", inspected[key]["Id"]]))[0]["State"] for key in storage]
                    if all(state.get("Running") and state.get("Health", {}).get("Status", "healthy") == "healthy" for state in states):
                        break
                    time.sleep(2)
                else:
                    raise RuntimeError("Storage containers restarted but did not regain health")
        finally:
            if writers:
                subprocess.run(["docker", "start"] + [inspected[key]["Id"] for key in writers],
                               check=True, stdout=subprocess.DEVNULL)
    print(json.dumps({"backup": str(folder), "checksums_verified": True, "restore_drill_performed": False}))


if __name__ == "__main__":
    main()
