#!/usr/bin/env python3
"""Isolated native manual-ingest + index-rebuild Langfuse smoke.

Requires --execute after the wrapper fix is deployed. Creates only one clearly
marked QA KB and two short synthetic documents, then rebuilds that KB. Existing
KBs/models/sources are never updated. Credentials are read into memory only.
The output fixture makes the small run resumable; it is retained for inspection.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import time
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
MODEL = "9e6ef20b-38ee-43fc-b28e-cbfadbc6db06"
MARKER = "synthetic-index-observability-only"
TEXTS = (
    "此段仅用于批量索引观测验收，不提供研究事实，不用于写作证据。验收检查平台能否保存测试文字、生成向量，并关联后台任务与观测记录。",
    "此段仅用于批量索引观测验收，不代表用户写作偏好。重建只针对本测试知识库，验收后保留编号供复核，不改变已有资料和研究文稿。",
)


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def now(): return dt.datetime.now(dt.timezone.utc)
def sha(value): return hashlib.sha256(json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
def check(condition, message):
    if not condition: raise AssertionError(message)


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    fd = os.open(temporary, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
    temporary.replace(path)


class Smoke:
    def __init__(self, args):
        self.args = args
        self.fixture_path = args.output / "fixtures.json"
        self.f = json.loads(self.fixture_path.read_text()) if self.fixture_path.exists() else {
            "marker": MARKER, "actor": "ui", "since": now().isoformat(), "model_id": args.model_id,
            "documents": [], "title": "验收 · 批量索引观测 · " + now().strftime("%Y%m%d-%H%M%S")}
        check(self.f.get("marker") == MARKER and self.f.get("actor") == "ui" and self.f.get("model_id") == args.model_id, "fixture_scope_mismatch")
        self.writing = load("writing_smoke", "research-writing-acceptance.py")
        state = json.loads(args.state.read_text())
        location = urllib.parse.urlsplit(state["base_url"])
        check(location.hostname in ("localhost", "127.0.0.1", "::1") and location.port == 8090, "local_platform_required")
        self.api = self.writing.API(state, "ui")
        self.lf = load("langfuse_smoke", "research-langfuse-verify.py")
        self.reader = self.lf.LangfuseReader(args.langfuse_url, args.env_file)
        self.persist()

    def persist(self): save(self.fixture_path, self.f)

    def traced_post(self, path, body, trace_id, expected=(200, 201)):
        # A real W3C incoming context identifies this smoke's HTTP/worker tree.
        # No observation is fabricated; only the platform/exporter writes LF.
        auth = self.api.actors["owner"]
        tenant = auth.get("active_tenant") or {}
        if isinstance(tenant, dict): tenant = tenant.get("id") or tenant.get("tenant_id")
        headers = {"Authorization": "Bearer " + auth["token"], "X-Tenant-ID": str(tenant),
                   "Content-Type": "application/json", "traceparent": "00-" + trace_id + "-" + uuid.uuid4().hex[:16] + "-01"}
        request = urllib.request.Request(self.api.base + path, data=json.dumps(body, ensure_ascii=False).encode(), headers=headers, method="POST")
        try:
            with urllib.request.urlopen(request, timeout=180) as response:
                check(response.status in expected, "unexpected_platform_status")
                payload = json.load(response)
        except Exception as error:
            raise RuntimeError("traced_platform_request_failed") from None
        return payload.get("data", payload)

    def prepare(self):
        models = self.api.data("GET", "/models")
        model = next((m for m in models if m["id"] == self.args.model_id), None)
        check(model and model["type"] == "Embedding" and model["source"] == "local" and model["status"] == "active", "active_local_embedding_required")
        self.f["model_name"] = model["name"]
        if not self.f.get("knowledge_base_id"):
            kb = self.api.data("POST", "/knowledge-bases", {"name": self.f["title"], "description": MARKER,
                "type": "document", "embedding_model_id": self.args.model_id, "summary_model_id": "",
                "chunking_config": {"chunk_size": 512, "chunk_overlap": 0, "enable_parent_child": False},
                "indexing_strategy": {"vector_enabled": True, "keyword_enabled": False, "graph_enabled": False, "wiki_enabled": False},
                "extract_config": {"enabled": False}, "question_generation_config": {"enabled": False}})
            self.f["knowledge_base_id"] = kb["id"]
            self.persist()
        kb = self.api.data("GET", "/knowledge-bases/" + self.f["knowledge_base_id"])
        check(kb["name"] == self.f["title"] and kb["description"] == MARKER and kb.get("embedding_model_id") == self.args.model_id, "isolated_kb_scope_mismatch")
        for index in range(len(self.f["documents"]), len(TEXTS)):
            trace_id = uuid.uuid4().hex
            document = self.traced_post("/knowledge-bases/" + kb["id"] + "/knowledge/manual", {
                "title": "纯测试 · 索引观测说明 " + str(index + 1), "status": "publish", "content": TEXTS[index],
                "process_config": {"summary_enabled": False, "graph_enabled": False, "question_generation_config": {"enabled": False}}}, trace_id)
            self.f["documents"].append({"id": document["id"], "ingest_trace_id": trace_id, "content_sha256": sha(TEXTS[index])})
            self.persist()
        deadline = time.monotonic() + 180
        while True:
            documents = [self.api.data("GET", "/knowledge/" + item["id"]) for item in self.f["documents"]]
            check(all(d["knowledge_base_id"] == kb["id"] for d in documents), "document_scope_mismatch")
            check(all(d["parse_status"] not in ("failed", "archived") for d in documents), "manual_ingestion_failed")
            if all(d["parse_status"] in ("completed", "ready") for d in documents): break
            check(time.monotonic() < deadline, "manual_ingestion_timeout")
            time.sleep(2)
        chunks = self.chunk_snapshot()
        check(2 <= len(chunks) <= 6, "unexpected_test_chunk_count")
        if "original_chunks" in self.f: check(chunks == self.f["original_chunks"], "test_chunk_content_changed")
        self.f["original_chunks"] = chunks
        self.persist()

    def chunk_snapshot(self):
        rows = []
        for document in self.f["documents"]:
            chunks = self.api.data("GET", "/chunks/" + document["id"] + "?page=1&page_size=100")
            check(isinstance(chunks, list) and len(chunks) < 100, "unexpected_chunk_pagination")
            for chunk in chunks:
                check(chunk["knowledge_base_id"] == self.f["knowledge_base_id"] and chunk["knowledge_id"] == document["id"], "chunk_scope_mismatch")
                stable = {key: chunk.get(key) for key in ("id", "knowledge_id", "knowledge_base_id", "content", "chunk_type", "chunk_index", "is_enabled", "parent_chunk_id")}
                rows.append({"id": chunk["id"], "sha256": sha(stable)})
        return sorted(rows, key=lambda row: row["id"])

    def rebuild(self):
        route = "/knowledge-bases/" + self.f["knowledge_base_id"] + "/rebuild-index"
        if not self.f.get("task_id"):
            self.f["rebuild_trace_id"] = uuid.uuid4().hex
            self.persist()
            progress = self.traced_post(route, {}, self.f["rebuild_trace_id"], (202,))
            self.f["task_id"] = progress["task_id"]
            self.persist()
        deadline = time.monotonic() + 180
        while True:
            progress = self.api.data("GET", route)
            check(progress["task_id"] == self.f["task_id"], "task_changed")
            check(progress["status"] != "failed", "index_rebuild_failed")
            if progress["status"] == "completed": break
            check(time.monotonic() < deadline, "index_rebuild_timeout")
            time.sleep(2)
        check(progress["documents_total"] == 2 and progress["documents_indexed"] == 2 and progress["documents_skipped"] == 0, "rebuild_document_coverage_mismatch")
        check(progress["chunks_indexed"] == len(self.f["original_chunks"]), "rebuild_chunk_coverage_mismatch")
        check(self.chunk_snapshot() == self.f["original_chunks"], "rebuild_changed_test_content")
        self.f["rebuild"] = {key: progress[key] for key in ("status", "task_id", "documents_total", "documents_indexed", "chunks_total", "chunks_indexed")}
        self.persist()

    def inspect(self, rows, cases=None):
        by_id = {row["id"]: row for row in rows}
        def good(row): return row.get("endTime") and row.get("level") not in ("ERROR", "FATAL")
        links = []
        if cases is None:
            cases = [(d["ingest_trace_id"], "ingest") for d in self.f["documents"]] + [(self.f["rebuild_trace_id"], "rebuild")]
        for trace_id, kind in cases:
            tree = [row for row in rows if row["traceId"] == trace_id]
            root_name = "POST /api/v1/knowledge-bases/:id/" + ("rebuild-index" if kind == "rebuild" else "knowledge/manual")
            roots = [row for row in tree if row.get("name") == root_name and good(row)]
            check(len(roots) == 1, kind + "_http_missing")
            generations = [row for row in tree if row.get("name") == "embedding.batch_embed" and good(row)]
            check(generations, kind + "_batch_embedding_missing")
            workers = [row for row in tree if str(row.get("name", "")).startswith("asynq.") and good(row) and self.lf.is_descendant(row, roots[0], by_id)]
            if kind == "rebuild":
                workers = [row for row in workers if row["name"] == "asynq.knowledge:index_rebuild" and self.lf.metadata(row).get("task_id") == self.f["task_id"]]
            check(workers, kind + "_worker_parent_missing")
            total = 0
            for generation in generations:
                check(any(self.lf.is_descendant(generation, worker, by_id) for worker in workers), kind + "_embedding_parent_mismatch")
                check(generation.get("model") == self.f["model_name"] and self.lf.metadata(generation).get("model_id") == self.args.model_id, kind + "_embedding_model_mismatch")
                inp, out = self.lf.decode(generation.get("input")), self.lf.decode(generation.get("output"))
                check(isinstance(inp, dict) and isinstance(out, dict) and inp.get("count", 0) > 0 and inp["count"] == out.get("count"), kind + "_batch_io_mismatch")
                check(sum(self.lf.clean_numeric_map(generation.get("usageDetails")).values()) > 0, kind + "_usage_missing")
                total += inp["count"]
            if kind == "rebuild": check(total == self.f["rebuild"]["chunks_indexed"], "rebuild_embedding_coverage_mismatch")
            links.append({"kind": kind, "trace_id": trace_id, "http_observation_id": roots[0]["id"], "worker_ids": [row["id"] for row in workers], "embedding_ids": [row["id"] for row in generations], "embedded_inputs": total})
        return links

    def verify(self):
        deadline = time.monotonic() + self.args.wait_seconds
        while True:
            rows, pagination = self.reader.read_all(self.lf.timestamp(self.f["since"]), now(), 100)
            traces = {d["ingest_trace_id"] for d in self.f["documents"]} | {self.f["rebuild_trace_id"]}
            selected = [row for row in rows if row["traceId"] in traces]
            links, failures, checks = [], [], []
            cases = [(d["ingest_trace_id"], "ingest") for d in self.f["documents"]] + [(self.f["rebuild_trace_id"], "rebuild")]
            for trace_id, kind in cases:
                try:
                    links.extend(self.inspect(selected, [(trace_id, kind)]))
                    checks.append({"kind": kind, "trace_id": trace_id, "status": "passed"})
                except AssertionError as failure:
                    failures.append(str(failure))
                    checks.append({"kind": kind, "trace_id": trace_id, "status": "failed", "missing_evidence": str(failure)})
            expired = time.monotonic() >= deadline
            report = {"suite": MARKER, "status": "passed" if not failures else "failed" if expired else "pending", "checked_at": now().isoformat(),
                "knowledge_base_id": self.f["knowledge_base_id"], "model_id": self.args.model_id, "task_id": self.f["task_id"],
                "rebuild": self.f["rebuild"], "chunks_preserved": True, "pagination": pagination, "links": links,
                "checks": checks, "missing_evidence": failures, "observations": [self.lf.summarize_observation(row) for row in selected],
                "scope": "Only this isolated two-document KB. Earlier 220-chunk indexing had no batch observations and is not retroactively proven."}
            save(self.args.output / "report.json", report)
            if not failures:
                print(json.dumps({key: report[key] for key in ("status", "knowledge_base_id", "task_id", "rebuild")}))
                return 0
            if expired:
                print(json.dumps({"status": "failed", "missing_evidence": failures}))
                return 1
            time.sleep(5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--state", type=Path, default=ROOT / ".research-state.json")
    parser.add_argument("--env-file", type=Path, default=ROOT / ".research-observability.env")
    parser.add_argument("--langfuse-url", default="http://localhost:3000")
    parser.add_argument("--model-id", default=MODEL)
    parser.add_argument("--output", type=Path, default=ROOT / "artifacts/research/semantic-langfuse-acceptance/index-observability-smoke")
    parser.add_argument("--wait-seconds", type=int, default=120)
    args = parser.parse_args()
    if not args.execute:
        print(json.dumps({"status": "ready", "requires": "--execute after deployment", "creates": "one QA KB, two synthetic manual documents", "rebuilds": "only the newly created QA KB"}))
        return 0
    try:
        smoke = Smoke(args)
        smoke.prepare(); smoke.rebuild()
        return smoke.verify()
    except Exception as error:
        # Never print authentication material, HTTP bodies or arbitrary errors.
        print(json.dumps({"status": "failed", "error_type": type(error).__name__}))
        return 1


if __name__ == "__main__": sys.exit(main())
