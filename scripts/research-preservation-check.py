#!/usr/bin/env python3
"""Read-only fingerprints for an in-place research stack update. Never print data or keys."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
TABLES = {"users", "tenants", "tenant_members", "knowledge_bases", "knowledges", "chunks",
          "wiki_pages", "wiki_page_revisions", "models", "organizations", "organization_members",
          "kb_shares", "sessions", "messages", "research_inboxes", "research_source_versions",
          "research_capture_jobs", "research_proposals", "research_exports",
          "research_writing_rules", "research_writing_feedbacks", "research_writing_events",
          "research_writing_learning_settings", "research_writing_extractions"}


def sql(query):
    command = ["docker", "exec", "-i", "context-workspace-postgres", "sh", "-c",
               'exec psql -X -q -t -A -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"']
    result = subprocess.run(command, input=query, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError("read-only database fingerprint query failed")
    return [json.loads(line) for line in result.stdout.splitlines() if line.strip()]


def fingerprint(baseline=None):
    schema = sql("SELECT json_build_object('table',table_name,'columns',json_agg(column_name ORDER BY ordinal_position)) "
                 "FROM information_schema.columns WHERE table_schema='public' GROUP BY table_name ORDER BY table_name;")
    columns = {item["table"]: item["columns"] for item in schema if item["table"] in TABLES}
    selected = baseline["columns"] if baseline else columns
    rows = []
    for table, fields in selected.items():
        if table not in columns or not set(fields).issubset(columns[table]):
            raise RuntimeError("a pre-update table or column is missing: " + table)
        # Compare original columns only; additive migrations must not look like
        # changes to old records merely because a new default column appeared.
        dropped = sorted(set(columns[table]) - set(fields))
        expr = "to_jsonb(t)"
        if dropped:
            expr += " - ARRAY[" + ",".join("'" + field.replace("'", "''") + "'" for field in dropped) + "]::text[]"
        quoted = '"' + table.replace('"', '""') + '"'
        rows.extend(sql("SELECT json_build_object('table','" + table + "','count',count(*),'digest',"
                        "md5(coalesce(string_agg(md5((" + expr + ")::text),'' ORDER BY (" + expr + ")::text),''))) "
                        "FROM public." + quoted + " t;"))
    files = subprocess.run(["docker", "exec", "context-workspace-app", "sh", "-c",
                            "find /data/files -type f -exec sha256sum {} \\;"], capture_output=True, text=True)
    if files.returncode:
        raise RuntimeError("read-only file fingerprint failed")
    lines = sorted(line for line in files.stdout.splitlines() if line)
    images, mounts = {}, {}
    for name in ("app", "frontend", "docreader", "postgres", "redis"):
        inspected = subprocess.run(["docker", "inspect", "context-workspace-" + name,
                                    "--format", "{{json .Mounts}}\n{{.Image}}"], capture_output=True, text=True, check=True)
        mount_json, image = inspected.stdout.strip().splitlines()
        images[name] = image
        mounts[name] = sorted([{key: item.get(key) for key in ("Type", "Name", "Source", "Destination", "RW")}
                               for item in json.loads(mount_json)], key=lambda item: item["Destination"])
    return {"columns": selected, "tables": rows, "env_sha256": hashlib.sha256((ROOT / ".env").read_bytes()).hexdigest(),
            "files": {"count": len(lines), "sha256": hashlib.sha256("\n".join(lines).encode()).hexdigest()},
            "mounts": mounts, "image_ids": images}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--before", type=Path, help="Verify an earlier fingerprint; otherwise create one")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    baseline = json.loads(args.before.read_text()) if args.before else None
    current = fingerprint(baseline)
    if baseline:
        checks = {"data_rows_unchanged": current["tables"] == baseline["tables"],
                  "configuration_unchanged": current["env_sha256"] == baseline["env_sha256"],
                  "originals_and_exports_unchanged": current["files"] == baseline["files"],
                  "persistent_mounts_unchanged": current["mounts"] == baseline["mounts"]}
        current["checks"] = checks
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(current, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(current.get("checks") or {"fingerprinted_tables": len(current["tables"]),
                                              "fingerprinted_files": current["files"]["count"]}, ensure_ascii=False))
    if baseline and not all(current["checks"].values()):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
