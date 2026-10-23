#!/usr/bin/env python3
"""Read-only restore verification using the existing synthetic QA fixtures.

Run after researchctl.py restore. No account, document or task is created.
Private credentials are read locally and never written to the report.
"""
import argparse
import datetime
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def load_module(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--backup", type=Path, required=True)
    parser.add_argument("--base-url", default="http://localhost:8092")
    parser.add_argument("--project", default="context-restore-verify")
    args = parser.parse_args()
    if args.project == "context-workspace":
        parser.error("verify a separate restored project")
    ctl = load_module("researchctl", "researchctl.py")
    api_module = load_module("research_drafts_acceptance", "research-drafts-acceptance.py")
    manifest = ctl.verify(args.backup.resolve())
    state = json.loads((ROOT / ".research-state.json").read_text())
    original = api_module.API(state)
    restored = api_module.API({**state, "base_url": args.base_url})
    qa = state["qa"]
    report = {"created_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "suite": "isolated-backup-restore", "synthetic_fixtures": True,
              "read_only": True, "checks": []}

    def passed(name, **details):
        report["checks"].append({"name": name, "status": "passed", **details})
        print("PASS " + name)

    try:
        passed("database_files_redis_and_configuration_checksums")
        for service in ("app", "frontend", "docreader", "postgres", "redis"):
            actual = subprocess.run(["docker", "inspect", args.project + "-" + service,
                "--format", "{{.Image}}"], check=True, capture_output=True, text=True).stdout.strip()
            assert actual == manifest["image_ids"][service], "Restored image differs: " + service
        passed("all_five_images_match_backup_ids")

        fixtures = [item["id"] if isinstance(item, dict) else item for item in qa["fixture_ids"]]
        assert len(fixtures) == 50
        for identifier in fixtures:
            path = "/knowledge/" + identifier
            before, after = original.data("GET", path), restored.data("GET", path)
            for field in ("id", "title", "knowledge_base_id", "file_hash", "parse_status"):
                assert before.get(field) == after.get(field), "Document differs after restore: " + field
        passed("fifty_native_document_ids_and_metadata_restored", documents=len(fixtures))

        kb, identifier = qa["native_source_kb_id"], qa["original_file_id"]
        path = f"/research/capture/{kb}/sources/{identifier}"
        before, versions = original.data("GET", path), restored.data("GET", path)
        assert {v["id"] for v in before} == {v["id"] for v in versions}
        expected = {qa["original_expected_v1"], qa["original_expected_v2"]}
        actual = set()
        for version in versions:
            if version.get("has_original"):
                route = f"/knowledgebase/{kb}/research/sources/{identifier}/versions/{version['id']}/assets/original"
                content = restored.call("GET", route, binary=True)
                assert hashlib.sha256(content).hexdigest() == version["content_hash"]
                assert content == original.call("GET", route, binary=True)
                actual.add(content.decode("utf-8"))
        assert expected.issubset(actual), "Immutable original versions were not restored"
        passed("both_immutable_original_versions_and_bytes_restored")

        drafts = json.loads((ROOT / ".research-drafts-state.json").read_text())
        root = f"/knowledgebase/{drafts['research_kb_id']}/research"
        route = root + "/drafts/" + drafts["draft_id"]
        before, after = original.data("GET", route), restored.data("GET", route)
        for field in ("id", "content", "version", "title"):
            assert before[field] == after[field], "Research draft differs: " + field
        revisions = restored.data("GET", route + "/revisions")["revisions"]
        assert len(revisions) >= 2
        passed("native_research_page_content_version_and_revisions_restored", revisions=len(revisions))
        jobs = restored.data("GET", root + "/exports?page_id=" + drafts["draft_id"])
        formats = set()
        for job in jobs:
            if job["status"] == "ready" and job.get("format") in ("md", "docx", "pdf"):
                content = restored.call("GET", root + f"/exports/{job['id']}/download", binary=True)
                assert len(content) > 100
                formats.add(job["format"])
        assert formats == {"md", "docx", "pdf"}, "One or more restored exports are missing"
        passed("three_format_export_jobs_and_downloadable_files_restored")

        with zipfile.ZipFile(ROOT / "artifacts/research/project-assets.zip") as archive:
            assert archive.testzip() is None
            assets = json.loads(archive.read("manifest.json"))
            portable = {item["id"]: item for item in assets["documents"]}
            for identifier in fixtures:
                assert archive.read(portable[identifier]["markdown"]).decode("utf-8").strip()
        passed("offline_zip_reads_all_fifty_documents_without_platform")
        report["status"] = "passed"
    except Exception as exc:
        report["status"], report["failure"] = "failed", str(exc)
        raise
    finally:
        target = ROOT / "artifacts/research/backup-restore-report.json"
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
