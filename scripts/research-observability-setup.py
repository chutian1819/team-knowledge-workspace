#!/usr/bin/env python3
"""Initialize this workspace's optional local observability services without printing secrets."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
from urllib.parse import quote
import uuid
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
ENV = ROOT / ".research-observability.env"


def update_private_environment(updates):
    """Atomically change only named fields, preserving unrelated credentials."""
    lines = [line for line in ENV.read_text().splitlines()
             if line.split("=", 1)[0].strip() not in updates]
    lines.extend(key + "=" + value for key, value in updates.items())
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", prefix=".research-observability-", suffix=".tmp",
                                         dir=ROOT, delete=False) as target:
            temporary = Path(target.name)
            os.fchmod(target.fileno(), 0o600)
            target.write("\n".join(lines) + "\n")
            target.flush()
            os.fsync(target.fileno())
        temporary.replace(ENV)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def database_state():
    raw_role = sql("""SELECT row_to_json(r) FROM (
        SELECT rolname, rolsuper, rolcreatedb, rolcreaterole, rolreplication,
               rolbypassrls, rolcanlogin,
               EXISTS (SELECT 1 FROM pg_auth_members WHERE member=pg_roles.oid) AS member_of_other_roles
        FROM pg_roles WHERE rolname='research_langfuse') r;""")
    role = json.loads(raw_role) if raw_role else None
    owner = sql("SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname='research_langfuse';")
    if role and (not role.get("rolcanlogin") or any(role.get(key) for key in
                ("rolsuper", "rolcreatedb", "rolcreaterole", "rolreplication", "rolbypassrls", "member_of_other_roles"))):
        raise RuntimeError("Existing observability role has unexpected privileges; left unchanged")
    if owner and owner != "research_langfuse":
        raise RuntimeError("Existing observability database belongs to another role; left unchanged")
    return role, owner


def dotenv(path):
    values = {}
    for line in path.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip().strip('"').strip("'")
    return values


def sql(query):
    result = subprocess.run(
        ["docker", "exec", "-i", "context-workspace-postgres", "sh", "-c",
         'exec psql -X -q -t -A -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres'],
        input=query, text=True, capture_output=True,
    )
    if result.returncode:
        # SQL error output can contain the statement and its password.
        raise RuntimeError("Local observability database initialization failed; no secrets were printed")
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--enable-platform", action="store_true",
                        help="After local services are healthy, enable their existing platform adapters")
    parser.add_argument("--migrate-redis", action="store_true",
                        help="Explicitly move this local configuration to a dedicated Redis; old DB 1 is preserved")
    args = parser.parse_args()
    role, existing_owner = database_state()
    created = not ENV.exists()
    if created and (role or existing_owner):
        raise RuntimeError("Observability database/role already exists but private configuration is missing; restore the original private environment instead of generating replacement secrets")
    if created:
        password = secrets.token_hex(24)
        redis_password = secrets.token_hex(24)
        values = {
            "LF_DATABASE_PASSWORD": password,
            "LF_DATABASE_URL": "postgresql://research_langfuse:" + password + "@context-workspace-postgres:5432/research_langfuse",
            "LF_REDIS_PASSWORD": redis_password,
            "LF_REDIS_URL": "redis://:" + quote(redis_password, safe="") + "@research-langfuse-redis:6379/0",
            "LF_SALT": secrets.token_hex(32),
            "LF_ENCRYPTION_KEY": secrets.token_hex(32),
            "LF_NEXTAUTH_SECRET": secrets.token_hex(32),
            "LF_CLICKHOUSE_PASSWORD": secrets.token_hex(24),
            "LF_MINIO_PASSWORD": secrets.token_hex(24),
            "LANGFUSE_PUBLIC_KEY": "pk-lf-" + str(uuid.uuid4()),
            "LANGFUSE_SECRET_KEY": "sk-lf-" + str(uuid.uuid4()),
            "LF_ADMIN_EMAIL": "admin@research.local",
            "LF_ADMIN_PASSWORD": secrets.token_urlsafe(24),
        }
        fd = os.open(ENV, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as target:
            target.write("# Private local credentials. Do not commit or share.\n")
            target.write("\n".join(key + "=" + value for key, value in values.items()) + "\n")
    else:
        values = dotenv(ENV)
        ENV.chmod(0o600)
    password = values.get("LF_DATABASE_PASSWORD", "")
    if not password or any(c not in "0123456789abcdef" for c in password):
        raise RuntimeError("Expected the generated hexadecimal database password")
    redis_password = values.get("LF_REDIS_PASSWORD", "")
    redis_url = "redis://:" + quote(redis_password, safe="") + "@research-langfuse-redis:6379/0"
    redis_migrated = False
    if not created and (not redis_password or values.get("LF_REDIS_URL") != redis_url):
        if not args.migrate_redis:
            raise RuntimeError("Existing configuration needs explicit --migrate-redis; no credentials or old Redis data were changed")
        # Reuse an already generated Redis password on every retry. Nothing
        # else is regenerated, including database, encryption and project keys.
        redis_password = redis_password or secrets.token_hex(24)
        updates = {"LF_REDIS_PASSWORD": redis_password,
                   "LF_REDIS_URL": "redis://:" + quote(redis_password, safe="") + "@research-langfuse-redis:6379/0"}
        update_private_environment(updates)
        values.update(updates)
        redis_migrated = True
    if role is None:
        sql("CREATE ROLE research_langfuse LOGIN PASSWORD '" + password + "';")
    if not existing_owner:
        sql("CREATE DATABASE research_langfuse OWNER research_langfuse TEMPLATE template0;")
    if args.enable_platform:
        auth = base64.b64encode((values["LANGFUSE_PUBLIC_KEY"] + ":" + values["LANGFUSE_SECRET_KEY"]).encode()).decode()
        request = urllib.request.Request("http://127.0.0.1:3000/api/public/projects",
                                         headers={"Authorization": "Basic " + auth})
        with urllib.request.urlopen(request, timeout=15) as response:
            projects = json.load(response).get("data", [])
        if not any(p.get("id") == "embodied-research" for p in projects):
            raise RuntimeError("The expected local Langfuse project is not ready")
        additions = {"LANGFUSE_ENABLED": "true", "LANGFUSE_HOST": "http://research-langfuse-web:3000",
                     "LANGFUSE_PUBLIC_KEY": values["LANGFUSE_PUBLIC_KEY"],
                     "LANGFUSE_SECRET_KEY": values["LANGFUSE_SECRET_KEY"],
                     "LANGFUSE_ENVIRONMENT": "local-research", "LANGFUSE_RELEASE": "semantic-writing-20261022",
                     "OLLAMA_BASE_URL": "http://research-embedding:11434"}
        target = ROOT / ".env"
        original = target.read_text()
        backup = ROOT / ".research-observability-platform-before.env"
        if not backup.exists():
            fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, "w") as handle:
                handle.write(original)
        lines = [line for line in original.splitlines() if line.split("=", 1)[0].strip() not in additions]
        lines.extend(key + "=" + value for key, value in additions.items())
        temporary = ROOT / ".research-observability-platform-next.env"
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as handle:
            handle.write("\n".join(lines) + "\n")
        temporary.replace(target)
    print(json.dumps({"private_configuration_created": created, "database_ready": True,
                      "credentials_printed": False, "database": "research_langfuse",
                      "redis_configuration_migrated": redis_migrated,
                      "redis_service": "research-langfuse-redis", "old_redis_db1_preserved": True,
                      "platform_configuration_enabled": args.enable_platform,
                      "platform_restart_required": args.enable_platform}))


if __name__ == "__main__":
    main()
