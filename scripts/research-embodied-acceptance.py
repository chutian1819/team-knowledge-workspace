#!/usr/bin/env python3
"""Exercise real public embodied-AI sources on the existing loopback UI QA account.

Reuses the writing-acceptance API and resumes recorded fixture IDs. No model,
runtime configuration, shared business document, or credential is modified.
Run --module submit before a service rebuild, then --module inspect afterward.
After resolving a network/model blocker, --retry-blocked resubmits only sources
that did not create a knowledge record; existing documents remain untouched.
--archive-image-only explicitly requests original-image storage without OCR or
image understanding. The default never silently substitutes archival for VLM.
Exit 2 records a capability/network blocker; it never means a successful parse.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import mimetypes
import os
from pathlib import Path
import re
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "artifacts/research/product-audit"
KB_NAME = "验收 · 具身智能调研 · 资料"
PROCESS = {"summary_enabled": False, "graph_enabled": False,
           "question_generation_config": {"enabled": False}, "enable_multimodel": False}
SOURCES = {
    "pi0_pdf": {"file": "pi0.pdf", "title": "π0 · 视觉语言动作模型论文（PDF）",
                "source_url": "https://www.physicalintelligence.company/download/pi0.pdf"},
    "digitalbuddy_html": {"file": "digitalbuddy-official.html",
        "title": "DigitalBuddy · 上海人工智能实验室官方文章（保存的 HTML）",
        "source_url": "https://www.shlab.org.cn/news/5444053",
        "provenance": "公众号文章的官方网页替代；不是直接抓取微信公众号正文"},
    "digitalbuddy_image": {"file": "digitalbuddy.png",
        "title": "DigitalBuddy · 官方文章配图（独立图片）",
        "source_url": "https://www.shlab.org.cn/news/5444053"},
    "pi0_web": {"url": "https://www.pi.website/blog/pi0", "title": "π0 · Physical Intelligence 官方网页"},
    "wechat": {"url": "https://mp.weixin.qq.com/s/XvAKfKHrf42vZNJ-Nx_Eeg",
        "title": "DigitalBuddy · 上海人工智能实验室微信公众号"},
    "digitalbuddy_mirror": {"url": "https://www.shlab.org.cn/news/5444053",
        "title": "DigitalBuddy · 官方网页替代来源",
        "provenance": "官方页面指向同篇公众号文章；不是绕过登录或验证码"},
}


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def save_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(".tmp")
    fd = os.open(temporary, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as handle:
        json.dump(value, handle, ensure_ascii=False, indent=2)
    temporary.replace(path)


def safe_error(value):
    if isinstance(value, dict):
        value = value.get("message") or value.get("code") or "API request failed"
    text = str(value)
    text = re.sub(r"(?i)(?:sk-[a-z0-9_-]+|bearer\s+\S+)", "[redacted]", text)
    return text[:600]


class Acceptance:
    def __init__(self, args):
        spec = importlib.util.spec_from_file_location("writing_acceptance", ROOT / "scripts/research-writing-acceptance.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        state = json.loads(args.state.read_text())
        location = urllib.parse.urlsplit(state["base_url"])
        if location.hostname not in ("localhost", "127.0.0.1", "::1") or location.port != 8090:
            raise ValueError("this acceptance is restricted to the existing local instance on port 8090")
        self.api = module.API(state, "ui")
        self.args = args
        self.fixture_path = args.output / "embodied-fixtures.json"
        self.report_path = args.output / "embodied-report.json"
        self.fixtures = json.loads(self.fixture_path.read_text()) if self.fixture_path.exists() else {
            "actor": "ui", "base_url": state["base_url"], "created_at": now(), "sources": {}}
        assert self.fixtures["actor"] == "ui", "fixture actor mismatch"
        self.report = {"suite": "embodied-real-source-ingestion", "started_at": now(), "checks": [],
            "real_public_sources": True, "model_inference_tested": False,
            "image_understanding_tested": False, "retrieval_quality_tested": False,
            "indexing_enabled": False, "business_documents_modified": False}

    def persist(self):
        save_json(self.fixture_path, self.fixtures)
        save_json(self.report_path, self.report)

    def headers(self):
        auth = self.api.actors["owner"]
        tenant = auth.get("active_tenant") or {}
        if isinstance(tenant, dict):
            tenant = tenant.get("id") or tenant.get("tenant_id")
        return {"Authorization": "Bearer " + auth["token"], "X-Tenant-ID": str(tenant)}

    def request_bytes(self, path, data=None, content_type=None):
        headers = self.headers()
        if content_type:
            headers["Content-Type"] = content_type
        request = urllib.request.Request(self.api.base + path, data=data, headers=headers,
                                         method="POST" if data is not None else "GET")
        try:
            with urllib.request.urlopen(request, timeout=90) as response:
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.read()

    def ensure_kb(self):
        if self.fixtures.get("source_kb_id"):
            kb = self.api.data("GET", "/knowledge-bases/" + self.fixtures["source_kb_id"])
            assert kb["name"] == KB_NAME, "refusing a non-fixture knowledge base"
            return
        kb = self.api.data("POST", "/knowledge-bases", {"name": KB_NAME,
            "description": "公开具身智能资料的格式兼容性验收。来源内容真实，验收账号为合成账号；无模型时只验证存储与正文解析。",
            "type": "document", "embedding_model_id": "", "summary_model_id": "",
            "indexing_strategy": {"vector_enabled": False, "keyword_enabled": False,
                                  "wiki_enabled": False, "graph_enabled": True},
            "extract_config": {"enabled": False}})
        self.fixtures["source_kb_id"] = kb["id"]
        self.persist()

    def submit(self):
        self.ensure_kb()
        kb = self.fixtures["source_kb_id"]
        for key, source in SOURCES.items():
            previous = self.fixtures["sources"].get(key)
            if previous and (previous.get("knowledge_id") or not self.args.retry_blocked):
                continue
            item = dict(source)
            item["submission_attempts"] = list((previous or {}).get("submission_attempts", []))
            if previous:
                item["submission_attempts"].append({key: previous.get(key) for key in
                    ("http_status", "submission_error", "image_archive_only") if key in previous})
            if source.get("file"):
                path = self.args.sources / source["file"]
                content = path.read_bytes()
                item["sha256"] = hashlib.sha256(content).hexdigest()
                item["bytes"] = len(content)
                process = dict(PROCESS)
                if key == "digitalbuddy_image" and self.args.archive_image_only:
                    process["image_archive_only"] = True
                    item["image_archive_only"] = True
                boundary = "embodied-acceptance-" + uuid.uuid4().hex
                fields = {"process_config": json.dumps(process), "enable_multimodel": "false",
                    "fileName": source["file"], "metadata": json.dumps({
                        "research_source_person": source["title"],
                        "original_source_url": source["source_url"],
                        "provenance": source.get("provenance", "公开官方来源，真实格式验收")}, ensure_ascii=False)}
                body = bytearray()
                for name, value in fields.items():
                    body.extend(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
                mime = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
                body.extend(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{path.name}"\r\nContent-Type: {mime}\r\n\r\n'.encode())
                body.extend(content)
                body.extend(f"\r\n--{boundary}--\r\n".encode())
                status, raw = self.request_bytes(f"/knowledge-bases/{kb}/knowledge/file", bytes(body),
                                                  "multipart/form-data; boundary=" + boundary)
                payload = json.loads(raw)
            else:
                status, payload = self.api.raw("POST", f"/knowledge-bases/{kb}/knowledge/url",
                    {"url": source["url"], "title": source["title"], "enable_multimodel": False,
                     "process_config": PROCESS}, "owner", expected=(200, 201, 400, 409, 422, 500))
            result = payload.get("data") or {}
            item["http_status"] = status
            if result.get("id"):
                item["knowledge_id"] = result["id"]
            else:
                item["submission_error"] = safe_error(payload.get("message") or payload.get("error") or f"HTTP {status}")
            self.fixtures["sources"][key] = item
            self.persist()
            print(json.dumps({"source": key, "http_status": status, "record_created": bool(item.get("knowledge_id"))}), flush=True)

    def inspect(self):
        self.ensure_kb()
        kb = self.fixtures["source_kb_id"]
        deadline = time.monotonic() + self.args.wait
        pending = {key: value for key, value in self.fixtures["sources"].items() if value.get("knowledge_id")}
        documents = {}
        while pending:
            for key, fixture in list(pending.items()):
                document = self.api.data("GET", "/knowledge/" + fixture["knowledge_id"])
                documents[key] = document
                if document.get("parse_status") in ("completed", "failed", "cancelled", "archived"):
                    del pending[key]
            if not pending or time.monotonic() >= deadline:
                break
            time.sleep(3)
        ready = []
        for key, fixture in self.fixtures["sources"].items():
            entry = {"source": key, "status": "blocked", "original_stored": False,
                     "parsed_text": False, "image_understanding": False,
                     "image_storage_status": "not_applicable" if key != "digitalbuddy_image" else "not_stored"}
            knowledge_id = fixture.get("knowledge_id")
            if not knowledge_id:
                entry["reason"] = fixture.get("submission_error", "no knowledge record was created")
            else:
                document = documents[key]
                entry.update({"knowledge_id": knowledge_id, "parse_status": document.get("parse_status"),
                              "enable_status": document.get("enable_status"),
                              "reason": safe_error(document.get("error_message") or "")})
                if fixture.get("file"):
                    code, original = self.request_bytes(f"/knowledge/{knowledge_id}/download")
                    entry["original_stored"] = code == 200 and hashlib.sha256(original).hexdigest() == fixture["sha256"]
                    entry["original_sha256_verified"] = entry["original_stored"]
                chunks = []
                page = 1
                while True:
                    _, result = self.api.raw("GET", f"/chunks/{knowledge_id}?page_size=100&page={page}", actor="owner")
                    batch = result.get("data") or []
                    chunks.extend(batch)
                    if not batch or len(chunks) >= result.get("total", len(chunks)):
                        break
                    page += 1
                text = "\n".join(chunk.get("content", "") for chunk in chunks)
                entry["chunks"] = len(chunks)
                entry["characters"] = len(text)
                versions = self.api.data("GET", f"/research/capture/{kb}/sources/{knowledge_id}")
                entry["versions"] = [{"id": version["id"], "has_original": bool(version.get("has_original")),
                    "has_snapshot": bool(version.get("snapshot_path")), "has_markdown": bool(version.get("markdown_path")),
                    "image_paths": version.get("image_paths") or []} for version in versions]
                entry["archived_images"] = sum(len(version.get("image_paths") or []) for version in versions)
                entry["readable_archived_images"] = 0
                # Read archived text and original bytes through the authorized version API.
                for version in versions:
                    route = f"/knowledgebase/{kb}/research/sources/{knowledge_id}/versions/{version['id']}/assets/"
                    if version.get("has_original") and fixture.get("sha256"):
                        code, original = self.request_bytes(route + "original")
                        entry["immutable_original_verified"] = code == 200 and hashlib.sha256(original).hexdigest() == fixture["sha256"]
                    if version.get("markdown_path"):
                        code, raw = self.request_bytes(route + "markdown")
                        if code == 200:
                            text = raw.decode("utf-8")
                            entry["archived_markdown_characters"] = len(text)
                    if version.get("snapshot_path"):
                        code, raw = self.request_bytes(route + "snapshot")
                        entry["web_snapshot_verified"] = code == 200 and len(raw) > 100
                        if fixture.get("url"):
                            entry["original_stored"] = entry["web_snapshot_verified"]
                            entry["original_kind"] = "captured_webpage_snapshot"
                    for image_path in version.get("image_paths") or []:
                        query = urllib.parse.urlencode({"file_path": image_path})
                        code, raw = self.request_bytes(f"/knowledge-bases/{kb}/files?{query}")
                        image_signature = raw.startswith((b"\x89PNG\r\n\x1a\n", b"\xff\xd8\xff", b"GIF87a", b"GIF89a")) or (
                            raw.startswith(b"RIFF") and raw[8:12] == b"WEBP") or b"<svg" in raw[:300]
                        if code == 200 and len(raw) > 32 and image_signature:
                            entry["readable_archived_images"] += 1
                            if key == "digitalbuddy_image":
                                entry["archived_image_sha256_verified"] = hashlib.sha256(raw).hexdigest() == fixture["sha256"]
                entry["external_image_urls_remaining"] = sorted(set(re.findall(r'!\[[^\]]*\]\((https?://[^\s)]+)', text)))
                entry["external_images_fully_archived"] = not entry["external_image_urls_remaining"]
                # Known article-body markers prevent a title-bearing login,
                # navigation or anti-bot page from being counted as research.
                markers = ("laundry",) if key in ("pi0_pdf", "pi0_web") else ("DigitalBuddy", "Real", "93.3%")
                entry["article_body_markers_verified"] = all(marker.lower() in text.lower() for marker in markers)
                useful = len(text.strip()) > 300 and entry["article_body_markers_verified"]
                entry["parsed_text"] = document.get("parse_status") == "completed" and useful
                if fixture.get("image_archive_only") and document.get("parse_status") == "archived":
                    entry["image_storage_status"] = "passed" if (entry["original_stored"] and
                        entry.get("immutable_original_verified") and entry.get("archived_image_sha256_verified") and
                        entry["readable_archived_images"] >= 1 and not chunks and document.get("enable_status") == "disabled") else "failed"
                    entry["status"] = "stored_only" if entry["image_storage_status"] == "passed" else "failed"
                    entry["image_understanding_status"] = "blocked_no_visual_model"
                    entry["reason"] = "Explicit original-only archival; zero recognized text chunks; OCR and image understanding remain unverified"
                    entry["draft_text_evidence_rejected"] = self.reject_archived_image_as_text(knowledge_id)
                elif entry["parsed_text"]:
                    entry["status"] = "passed"
                    ready.append(knowledge_id)
                elif document.get("parse_status") == "completed":
                    entry["status"] = "failed"
                    entry["reason"] = "Parser reported completion but relevant article text was not recovered; an access/verification page is not source content"
                elif key == "digitalbuddy_image":
                    entry["image_understanding_status"] = "blocked_no_visual_model"
                    entry["reason"] = entry["reason"] or "Standalone image requires a real multimodal model; original storage is separate from image understanding"
                elif key in pending:
                    entry["reason"] = "Parsing did not reach a terminal state within this inspection window"
                if fixture.get("file") and not entry["original_stored"]:
                    entry["status"] = "failed"
                    entry["reason"] = "Uploaded original could not be read back with the same SHA-256"
                if entry["readable_archived_images"] != entry["archived_images"]:
                    entry["status"] = "failed"
                    entry["reason"] = "An archived image path did not resolve to readable image bytes through the authorized knowledge-base file route"
            self.report["checks"].append(entry)
            fixture["verification"] = entry
            print(json.dumps({"source": key, "status": entry["status"], "parse_status": entry.get("parse_status"),
                              "original_stored": entry["original_stored"], "parsed_text": entry["parsed_text"]}), flush=True)
            self.persist()
        self.fixtures["ready_source_ids"] = ready
        self.fixtures["source_ids"] = ready
        self.report["finished_at"] = now()
        self.report["status"] = "failed" if any(check["status"] == "failed" for check in self.report["checks"]) else (
            "blocked" if any(check["status"] in ("blocked", "stored_only") for check in self.report["checks"]) else "passed")
        self.persist()

    def reject_archived_image_as_text(self, knowledge_id):
        writing_path = self.args.output / "embodied-writing-fixtures.json"
        assert writing_path.exists(), "create the isolated embodied writing fixture before checking image evidence rejection"
        writing = json.loads(writing_path.read_text())
        assert writing["actor"] == "ui" and writing["source_kb_id"] == self.fixtures["source_kb_id"]
        kb = self.api.data("GET", "/knowledge-bases/" + writing["research_kb_id"])
        assert kb["name"] == "验收 · 具身智能调研 · 研究稿"
        route = f"/knowledgebase/{writing['research_kb_id']}/research/drafts"
        before = {draft["id"] for draft in self.api.data("GET", route)}
        status, _ = self.api.raw("POST", route, {"title": "原图文字证据拒绝验收（不应创建）",
            "template_id": "brief", "source_scope": "selected", "source_ids": [knowledge_id],
            "source_kb_id": self.fixtures["source_kb_id"], "content": "原图尚未识别，不得冒充文字来源。"},
            "owner", expected=(400,))
        after = {draft["id"] for draft in self.api.data("GET", route)}
        assert before == after, "rejected image evidence created a research draft"
        return status == 400


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--module", choices=("submit", "inspect", "all"), default="all")
    parser.add_argument("--state", type=Path, default=ROOT / ".research-state.json")
    parser.add_argument("--output", type=Path, default=OUTPUT)
    parser.add_argument("--sources", type=Path, default=OUTPUT / "sources")
    parser.add_argument("--wait", type=int, default=180)
    parser.add_argument("--retry-blocked", action="store_true")
    parser.add_argument("--archive-image-only", action="store_true",
        help="explicitly save the standalone image original without recognition; never include it in ready_source_ids")
    args = parser.parse_args()
    suite = Acceptance(args)
    try:
        if args.module in ("submit", "all"):
            suite.submit()
        if args.module in ("inspect", "all"):
            suite.inspect()
    finally:
        suite.persist()
    print(json.dumps({"status": suite.report.get("status", "submitted"), "fixture_path": str(suite.fixture_path),
                      "source_kb_id": suite.fixtures["source_kb_id"]}, ensure_ascii=False))
    return {"failed": 1, "blocked": 2}.get(suite.report.get("status"), 0)


if __name__ == "__main__":
    raise SystemExit(main())
