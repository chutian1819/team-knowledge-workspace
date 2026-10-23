#!/usr/bin/env python3
"""Exercise reviewed drafts and exports against the local five-service stack.

Uses only stdlib and synthetic fixtures. Credentials are read from the private
root state and never copied into reports or printed. Run capture acceptance
first so .research-state.json contains qa.source_kb_id and qa.organization_id.
No model inference or real corpus retrieval quality is claimed by this script.
"""
from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET
import zipfile

ROOT = Path(__file__).resolve().parents[1]


class API:
    def __init__(self, state):
        base = state["base_url"].rstrip("/")
        self.base = base if base.endswith("/api/v1") else base + "/api/v1"
        self.actors = state["actors"]

    def call(self, method, path, body=None, actor="owner", expected=(200, 201), binary=False):
        auth = self.actors[actor]
        headers = {"Authorization": "Bearer " + auth["token"]}
        tenant = auth.get("active_tenant") or {}
        if isinstance(tenant, dict):
            tenant = tenant.get("id") or tenant.get("tenant_id")
        if tenant:
            headers["X-Tenant-ID"] = str(tenant)
        if body is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(
            self.base + path, data=json.dumps(body, ensure_ascii=False).encode() if body is not None else None,
            headers=headers, method=method,
        )
        try:
            with urllib.request.urlopen(request, timeout=200) as response:
                status, data = response.status, response.read()
        except urllib.error.HTTPError as exc:
            status, data = exc.code, exc.read()
        if status not in expected:
            try:
                error = json.loads(data).get("error") or json.loads(data).get("message") or ""
                if isinstance(error, dict):
                    error = error.get("message", "")
            except (ValueError, TypeError):
                error = "non-JSON response"
            raise AssertionError(f"{method} {path}: HTTP {status}; {str(error)[:400]}")
        if binary:
            return data
        return json.loads(data) if data else {}

    def data(self, *args, **kwargs):
        payload = self.call(*args, **kwargs)
        return payload.get("data", payload)


def kb_body(name, research=False):
    return {
        "name": name, "description": "Synthetic acceptance fixture; not investment evidence.",
        "type": "document", "indexing_strategy": {
            "vector_enabled": False, "keyword_enabled": False,
            "wiki_enabled": True, "graph_enabled": False,
        },
        "wiki_config": {"research_only": research},
    }


def wait_export(api, kb, export_id, timeout=180):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        result = api.data("GET", f"/knowledgebase/{kb}/research/exports/{export_id}")
        if result["status"] == "ready":
            return result
        if result["status"] == "failed":
            raise AssertionError("Export failed: " + result.get("error", "unknown"))
        time.sleep(1)
    raise TimeoutError("export did not complete")


def docx_text(content):
    with zipfile.ZipFile(io.BytesIO(content)) as archive:
        xml = ET.fromstring(archive.read("word/document.xml"))
    return "".join(xml.itertext())


def run(state_path, output_dir):
    state = json.loads(state_path.read_text())
    qa = state.get("qa", {})
    source_kb = qa.get("source_kb_id")
    org = qa.get("organization_id")
    source_ids = qa.get("fixture_ids", [])
    if not source_kb or not org or len(source_ids) < 2:
        raise RuntimeError("Run research-live-acceptance.py seed/share before draft acceptance")
    source_ids = [entry["id"] if isinstance(entry, dict) else entry for entry in source_ids]
    api = API(state)
    output_dir.mkdir(parents=True, exist_ok=True)
    report = {
        "suite": "research-drafts-and-exports",
        "synthetic_fixtures": True,
        "model_inference_tested": False,
        "retrieval_quality_tested": False,
        "checks": [],
        "artifacts": [],
    }
    local_state = {}
    local_path = ROOT / ".research-drafts-state.json"

    def passed(name, **details):
        report["checks"].append({"name": name, "status": "passed", **details})
        print("PASS " + name)

    def save_state():
        temporary = local_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(local_state, ensure_ascii=False, indent=2))
        temporary.chmod(0o600)
        temporary.replace(local_path)

    run_label = time.strftime("%Y%m%d-%H%M%S", time.gmtime())
    try:
        draft_kb = api.data("POST", "/knowledge-bases", kb_body("验收 · 研究稿 " + run_label, True))["id"]
        local_state["research_kb_id"] = draft_kb
        save_state()
        root = f"/knowledgebase/{draft_kb}/research"
        templates = api.data("GET", root + "/templates")
        assert len(templates) == 4
        passed("four_research_templates")

        draft = api.data("POST", root + "/drafts", {
            "title": "AI4S 研究稿验收（合成资料）", "template_id": "one-pager",
            "source_ids": source_ids[:2], "source_scope": "selected", "source_kb_id": source_kb,
            "content": "# AI4S 研究稿验收\n\n## 人工结论\n这是一份用于验证工作流的合成研究稿，非投资事实。\n\n"
                       "| 项目 | 状态 |\n| --- | --- |\n| 中文表格 | 待验证 |\n",
        })
        draft_id = draft["id"]
        local_state["draft_id"] = draft_id
        save_state()
        route = root + "/drafts/" + draft_id
        api.call("GET", route, actor="colleague", expected=(403, 404))
        passed("private_research_is_invisible_to_colleague")

        share = api.data("POST", f"/knowledge-bases/{draft_kb}/shares", {"organization_id": org, "permission": "editor"})
        local_state["research_share_id"] = share["id"]
        save_state()
        visible = api.data("GET", route, actor="colleague")
        assert visible["id"] == draft_id
        passed("shared_colleague_reads_same_native_page")

        # A direct native Wiki read/write is disallowed for ResearchOnly KBs.
        native = f"/knowledgebase/{draft_kb}/wiki/pages/{draft['slug']}"
        api.call("GET", native, expected=(400, 403))
        api.call("PUT", native, {"title": "bypass", "content": "bypass"}, expected=(400, 403))
        config = api.data("GET", f"/knowledge-bases/{draft_kb}")
        api.call("PUT", f"/knowledge-bases/{draft_kb}", {
            "name": config["name"], "description": config.get("description", ""),
            "config": {"wiki_config": {"research_only": False}},
        })
        protected = api.data("GET", f"/knowledge-bases/{draft_kb}")
        assert protected["wiki_config"]["research_only"] is True
        api.call("GET", native, expected=(400, 403))
        passed("native_wiki_bypass_and_research_only_downgrade_rejected")

        proposal = api.data("POST", route + "/proposals", {
            "base_version": draft["version"], "content": "## 过期候选\n不得覆盖人工新版本。",
        })
        saved = api.data("PUT", route, {"base_version": draft["version"], "title": draft["title"],
                                         "content": draft["content"] + "\n人工补充：必须保留。\n"})
        api.call("POST", route + f"/proposals/{proposal['id']}/accept", {}, expected=(409,))
        api.call("PUT", route, {"base_version": draft["version"], "title": draft["title"], "content": "过期写入"}, expected=(409,))
        current = api.data("GET", route)
        assert "人工补充" in current["content"] and current["version"] == saved["version"]
        rejected = api.data("POST", route + f"/proposals/{proposal['id']}/reject", {})
        assert rejected["status"] == "rejected"
        passed("stale_proposal_and_concurrent_save_preserve_human_revision")

        # The shared fixture organization intentionally gives the colleague a
        # viewer role. An editor KB share must not lift that organization cap.
        api.call("POST", route + "/proposals", {
            "base_version": current["version"], "content": "viewer must not publish",
        }, actor="colleague", expected=(403,))
        passed("organization_viewer_cap_blocks_research_mutation")
        proposal = api.data("POST", route + "/proposals", {
            "base_version": current["version"],
            "content": current["content"] + "\n审核通过的候选内容。\n",
        })
        accepted = api.data("POST", route + f"/proposals/{proposal['id']}/accept", {})
        assert accepted["version"] == current["version"] + 1
        assert "审核通过" in accepted["content"]
        history = api.data("GET", route + "/revisions")
        assert len(history["revisions"]) >= 2
        reverted = api.data("POST", route + "/revert", {"version": 1, "base_version": accepted["version"]})
        assert reverted["content"] == draft["content"] and reverted["version"] > accepted["version"]
        passed("review_accept_reject_and_native_revision_rollback")

        # A private source cannot publish facts into a team-visible draft.
        private_kb = api.data("POST", "/knowledge-bases", kb_body("验收 · 私密来源 " + run_label))["id"]
        private_source = api.data("POST", f"/knowledge-bases/{private_kb}/knowledge/manual", {
            "title": "私密合成访谈", "content": "仅所有者可见的合成来源。", "status": "draft",
        })
        private_request = {"title": "不得泄露", "source_ids": [private_source["id"]], "content": "private source"}
        api.call("POST", root + "/drafts", private_request, expected=(403,))
        api.call("GET", f"/knowledge-bases/{private_kb}", actor="colleague", expected=(403, 404))
        passed("private_source_cannot_expand_into_team_audience")

        temporary_share = api.data("POST", f"/knowledge-bases/{private_kb}/shares", {"organization_id": org, "permission": "viewer"})
        scoped = api.data("POST", root + "/drafts", private_request)
        scoped_route = root + "/drafts/" + scoped["id"]
        pending = api.data("POST", scoped_route + "/proposals", {"base_version": scoped["version"], "content": "候选"})
        historical_export = api.data("POST", scoped_route + "/exports", {"format": "md"})
        wait_export(api, draft_kb, historical_export["id"])
        api.call("DELETE", f"/knowledge-bases/{private_kb}/shares/{temporary_share['id']}", expected=(200, 204))
        api.call("POST", scoped_route + f"/proposals/{pending['id']}/accept", {}, expected=(403,))
        api.call("GET", scoped_route, actor="colleague", expected=(403,))
        api.call("GET", root + f"/exports/{historical_export['id']}", actor="colleague", expected=(403,))
        api.call("GET", root + f"/exports/{historical_export['id']}/download", actor="colleague", expected=(403,))
        # Restore the source audience so this intentionally revoked fixture
        # does not leave the team's list of research pages locked.
        api.data("POST", f"/knowledge-bases/{private_kb}/shares", {"organization_id": org, "permission": "viewer"})
        passed("source_revocation_is_rechecked_before_read_accept_and_historical_export")

        for format in ("md", "docx", "pdf"):
            export = api.data("POST", route + "/exports", {"format": format})
            ready = wait_export(api, draft_kb, export["id"])
            content = api.call("GET", root + f"/exports/{export['id']}/download", binary=True, actor="colleague")
            assert len(content) > 100
            if format == "md":
                assert "合成研究稿" in content.decode("utf-8")
            elif format == "docx":
                text = docx_text(content)
                assert "合成研究稿" in text and "中文表格" in text
                assert "来源与证据版本" in text and "observed_updated_at" not in text
            else:
                assert content.startswith(b"%PDF-")
            target = output_dir / ("research-draft." + format)
            target.write_bytes(content)
            report["artifacts"].append({"file": target.name, "bytes": len(content), "version": ready["version"]})
            passed("async_" + format + "_export_and_authenticated_download")

        # Starting and downloading an original-source ZIP keeps the native
        # Contributor + Editor boundary: org viewer cannot use export to bypass it.
        asset_root = f"/knowledgebase/{source_kb}/research"
        api.call("POST", asset_root + "/asset-exports", {}, actor="colleague", expected=(403,))
        export = api.data("POST", asset_root + "/asset-exports", {})
        wait_export(api, source_kb, export["id"], timeout=300)
        api.call("GET", asset_root + f"/exports/{export['id']}/download", actor="colleague", expected=(403,))
        archive_bytes = api.call("GET", asset_root + f"/exports/{export['id']}/download", binary=True)
        with zipfile.ZipFile(io.BytesIO(archive_bytes)) as archive:
            assert archive.testzip() is None
            for name in archive.namelist():
                assert not name.startswith("/") and ".." not in Path(name).parts
            manifest = json.loads(archive.read("manifest.json"))
            assert manifest["format_version"] == 1
            exported_ids = {item["id"] for item in manifest["documents"]}
            assert set(source_ids).issubset(exported_ids)
            for item in manifest["documents"]:
                markdown = archive.read(item["markdown"]).decode("utf-8")
                if item["id"] in source_ids:
                    assert markdown.strip(), "Unpublished manual-note content was lost in the archive"
        (output_dir / "project-assets.zip").write_bytes(archive_bytes)
        report["artifacts"].append({"file": "project-assets.zip", "bytes": len(archive_bytes),
                                    "documents": len(manifest["documents"])})
        passed("asset_zip_portable_manifest_and_original_download_acl")
        listing = api.data("GET", root + "/exports?page_id=" + draft_id)
        assert len(listing) >= 3
        passed("export_jobs_survive_page_reload")
        report["status"] = "passed"
    except Exception as exc:
        report["status"] = "failed"
        report["failure"] = str(exc)
        raise
    finally:
        report["finished_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        (output_dir / "drafts-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))


def run_source_assets(state_path, output_dir):
    """Read capture's uploaded/replaced synthetic fixture without changing it."""
    state = json.loads(state_path.read_text())
    qa = state.get("qa", {})
    kb, knowledge_id = qa.get("native_source_kb_id"), qa.get("original_file_id")
    expected = {qa.get("original_expected_v1"), qa.get("original_expected_v2")}
    if not kb or not knowledge_id or None in expected or len(expected) != 2:
        raise RuntimeError("Run capture acceptance --module originals before source assets acceptance")
    api = API(state)
    output_dir.mkdir(parents=True, exist_ok=True)
    report = {"suite": "immutable-source-assets", "synthetic_fixtures": True, "checks": []}

    def passed(name):
        report["checks"].append({"name": name, "status": "passed"})
        print("PASS " + name)

    try:
        versions = api.data("GET", f"/research/capture/{kb}/sources/{knowledge_id}")
        seen = set()
        markdown_read = False
        for version in versions:
            assert "original_path" not in version, "private original storage locator was exposed"
            route = f"/knowledgebase/{kb}/research/sources/{knowledge_id}/versions/{version['id']}/assets"
            if version.get("has_original"):
                original = api.call("GET", route + "/original", binary=True)
                assert hashlib.sha256(original).hexdigest() == version["content_hash"]
                seen.add(original.decode("utf-8"))
                api.call("GET", route + "/original", actor="colleague", expected=(403,))
            if version.get("markdown_path"):
                text = api.call("GET", route + "/markdown", actor="colleague", binary=True).decode("utf-8")
                assert text.strip()
                markdown_read = True
        assert expected.issubset(seen), "immutable original bytes differ from the two uploaded versions"
        assert markdown_read, "no readable parsed source version was produced"
        passed("owner_reads_both_immutable_originals_after_replacement")
        passed("shared_viewer_reads_markdown_but_cannot_download_original")

        root = f"/knowledgebase/{kb}/research"
        export = api.data("POST", root + "/asset-exports", {})
        wait_export(api, kb, export["id"], timeout=300)
        data = api.call("GET", root + f"/exports/{export['id']}/download", binary=True)
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            assert archive.testzip() is None
            manifest = json.loads(archive.read("manifest.json"))
            source = next(item for item in manifest["documents"] if item["id"] == knowledge_id)
            original_versions = set()
            for version in source.get("versions", []):
                if version.get("original"):
                    original = archive.read(version["original"])
                    assert hashlib.sha256(original).hexdigest() == version["content_hash"]
                    original_versions.add(original.decode("utf-8"))
                    assert version.get("file_name") and version.get("content_hash")
            assert expected.issubset(original_versions), "ZIP lost an immutable original version"
        (output_dir / "source-original-versions.zip").write_bytes(data)
        passed("offline_zip_manifest_resolves_both_original_versions")
        report["status"] = "passed"
    except Exception as exc:
        report["status"], report["failure"] = "failed", str(exc)
        raise
    finally:
        report["finished_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        (output_dir / "source-assets-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--state", type=Path, default=ROOT / ".research-state.json")
    parser.add_argument("--output", type=Path, default=ROOT / "artifacts/research")
    parser.add_argument("--module", choices=("drafts", "source-assets"), default="drafts")
    args = parser.parse_args()
    try:
        if args.module == "source-assets":
            run_source_assets(args.state, args.output)
        else:
            run(args.state, args.output)
    except Exception as exc:
        print("FAIL " + str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
