#!/usr/bin/env python3
"""Operate the five-service research distribution; never print credentials."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def build_images(services=None):
    if services is not None and not set(services).issubset({"app", "docreader", "frontend"}):
        raise ValueError("Unknown research build service")
    # BuildKit's directory-session metadata rejects non-ASCII paths on some
    # Docker/Colima releases. Sending an archive also works in Chinese folders.
    # Never send local accounts, keys, backups or dependency caches to Docker.
    excluded = {".git", "node_modules", ".venv", ".venv_proto", "__pycache__",
                ".cursor", ".claude", ".codebuddy", ".vscode", ".idea"}

    def include(member):
        parts = Path(member.name).parts
        if any(part in excluded or part.startswith(".research") or
               part == ".env" or part.startswith(".env.") for part in parts):
            return None
        if parts and parts[0] in {"artifacts", "logs", "tmp", "temp", "dist", "dist-ssr", "coverage"}:
            return None
        if len(parts) >= 2 and parts[0] == "frontend" and parts[1] in {"dist", "dist-ssr", "coverage"}:
            return None
        if member.name.endswith((".pem", ".key", ".crt", ".pyc", ".log", ".tsbuildinfo")):
            return None
        return member

    subprocess.run(["docker", "buildx", "version"], check=True, stdout=subprocess.DEVNULL)
    with tempfile.TemporaryDirectory(prefix="research-build-") as temporary:
        archives = {}
        for service, context, dockerfile in (
            ("app", ROOT, "docker/Dockerfile.research-app"),
            ("docreader", ROOT, "docker/Dockerfile.research-docreader"),
            ("frontend", ROOT / "frontend", "Dockerfile"),
        ):
            if services is not None and service not in services:
                continue
            archive = archives.get(context)
            if archive is None:
                archive = Path(temporary) / (service + ".tar.gz")
                with tarfile.open(archive, "w:gz", compresslevel=1) as output:
                    output.add(context, arcname=".", filter=include)
                archives[context] = archive
            command = ["docker", "buildx", "build", "--load", "--progress=plain"]
            if dockerfile != "Dockerfile":
                command += ["-f", dockerfile]
            command += ["-t", "context-workspace/" + service + ":0.1.0"]
            if service == "frontend":
                for name in ("NPM_REGISTRY", "NODE_MAX_OLD_SPACE_SIZE", "VITE_FRONTEND_COMMIT"):
                    if os.environ.get(name):
                        command += ["--build-arg", name + "=" + os.environ[name]]
            with archive.open("rb") as source:
                subprocess.run(command + ["-"], cwd=ROOT, stdin=source, check=True)


def compose(args, env=None, **kwargs):
    command = ["docker", "compose", "-f", str(ROOT / "compose.research.yaml")]
    effective = os.environ.copy()
    if env:
        effective.update(env)
    if effective.get("RESEARCH_ENV_FILE"):
        command += ["--env-file", effective["RESEARCH_ENV_FILE"]]
    if effective.get("RESEARCH_BASELINE") == "1":
        command += ["-f", str(ROOT / "compose.research.baseline.yaml")]
    return subprocess.run(command + args, cwd=ROOT, env=effective, check=True, **kwargs)


def binary_output(args, path, env=None):
    temporary = path.with_suffix(path.suffix + ".partial")
    try:
        with temporary.open("wb") as output:
            compose(args, env, stdout=output)
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)


def verify(folder):
    manifest = json.loads((folder / "manifest.json").read_text())
    if manifest.get("schema_version") != 1:
        raise ValueError("unsupported backup format")
    for name in ("database.dump", "files.tar.gz", "redis.tar.gz", ".env"):
        path = folder / name
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != manifest["sha256"].get(name):
            raise ValueError("backup checksum mismatch: " + name)
    for name in ("files.tar.gz", "redis.tar.gz"):
        with tarfile.open(folder / name) as archive:
            for member in archive:
                parts = Path(member.name).parts
                if Path(member.name).is_absolute() or ".." in parts or member.issym() or member.islnk():
                    raise ValueError("unsafe archive member")
    return manifest


def wait_healthy(service, env=None):
    for _ in range(60):
        result = compose(["ps", "-q", service], env, capture_output=True, text=True)
        identifier = result.stdout.strip()
        if identifier:
            state = subprocess.run(["docker", "inspect", identifier, "--format",
                "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}"],
                check=True, capture_output=True, text=True).stdout.strip()
            if state in ("healthy", "running"):
                return
            if state in ("exited", "dead", "unhealthy"):
                raise RuntimeError(service + " did not become healthy; inspect its logs")
        time.sleep(2)
    raise TimeoutError(service + " startup timed out")


def backup(folder):
    folder.mkdir(parents=True, exist_ok=False, mode=0o700)
    # Quiesce only this application's writers. The unrelated host containers
    # and this instance's database stay running. A stopped Redis flushes AOF.
    compose(["stop", "app", "redis"])
    try:
        binary_output(["exec", "-T", "postgres", "sh", "-c",
            'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc'], folder / "database.dump")
        binary_output(["run", "--rm", "--no-deps", "-T", "--entrypoint", "tar", "app",
            "-czf", "-", "-C", "/data/files", "."], folder / "files.tar.gz")
        binary_output(["run", "--rm", "--no-deps", "-T", "--entrypoint", "tar", "redis",
            "-czf", "-", "-C", "/data", "."], folder / "redis.tar.gz")
        config = Path(os.environ.get("RESEARCH_ENV_FILE", ROOT / ".env"))
        shutil.copyfile(config, folder / ".env")
        (folder / ".env").chmod(0o600)
        services = json.loads(compose(["config", "--format", "json"], capture_output=True, text=True).stdout)["services"]
        image_ids = {}
        for name in services:
            identifier = compose(["ps", "-aq", name], capture_output=True, text=True).stdout.strip()
            image_ids[name] = subprocess.run(["docker", "inspect", identifier, "--format", "{{.Image}}"],
                check=True, capture_output=True, text=True).stdout.strip()
        manifest = {
            "schema_version": 1,
            "created_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "upstream_commit": "3e22822c1d2397be730c458c7539d0eb8c113a24",
            "images": {name: config["image"] for name, config in services.items()},
            "image_ids": image_ids,
            "sha256": {name: hashlib.sha256((folder / name).read_bytes()).hexdigest()
                for name in ("database.dump", "files.tar.gz", "redis.tar.gz", ".env")},
        }
        (folder / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2))
        verify(folder)
    finally:
        compose(["up", "-d", "redis", "app"])
    print("一致性备份已校验：", folder)


def restore(folder, project, port):
    manifest = verify(folder)
    if not re.fullmatch(r"[a-z][a-z0-9-]{2,40}", project) or project == "context-workspace":
        raise ValueError("restore requires a new project name, e.g. context-restore-check")
    existing = subprocess.run(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=" + project],
        check=True, capture_output=True, text=True).stdout.strip()
    if existing:
        raise ValueError("destination project already exists; restoration never overwrites an existing instance")
    volumes = subprocess.run(["docker", "volume", "ls", "-q", "--filter", "label=com.docker.compose.project=" + project],
        check=True, capture_output=True, text=True).stdout.strip()
    if volumes:
        raise ValueError("destination project already has data volumes; choose a new project name")
    env = {
        "RESEARCH_PROJECT": project,
        "RESEARCH_ENV_FILE": str(folder / ".env"),
        "FRONTEND_PORT": str(port),
        "FRONTEND_BASE_URL": f"http://localhost:{port}",
        "RESEARCH_BIND_ADDRESS": "127.0.0.1",
    }
    # Restore the exact binaries recorded by the backup, even if a local tag
    # has subsequently been rebuilt. Load those images first on another host.
    for service in ("app", "frontend", "docreader"):
        image = manifest.get("image_ids", {}).get(service)
        if image:
            subprocess.run(["docker", "image", "inspect", image], check=True, stdout=subprocess.DEVNULL)
            env["RESEARCH_" + service.upper() + "_IMAGE"] = image
    compose(["up", "-d", "postgres", "redis", "docreader"], env)
    wait_healthy("postgres", env)
    with (folder / "database.dump").open("rb") as source:
        # ParadeDB initializes extensions/schemas even in a brand-new volume.
        # This destination was verified absent before creation, so cleaning its
        # bootstrap objects is safe and avoids duplicate-schema restore errors.
        compose(["exec", "-T", "postgres", "sh", "-c",
            'exec pg_restore --clean --if-exists --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"'], env, stdin=source)
    mounted = str(folder) + ":/restore:ro"
    compose(["run", "--rm", "--no-deps", "-T", "-v", mounted, "--entrypoint", "tar", "app",
        "-xzf", "/restore/files.tar.gz", "-C", "/data/files"], env)
    compose(["stop", "redis"], env)
    compose(["run", "--rm", "--no-deps", "-T", "-v", mounted, "--entrypoint", "tar", "redis",
        "-xzf", "/restore/redis.tar.gz", "-C", "/data"], env)
    compose(["up", "-d"], env)
    wait_healthy("app", env)
    print(f"恢复到了独立实例：http://localhost:{port}/platform/research")
    print("项目名：", project)


def main():
    parser = argparse.ArgumentParser(description="团队研究平台运行与一致性备份")
    sub = parser.add_subparsers(dest="action", required=True)
    start = sub.add_parser("up")
    start.add_argument("--build", action="store_true")
    start.add_argument("--baseline", action="store_true")
    sub.add_parser("status")
    sub.add_parser("stop")
    save = sub.add_parser("backup")
    save.add_argument("folder", type=Path)
    check = sub.add_parser("verify-backup")
    check.add_argument("folder", type=Path)
    load = sub.add_parser("restore")
    load.add_argument("folder", type=Path)
    load.add_argument("--project", required=True)
    load.add_argument("--port", type=int, default=8092)
    args = parser.parse_args()
    if args.action == "up":
        subprocess.run([sys.executable, str(ROOT / "scripts/research-setup.py")], check=True)
        env = {"RESEARCH_BASELINE": "1"} if args.baseline else None
        if args.build and not args.baseline:
            build_images()
        command = ["up", "-d", "--no-build"]
        compose(command, env)
        wait_healthy("app", env)
        print("打开 http://localhost:8090/platform/research；首次使用先创建账号，再配置模型。")
    elif args.action == "status":
        compose(["ps"])
    elif args.action == "stop":
        compose(["stop"])
    elif args.action == "backup":
        backup(args.folder.resolve())
    elif args.action == "verify-backup":
        verify(args.folder.resolve())
        print("备份校验通过。")
    elif args.action == "restore":
        restore(args.folder.resolve(), args.project, args.port)


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, ValueError, RuntimeError, OSError) as error:
        print("操作未完成：", error, file=sys.stderr)
        raise SystemExit(1)
