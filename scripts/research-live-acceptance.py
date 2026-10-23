#!/usr/bin/env python3
"""Live API acceptance for the single-base research product (Python stdlib only).

Run after deploying the custom build:
  python3 scripts/research-live-acceptance.py --module capture
  python3 scripts/research-live-acceptance.py --module move
  python3 scripts/research-live-acceptance.py --module native

Reads local credentials from .research-state.json; never prints credentials.
Creates isolated QA data, persists its IDs for resumability, and writes a JSON
report. The 50 fixtures are explicitly synthetic, unpublished native documents:
this verifies persistence/ACL/workflow, not retrieval or model quality. The move
module requires genuinely processed documents (publish fixtures using configured
models first); it never forges completed states or supplies a fake model.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import datetime as dt
import json
import os
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
STATE_PATH = ROOT / ".research-state.json"
REPORT_DIR = ROOT / "artifacts" / "research"


class Blocked(Exception):
    pass


class APIError(Exception):
    def __init__(self, method, path, status):
        super().__init__(f"{method} {path.split('?')[0]} returned HTTP {status}")
        self.status = status


class Acceptance:
    def __init__(self):
        self.state = json.loads(STATE_PATH.read_text())
        self.qa = self.state.setdefault("qa", {})
        self.report = {"started_at": dt.datetime.now(dt.timezone.utc).isoformat(),
                       "mode": "live API / synthetic fixtures; no AI-quality claims", "checks": []}
        self.base = self.state["base_url"].rstrip("/")

    def save_state(self):
        tmp = STATE_PATH.with_suffix(".json.tmp")
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as handle:
            json.dump(self.state, handle, ensure_ascii=False, indent=2)
        os.replace(tmp, STATE_PATH)
        os.chmod(STATE_PATH, 0o600)

    def tenant(self, actor):
        tenant = self.state["actors"][actor]["active_tenant"]
        return int(tenant.get("id", tenant.get("tenant_id")))

    def request(self, actor, method, path, body=None, allowed=(200, 201)):
        user = self.state["actors"][actor]
        headers = {"Authorization": "Bearer " + user["token"],
                   "X-Tenant-ID": str(self.tenant(actor))}
        data = None
        if body is not None:
            data = json.dumps(body, ensure_ascii=False).encode()
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.base + "/api/v1" + path,
                                         data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=45) as response:
                status, raw = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, raw = error.code, error.read()
        if status not in allowed:
            raise APIError(method, path, status)
        try:
            parsed = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            parsed = {}
        return status, parsed.get("data", parsed)

    def upload(self, actor, method, path, content, filename, fields=None, allowed=(200, 201)):
        boundary = "research-acceptance-" + uuid.uuid4().hex
        data = bytearray()
        for name, value in (fields or {}).items():
            data.extend(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
        data.extend(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\nContent-Type: text/plain\r\n\r\n'.encode())
        data.extend(content)
        data.extend(f"\r\n--{boundary}--\r\n".encode())
        headers = {"Authorization": "Bearer " + self.state["actors"][actor]["token"],
                   "X-Tenant-ID": str(self.tenant(actor)),
                   "Content-Type": "multipart/form-data; boundary=" + boundary}
        request = urllib.request.Request(self.base + "/api/v1" + path, data=bytes(data), headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=45) as response:
                status, raw = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, raw = error.code, error.read()
        if status not in allowed:
            raise APIError(method, path, status)
        parsed = json.loads(raw) if raw else {}
        return parsed.get("data", parsed)

    def wait_native_document(self, id_):
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            _, document = self.request("owner", "GET", f"/knowledge/{id_}")
            if document["parse_status"] == "completed":
                return document
            if document["parse_status"] == "failed":
                raise AssertionError("Native document processing failed")
            time.sleep(2)
        raise AssertionError("Native processing did not complete in 120 seconds")

    def check(self, name, fn):
        try:
            detail = fn()
            entry = {"name": name, "status": "passed"}
            if isinstance(detail, dict):
                entry["detail"] = detail
        except Blocked as error:
            entry = {"name": name, "status": "blocked", "reason": str(error)}
        except Exception as error:
            # Do not serialize API bodies, headers, actors or exceptions containing credentials.
            entry = {"name": name, "status": "failed",
                     "error": str(error) if isinstance(error, (APIError, AssertionError)) else type(error).__name__}
        self.report["checks"].append(entry)
        print(json.dumps({"check": name, "status": entry["status"]}, ensure_ascii=False))
        return entry["status"] == "passed"

    def create_kb(self, key, name):
        if not self.qa.get(key):
            _, kb = self.request("owner", "POST", "/knowledge-bases", {
                "name": name, "description": "Live acceptance fixtures; synthetic and unpublished",
                "type": "document", "indexing_strategy": {"vector_enabled": False,
                    "keyword_enabled": True, "wiki_enabled": False, "graph_enabled": False}})
            self.qa[key] = kb["id"]
            self.save_state()
        return self.qa[key]

    def fixtures(self):
        source = self.create_kb("source_kb_id", "验收资料 · 合成样本")
        self.create_kb("target_kb_id", "验收项目 · 共享资料")
        if not self.qa.get("tag_id"):
            _, tag = self.request("owner", "POST", f"/knowledge-bases/{source}/tags",
                                 {"name": "验收来源", "color": "#3B82F6"})
            self.qa["tag_id"] = tag["id"]
            self.save_state()
        ids = self.qa.setdefault("fixture_ids", [])
        for i in range(len(ids), 50):
            _, document = self.request("owner", "POST", f"/knowledge-bases/{source}/knowledge/manual", {
                "title": f"合成验收资料 {i + 1:02d}", "status": "draft", "tag_ids": [self.qa["tag_id"]],
                "content": f"# 合成验收资料 {i + 1:02d}\n\n这是一份流程验收样本，不是真实投资信息。\n\n"
                           f"编号：QA-{i + 1:02d}。主题：AI4S。负责人：验收账号。\n\n"
                           "| 检查项 | 预期 |\n| --- | --- |\n| 中文 | 保留 |\n| 来源 | 可追溯 |\n"})
            ids.append(document["id"])
            self.save_state()
        assert len(set(ids)) == 50, "Expected 50 independent native knowledge IDs"
        for item in (ids[0], ids[-1]):
            _, document = self.request("owner", "GET", f"/knowledge/{item}")
            assert document["id"] == item, "Fixture ID was not preserved"
        return {"native_manual_drafts": len(ids), "model_calls": 0}

    def inbox(self):
        _, first = self.request("owner", "POST", "/research/capture/inbox", {"template_kb_id": self.qa["source_kb_id"]})
        _, second = self.request("owner", "POST", "/research/capture/inbox", {})
        assert first["id"] == second["id"], "Inbox bootstrap is not idempotent"
        self.qa["inbox_id"] = first["id"]
        self.save_state()
        self.request("colleague", "GET", f'/knowledge-bases/{first["id"]}/knowledge', allowed=(403, 404))
        return {"idempotent": True, "other_workspace_denied": True}

    def organization(self):
        if not self.qa.get("organization_id"):
            _, org = self.request("owner", "POST", "/organizations", {"name": "验收项目协作", "description": "Synthetic acceptance project"})
            self.qa["organization_id"] = org["id"]
            self.save_state()
        org_id = self.qa["organization_id"]
        _, org = self.request("owner", "GET", f"/organizations/{org_id}")
        _, members = self.request("owner", "GET", f"/organizations/{org_id}/members")
        if isinstance(members, dict):
            members = members.get("members", [])
        if self.tenant("colleague") not in [int(m["tenant_id"]) for m in members]:
            self.request("colleague", "POST", "/organizations/join", {"invite_code": org["invite_code"]})
        for key in ("source_kb_id", "target_kb_id"):
            self.request("owner", "POST", f'/knowledge-bases/{self.qa[key]}/shares',
                         {"organization_id": org_id, "permission": "viewer"})
        return {"workspace_members": 2}

    def scope_and_revoke(self):
        source, target, org = self.qa["source_kb_id"], self.qa["target_kb_id"], self.qa["organization_id"]
        fixture = None
        for candidate in self.qa["fixture_ids"]:
            _, current = self.request("owner", "GET", f"/knowledge/{candidate}")
            if current["knowledge_base_id"] == source:
                fixture = candidate
                break
        if fixture is None:
            raise Blocked("All source fixtures have been moved; no source document remains for revocation checks")
        self.request("owner", "POST", f"/research/capture/{source}/share", {
            "target_kb_id": target, "organization_id": org,
            "selected_tenant_ids": [self.tenant("owner")], "knowledge_ids": [fixture],
            "idempotency_key": "wrong-audience-" + uuid.uuid4().hex}, allowed=(409,))
        self.request("colleague", "GET", f"/knowledge/{fixture}")
        self.request("colleague", "GET", f"/research/capture/{source}/sources/{fixture}")
        _, shares = self.request("owner", "GET", f"/knowledge-bases/{source}/shares")
        if isinstance(shares, dict):
            shares = shares.get("shares", [])
        share_id = next(share["id"] for share in shares if share["organization_id"] == org)
        self.request("owner", "DELETE", f"/knowledge-bases/{source}/shares/{share_id}")
        try:
            self.request("colleague", "GET", f"/knowledge/{fixture}", allowed=(403, 404))
            self.request("colleague", "GET", f"/research/capture/{source}/sources/{fixture}", allowed=(403, 404))
        finally:
            self.request("owner", "POST", f"/knowledge-bases/{source}/shares", {"organization_id": org, "permission": "viewer"})
        self.request("owner", "DELETE", f'/organizations/{org}/members/{self.tenant("colleague")}')
        try:
            self.request("colleague", "GET", f"/knowledge/{fixture}", allowed=(403, 404))
        finally:
            _, current_org = self.request("owner", "GET", f"/organizations/{org}")
            self.request("colleague", "POST", "/organizations/join", {"invite_code": current_org["invite_code"]})
        return {"partial_audience_rejected": True, "share_revocation_denied_read": True,
                "member_removal_denied_read": True, "source_version_acl_verified": True}

    def bookmarks(self):
        html = ('<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><DT><H3>验收</H3><DL>'
                '<DT><A HREF="https://example.com/#first" ADD_DATE="1700000000">示例网页</A>'
                '<DT><A HREF="https://example.com/#second">重复</A>'
                '<DT><A HREF="javascript:alert(1)">不支持</A></DL></DL>')
        _, preview = self.request("owner", "POST", "/research/capture/bookmarks/preview", {"html": html})
        assert len(preview["items"]) == 1 and preview["skipped"] == 2, "Bookmark preview did not deduplicate/filter"
        self.request("owner", "POST", "/research/capture/bookmarks/preview", {"html": "<html>empty</html>"}, allowed=(400,))
        kb = self.qa["inbox_id"]
        body = {"items": preview["items"], "idempotency_key": "live-acceptance-bookmarks-v1"}
        _, job = self.request("owner", "POST", f"/research/capture/{kb}/bookmarks/import", body)
        _, repeated = self.request("owner", "POST", f"/research/capture/{kb}/bookmarks/import", body)
        assert job["id"] == repeated["id"], "Repeated request created another job"
        self.qa["bookmark_job_id"] = job["id"]
        self.save_state()
        changed = {"items": [{"url": "https://example.org/", "title": "different"}], "idempotency_key": body["idempotency_key"]}
        self.request("owner", "POST", f"/research/capture/{kb}/bookmarks/import", changed, allowed=(409,))
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            _, latest = self.request("owner", "GET", f'/research/capture/{kb}/jobs/{job["id"]}')
            if latest["status"] in ("completed", "partial", "failed"):
                break
            time.sleep(2)
        assert latest["status"] in ("completed", "partial", "failed"), "Capture queue did not advance in 120 seconds"
        self.request("colleague", "GET", f'/research/capture/{kb}/jobs/{job["id"]}', allowed=(403, 404))
        return {"preview_items": 1, "skipped": 2, "job_status": latest["status"],
                "note": "Saved means handed to native ingestion; model parsing quality is not asserted."}

    def move(self):
        source, target = self.qa["source_kb_id"], self.qa["target_kb_id"]
        if self.qa.get("moved_fixture_ids"):
            for id_ in self.qa["moved_fixture_ids"]:
                _, existing = self.request("colleague", "GET", f"/knowledge/{id_}")
                assert existing["knowledge_base_id"] == target, "Previously moved fixture changed scope"
                assert any(t["name"] == "验收来源" for t in existing.get("tags", [])), "Previously moved fixture lost tags"
            return {"preserved_knowledge_ids": len(self.qa["moved_fixture_ids"]), "reused_completed_check": True}
        ready = []
        for id_ in self.qa["fixture_ids"]:
            _, document = self.request("owner", "GET", f"/knowledge/{id_}")
            if document["parse_status"] == "completed" and document["knowledge_base_id"] == source:
                ready.append(id_)
            if len(ready) == 2:
                break
        if not ready and not self.qa.get("move_request"):
            raise Blocked("No published/processed fixture. Configure real models and publish source fixtures; no completed states were forged.")

        body = self.qa.get("move_request") or {"target_kb_id": target, "organization_id": self.qa["organization_id"],
                "selected_tenant_ids": [self.tenant("owner"), self.tenant("colleague")],
                "knowledge_ids": ready, "idempotency_key": "live-move-" + uuid.uuid4().hex}
        self.qa["move_request"] = body
        ready = body["knowledge_ids"]
        self.save_state()
        _, job = self.request("owner", "POST", f"/research/capture/{source}/share", body)
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            _, job = self.request("owner", "GET", f'/research/capture/{source}/jobs/{job["id"]}')
            if job["status"] in ("completed", "partial", "failed"):
                break
            time.sleep(2)
        assert job["status"] == "completed", "Native move/share did not complete"
        for id_ in ready:
            _, moved = self.request("colleague", "GET", f"/knowledge/{id_}")
            assert moved["id"] == id_ and moved["knowledge_base_id"] == target, "Moved document was copied or incorrectly scoped"
            assert any(t["name"] == "验收来源" for t in moved.get("tags", [])), "Tags were lost during move"
        self.qa["moved_fixture_ids"] = ready
        self.save_state()
        return {"preserved_knowledge_ids": len(ready)}

    def native_move(self):
        """Exercise real native chunking/moving without a fake model or DB state edits."""
        if not self.qa.get("organization_id"):
            raise Blocked("Run capture first to establish the isolated QA organization")
        for key, title in (("native_source_kb_id", "验收 · 无模型原生源库"),
                           ("native_target_kb_id", "验收 · 无模型原生目标库")):
            if not self.qa.get(key):
                _, kb = self.request("owner", "POST", "/knowledge-bases", {
                    "name": title, "description": "Native chunk/move acceptance only; no AI quality claim",
                    "type": "document", "embedding_model_id": "", "summary_model_id": "",
                    "indexing_strategy": {"vector_enabled": False, "keyword_enabled": False,
                                          "wiki_enabled": True, "graph_enabled": False},
                    "wiki_config": {"research_only": True}})
                self.qa[key] = kb["id"]
                self.save_state()
        source, target = self.qa["native_source_kb_id"], self.qa["native_target_kb_id"]
        if not self.qa.get("native_tag_id"):
            _, tag = self.request("owner", "POST", f"/knowledge-bases/{source}/tags",
                                 {"name": "真实原生移动验收", "color": "#3B82F6"})
            self.qa["native_tag_id"] = tag["id"]
            self.save_state()
        ids = self.qa.setdefault("native_fixture_ids", [])
        for i in range(len(ids), 2):
            content = f"# 原生无模型验收 {i + 1}\n\n此文本真实经过原生分块和后处理，禁止模拟模型或直接改数据库状态。\n\n证据编号：NATIVE-{i + 1}。\n"
            _, document = self.request("owner", "POST", f"/knowledge-bases/{source}/knowledge/manual", {
                "title": f"原生无模型样本 {i + 1}", "content": content, "status": "publish",
                "tag_ids": [self.qa["native_tag_id"]],
                "process_config": {"summary_enabled": False, "graph_enabled": False,
                                   "question_generation_config": {"enabled": False}}})
            ids.append(document["id"])
            self.qa.setdefault("native_original_contents", {})[document["id"]] = content
            self.save_state()
        deadline = time.monotonic() + 120
        states = []
        while time.monotonic() < deadline:
            states = [self.request("owner", "GET", f"/knowledge/{id_}")[1]["parse_status"] for id_ in ids]
            if all(status == "completed" for status in states):
                break
            if "failed" in states:
                raise AssertionError("Native document processing failed without a model")
            time.sleep(2)
        assert all(status == "completed" for status in states), "Native processing did not complete in 120 seconds"
        originals = self.qa.setdefault("native_chunk_ids", {})
        for id_ in ids:
            _, chunks = self.request("owner", "GET", f"/chunks/{id_}")
            if isinstance(chunks, dict):
                chunks = chunks.get("chunks", [])
            assert chunks, "Native processing produced no chunks"
            if id_ not in originals:
                originals[id_] = [chunk["id"] for chunk in chunks]
        self.save_state()
        if not self.qa.get("native_move_completed"):
            # The target starts private. Selected documents must be moved before
            # the coordinator grants the org its first read-only share.
            _, first_document = self.request("owner", "GET", f"/knowledge/{ids[0]}")
            if first_document["knowledge_base_id"] == source:
                self.request("colleague", "GET", f"/knowledge/{ids[0]}", allowed=(403, 404))
            body = {"target_kb_id": target, "organization_id": self.qa["organization_id"],
                    "selected_tenant_ids": [self.tenant("owner"), self.tenant("colleague")],
                    "knowledge_ids": ids, "idempotency_key": "live-native-no-model-v1"}
            _, job = self.request("owner", "POST", f"/research/capture/{source}/share", body)
            self.qa["native_move_job_id"] = job["id"]
            self.save_state()
            deadline = time.monotonic() + 120
            while time.monotonic() < deadline:
                _, job = self.request("owner", "GET", f'/research/capture/{source}/jobs/{job["id"]}')
                if job["status"] in ("completed", "partial", "failed"):
                    break
                time.sleep(2)
            assert job["status"] == "completed", "Native same-ID move/share did not complete"
            self.qa["native_move_completed"] = True
            self.save_state()
        # Recover a previous interrupted revocation check before revalidating.
        self.request("owner", "POST", f"/knowledge-bases/{target}/shares",
                     {"organization_id": self.qa["organization_id"], "permission": "viewer"})
        for id_ in ids:
            _, document = self.request("colleague", "GET", f"/knowledge/{id_}")
            assert document["id"] == id_ and document["knowledge_base_id"] == target, "Knowledge identity/scope changed"
            assert document["metadata"]["content"] == self.qa["native_original_contents"][id_], "Original text changed"
            assert any(tag["name"] == "真实原生移动验收" for tag in document.get("tags", [])), "Tags were lost"
            _, chunks = self.request("colleague", "GET", f"/chunks/{id_}")
            if isinstance(chunks, dict):
                chunks = chunks.get("chunks", [])
            assert set(originals[id_]) == {chunk["id"] for chunk in chunks}, "Move duplicated or lost native chunks"
            assert all(chunk["knowledge_base_id"] == target for chunk in chunks), "Chunk ACL scope was not moved"
        _, shares = self.request("owner", "GET", f"/knowledge-bases/{target}/shares")
        if isinstance(shares, dict):
            shares = shares.get("shares", [])
        share_id = next(share["id"] for share in shares if share["organization_id"] == self.qa["organization_id"])
        self.request("owner", "DELETE", f"/knowledge-bases/{target}/shares/{share_id}")
        try:
            self.request("colleague", "GET", f"/knowledge/{ids[0]}", allowed=(403, 404))
            self.request("colleague", "GET", f"/chunks/{ids[0]}", allowed=(403, 404))
            self.request("colleague", "GET", f"/research/capture/{target}/sources/{ids[0]}", allowed=(403, 404))
        finally:
            self.request("owner", "POST", f"/knowledge-bases/{target}/shares",
                         {"organization_id": self.qa["organization_id"], "permission": "viewer"})
        return {"native_processed_documents": len(ids), "same_knowledge_and_chunk_ids": True,
                "original_text_and_tags_preserved": True, "revoked_content_and_chunks_denied": True,
                "configured_models": 0, "note": "Actual native chunking/postprocess/move; AI generation disabled by explicit public API configuration."}

    def original_seed(self):
        source = self.qa.get("native_source_kb_id")
        if not source:
            raise Blocked("Run native first to establish the isolated non-model QA collections")
        first = "第一版原件\n证据编号：ORIGINAL-V1\n"
        second = "第二版原件\n证据编号：ORIGINAL-V2\n"
        self.qa["original_expected_v1"] = first
        self.qa["original_expected_v2"] = second
        if not self.qa.get("original_file_id"):
            document = self.upload("owner", "POST", f"/knowledge-bases/{source}/knowledge/file",
                                   first.encode(), "provenance.txt",
                                   {"process_config": json.dumps({"summary_enabled": False,
                                       "graph_enabled": False, "question_generation_config": {"enabled": False}})})
            self.qa["original_file_id"] = document["id"]
            self.save_state()
        document = self.wait_native_document(self.qa["original_file_id"])
        assert document["knowledge_base_id"] == source
        self.save_state()
        return {"native_file_processed": True, "configured_models": 0}

    def originals(self):
        self.original_seed()
        source, id_ = self.qa["native_source_kb_id"], self.qa["original_file_id"]
        result = self.upload("owner", "PUT", f"/research/capture/{source}/sources/{id_}/file",
                             self.qa["original_expected_v2"].encode(), "provenance.txt")
        assert result["id"] == id_, "Replacing an original changed the knowledge ID"
        self.wait_native_document(id_)
        _, versions = self.request("owner", "GET", f"/research/capture/{source}/sources/{id_}")
        import hashlib
        expected = {hashlib.md5(self.qa[key].encode()).hexdigest() for key in ("original_expected_v1", "original_expected_v2")}
        assert expected.issubset({version["file_hash"] for version in versions}), "Previous or current original version is missing"
        assert all(version.get("has_original") for version in versions), "A file evidence version has no independent original"
        assert all("original_path" not in version for version in versions), "Read API leaked an original storage locator"
        self.qa["original_version_ids"] = [version["id"] for version in versions]
        self.save_state()
        repeated = self.upload("owner", "PUT", f"/research/capture/{source}/sources/{id_}/file",
                               self.qa["original_expected_v2"].encode(), "provenance.txt")
        assert repeated["id"] == id_
        _, after = self.request("owner", "GET", f"/research/capture/{source}/sources/{id_}")
        assert {v["id"] for v in after} == set(self.qa["original_version_ids"]), "Duplicate replacement created a redundant version"
        self.request("owner", "POST", f"/knowledge-bases/{source}/shares",
                     {"organization_id": self.qa["organization_id"], "permission": "viewer"})
        self.upload("colleague", "PUT", f"/research/capture/{source}/sources/{id_}/file",
                    b"must not overwrite", "provenance.txt", allowed=(403,))
        return {"same_knowledge_id": True, "original_versions": len(versions),
                "duplicate_replacement_deduplicated": True, "shared_viewer_cannot_replace": True,
                "configured_models": 0}

    def inbox_dedup(self):
        inbox = self.qa.get("inbox_id")
        if not inbox or not self.qa.get("organization_id"):
            raise Blocked("Run capture first to establish the private Inbox and QA organization")
        _, current = self.request("owner", "GET", f"/knowledge-bases/{inbox}")
        if not self.qa.get("dedup_inbox_config_pending"):
            self.qa["dedup_inbox_original_config"] = {
                "name": current["name"], "description": current.get("description", ""),
                "config": {"chunking_config": current["chunking_config"],
                           "indexing_strategy": current["indexing_strategy"]}}
        original_config = self.qa["dedup_inbox_original_config"]
        self.qa["dedup_inbox_config_pending"] = True
        self.save_state()
        try:
            # Public config only: native chunking stays active while per-upload
            # graph/summary overrides disable model work. Restore it in finally.
            temporary = dict(original_config)
            temporary["config"] = dict(original_config["config"])
            temporary["config"]["indexing_strategy"] = {
                "vector_enabled": False, "keyword_enabled": False,
                "wiki_enabled": False, "graph_enabled": True}
            self.request("owner", "PUT", f"/knowledge-bases/{inbox}", temporary)
            if not self.qa.get("dedup_target_kb_id"):
                _, target = self.request("owner", "POST", "/knowledge-bases", {
                    "name": "验收 · 去重移动目标", "type": "document",
                    "indexing_strategy": temporary["config"]["indexing_strategy"],
                    "extract_config": {"enabled": False}})
                self.qa["dedup_target_kb_id"] = target["id"]
                self.save_state()
            target = self.qa["dedup_target_kb_id"]
            content = "并发收件箱去重验收\n原生文件处理后移动，再次导入必须保持同一个知识ID。\n".encode()
            fields = {"process_config": json.dumps({"summary_enabled": False,
                      "graph_enabled": False, "question_generation_config": {"enabled": False}})}
            def upload_once(_):
                return self.upload("owner", "POST", f"/knowledge-bases/{inbox}/knowledge/file",
                                   content, "inbox-dedup.txt", fields, allowed=(200, 409))
            with ThreadPoolExecutor(max_workers=4) as pool:
                imported = list(pool.map(upload_once, range(4)))
            ids = {item["id"] for item in imported}
            assert len(ids) == 1, "Concurrent same-file uploads created different knowledge IDs"
            id_ = ids.pop()
            if self.qa.get("dedup_file_id"):
                assert id_ == self.qa["dedup_file_id"], "Repeated acceptance run duplicated its evidence"
            self.qa["dedup_file_id"] = id_
            self.save_state()
            self.wait_native_document(id_)
            body = {"target_kb_id": target, "organization_id": self.qa["organization_id"],
                    "selected_tenant_ids": [self.tenant("owner"), self.tenant("colleague")],
                    "knowledge_ids": [id_], "idempotency_key": "live-inbox-dedup-move-v1"}
            _, job = self.request("owner", "POST", f"/research/capture/{inbox}/share", body)
            deadline = time.monotonic() + 120
            while time.monotonic() < deadline:
                _, job = self.request("owner", "GET", f'/research/capture/{inbox}/jobs/{job["id"]}')
                if job["status"] in ("completed", "partial", "failed"):
                    break
                time.sleep(2)
            assert job["status"] == "completed", "Moving Inbox evidence did not finish"
            _, before = self.request("owner", "GET", f"/knowledge/{id_}")
            _, before_chunks = self.request("owner", "GET", f"/chunks/{id_}")
            _, before_versions = self.request("owner", "GET", f"/research/capture/{target}/sources/{id_}")
            assert before["knowledge_base_id"] == target, "Native move did not leave the Inbox"
            with ThreadPoolExecutor(max_workers=4) as pool:
                repeated = list(pool.map(upload_once, range(4)))
            assert {item["id"] for item in repeated} == {id_}, "Re-importing moved evidence created another knowledge ID"
            assert all(item["knowledge_base_id"] == target for item in repeated), "Re-import moved the evidence back into the Inbox"
            time.sleep(2)
            _, after = self.request("owner", "GET", f"/knowledge/{id_}")
            _, after_chunks = self.request("owner", "GET", f"/chunks/{id_}")
            _, after_versions = self.request("owner", "GET", f"/research/capture/{target}/sources/{id_}")
            # Inbox duplicates also preserve observation timestamps, so unchanged
            # evidence does not invalidate an in-flight research proposal.
            for field in ("id", "knowledge_base_id", "file_hash", "file_path", "file_size",
                          "file_type", "source", "created_at", "updated_at"):
                assert before.get(field) == after.get(field), f"Duplicate intake altered {field}"
            assert before["parse_status"] == after["parse_status"] == "completed"
            assert before_chunks == after_chunks, "Duplicate intake reparsed or rewrote native chunks"
            assert before_versions == after_versions, "Duplicate intake generated another evidence version"
            self.request("colleague", "GET", f"/knowledge/{id_}")
            self.qa["dedup_verified"] = True
            self.save_state()
            return {"concurrent_uploads": 4, "concurrent_reimports_after_move": 4,
                    "unique_knowledge_ids": 1, "same_target_scope_and_chunks": True,
                    "evidence_versions_unchanged": True, "processed_source_unchanged": True,
                    "observation_timestamps_unchanged": True, "configured_models": 0}
        finally:
            self.request("owner", "PUT", f"/knowledge-bases/{inbox}", original_config)
            self.qa["dedup_inbox_config_pending"] = False
            self.save_state()

    def finish(self):
        self.report["finished_at"] = dt.datetime.now(dt.timezone.utc).isoformat()
        statuses = [c["status"] for c in self.report["checks"]]
        self.report["result"] = "failed" if "failed" in statuses else "incomplete" if "blocked" in statuses else "passed"
        REPORT_DIR.mkdir(parents=True, exist_ok=True)
        path = REPORT_DIR / ("capture-report-" + dt.datetime.now().strftime("%Y%m%d-%H%M%S") + ".json")
        path.write_text(json.dumps(self.report, ensure_ascii=False, indent=2))
        print(json.dumps({"result": self.report["result"], "report": str(path.relative_to(ROOT))}))
        return 1 if "failed" in statuses else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--module", choices=("capture", "fixtures", "move", "native", "original-seed", "originals", "dedup"), default="capture")
    args = parser.parse_args()
    acceptance = Acceptance()
    if args.module == "dedup":
        acceptance.check("concurrent Inbox imports / same ID after moving / no reparse", acceptance.inbox_dedup)
    elif args.module == "original-seed":
        acceptance.check("native original upload and processing", acceptance.original_seed)
    elif args.module == "originals":
        acceptance.check("same-ID original replace / immutable versions / duplicate / write ACL", acceptance.originals)
    elif args.module == "native":
        acceptance.check("real native processing / same-ID move / text tags chunk ACL", acceptance.native_move)
    elif args.module == "move":
        acceptance.check("same-ID move and tag preservation", acceptance.move)
    else:
        ready = acceptance.check("50 native draft fixtures", acceptance.fixtures)
        if ready and args.module == "capture":
            inbox_ok = acceptance.check("idempotent private inbox", acceptance.inbox)
            org_ok = acceptance.check("native project and shared collections", acceptance.organization)
            if org_ok:
                acceptance.check("exact audience / revoke / member removal / source ACL", acceptance.scope_and_revoke)
            if inbox_ok:
                acceptance.check("bookmarks preview / idempotency / native queue", acceptance.bookmarks)
    return acceptance.finish()


if __name__ == "__main__":
    raise SystemExit(main())
