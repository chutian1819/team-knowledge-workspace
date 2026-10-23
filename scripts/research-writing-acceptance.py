#!/usr/bin/env python3
"""Verify live writing memory with isolated, explicitly synthetic fixtures.

Only loopback instances and the existing @example.invalid QA actors are allowed.
Login refreshes live in memory; tokens, model endpoints and API bodies are never
included in output. The script creates its own source/research KBs and changes
only those fixtures. It never configures models or simulates model responses.

  python3 scripts/research-writing-acceptance.py --module all
  python3 scripts/research-writing-acceptance.py --module model_real --model-id ID

Exit 0 means every requested check passed; 1 means a failure; 2 means one or more
real-model checks are blocked. Model inference and writing quality are distinct:
even a passing real-model workflow is not a measured personal style improvement.
"""
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
QA_PREFIX = "验收 · 写作经验 · 合成"


class APIError(Exception):
    def __init__(self, method, path, status):
        super().__init__(f"{method} {path.split('?')[0]}: HTTP {status}")
        self.status = status


class Blocked(Exception):
    pass


class API:
    def __init__(self, state, primary_actor="owner"):
        self.base = state["base_url"].rstrip("/")
        if urllib.parse.urlsplit(self.base).hostname not in ("localhost", "127.0.0.1", "::1"):
            raise ValueError("acceptance is restricted to loopback")
        if not self.base.endswith("/api/v1"):
            self.base += "/api/v1"
        self.actors = {}
        for actor, state_actor in (("owner", primary_actor), ("colleague", "colleague")):
            account = state["actors"][state_actor]
            if not account["email"].endswith("@example.invalid"):
                raise ValueError("existing synthetic QA actors are required")
            _, login = self.raw("POST", "/auth/login", {
                key: account[key] for key in ("email", "password")})
            self.actors[actor] = login

    def raw(self, method, path, body=None, actor=None, expected=(200, 201), timeout=220):
        headers = {}
        if actor:
            auth = self.actors[actor]
            headers["Authorization"] = "Bearer " + auth["token"]
            tenant = auth.get("active_tenant") or {}
            if isinstance(tenant, dict):
                tenant = tenant.get("id") or tenant.get("tenant_id")
            headers["X-Tenant-ID"] = str(tenant)
        if body is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.base + path, method=method,
            data=json.dumps(body, ensure_ascii=False).encode() if body is not None else None,
            headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                status, content = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, content = error.code, error.read()
        if status not in expected:
            raise APIError(method, path, status)
        try:
            payload = json.loads(content) if content else {}
        except ValueError:
            raise AssertionError("API returned non-JSON response") from None
        return status, payload

    def data(self, method, path, body=None, actor="owner", expected=(200, 201)):
        _, payload = self.raw(method, path, body, actor, expected)
        return payload.get("data", payload)


def kb_body(name, research):
    return {"name": name, "description": "Synthetic QA only; not research or investment evidence.",
        "type": "document", "embedding_model_id": "", "summary_model_id": "",
        # The native keyword path also requests an embedding model. For an
        # isolated, model-free source fixture use the same explicit index-off
        # configuration as research-live-acceptance.py's native dedup suite.
        # Per-document graph overrides below disable graph inference as well.
        "indexing_strategy": {"vector_enabled": False, "keyword_enabled": False,
                              "wiki_enabled": research, "graph_enabled": not research},
        "extract_config": {"enabled": False},
        "wiki_config": {"research_only": research}}


class Acceptance:
    def __init__(self, args):
        self.args = args
        self.state = json.loads(args.state.read_text())
        self.api = API(self.state, args.actor)
        args.output.mkdir(parents=True, exist_ok=True)
        self.fixture_path = args.fixtures or args.output / ("fixtures-" + args.actor + ".json")
        self.fixtures = json.loads(self.fixture_path.read_text()) if self.fixture_path.exists() and not args.new_fixtures else {}
        self.preflight_read_only = False
        if self.fixtures and self.fixtures.get("actor", args.actor) != args.actor:
            raise ValueError("resumed QA fixtures belong to another actor")
        self.report = {"suite": "research-writing-memory", "started_at": self.now(),
            "synthetic_fixtures": True, "module": args.module, "actor": args.actor, "checks": [],
            "model_inference_tested": False, "writing_quality_measured": False,
            "retrieval_quality_tested": False,
            "external_sync_performed": False, "business_documents_modified": False}
        self.models = self.api.data("GET", "/models")
        if not isinstance(self.models, list):
            raise AssertionError("model list must be an array")
        self.report["models"] = [{"id": model.get("id"), "name": model.get("name"),
            "status": model.get("status"), "type": model.get("type"),
            "provider": (model.get("parameters") or {}).get("provider"),
            "credentials_configured": {key: bool(value.get("configured", False))
                for key, value in (model.get("credentials") or {}).items() if isinstance(value, dict)}}
            for model in self.models]
        eligible = [model for model in self.models
                    if model.get("type") == "KnowledgeQA" and model.get("status") == "active"]
        self.active_models = eligible
        if args.model_id:
            eligible = [model for model in eligible if model.get("id") == args.model_id]
        # Retained provider profiles may be active without verified credentials.
        # Never silently pick the first one when the operator has several.
        self.model_id = eligible[0]["id"] if len(eligible) == 1 else ""

    @staticmethod
    def now():
        return dt.datetime.now(dt.timezone.utc).isoformat()

    def save_fixtures(self):
        self.fixture_path.parent.mkdir(parents=True, exist_ok=True)
        temporary = self.fixture_path.with_suffix(".tmp")
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as handle:
            json.dump(self.fixtures, handle, ensure_ascii=False, indent=2)
        temporary.replace(self.fixture_path)

    def check(self, name, callback):
        try:
            details = callback()
            entry = {"name": name, "status": "passed"}
            if details:
                entry["details"] = details
        except Blocked as error:
            entry = {"name": name, "status": "blocked", "reason": str(error)}
        except Exception as error:
            entry = {"name": name, "status": "failed", "error":
                str(error) if isinstance(error, (APIError, AssertionError)) else type(error).__name__}
        self.report["checks"].append(entry)
        print(json.dumps({key: entry[key] for key in ("name", "status")}, ensure_ascii=False), flush=True)
        return entry["status"] == "passed"

    @property
    def root(self):
        return f"/knowledgebase/{self.fixtures['research_kb_id']}/research"

    @property
    def route(self):
        return self.root + "/drafts/" + self.fixtures["draft_id"]

    def current(self):
        return self.api.data("GET", self.route)

    def events(self):
        events = self.api.data("GET", self.route + "/writing-events")
        assert isinstance(events, list), "writing events must be an array"
        return events

    def settings(self, **updates):
        return self.api.data("POST" if updates else "GET", self.route + "/writing-learning", updates or None)

    def save(self, content):
        draft = self.current()
        return self.api.data("PUT", self.route, {"base_version": draft["version"],
            "title": draft["title"], "content": content})

    def event_for(self, kind, version):
        return next(event for event in self.events()
                    if event["kind"] == kind and event.get("result_version") == version)

    def require_model(self):
        if not self.model_id:
            if not self.args.model_id and len(self.active_models) > 1:
                raise Blocked("Several active KnowledgeQA profiles are visible; pass --model-id for the explicitly verified model")
            raise Blocked("No active KnowledgeQA model is visible to the selected existing synthetic QA actor; no fake model configured")

    def fixtures_ready(self):
        # The permissions module deliberately shares its KBs. Replaying the
        # private-to-shared workflow on those same objects cannot test an
        # initially private KB, and previously failed only after editing them.
        # Real-model continuation is different: it intentionally reuses the
        # recorded writing sample and the established viewer grant.
        if (self.fixtures.get("organization_id") and
                self.args.module in ("workflow_no_model", "all")):
            self.preflight_read_only = True
            raise Blocked("These synthetic fixtures were already shared by an earlier permissions check; "
                          "run with --new-fixtures and a new --output directory, omitting --fixtures. "
                          "No existing fixture content or permissions were changed.")
        # Validate every resumed ID before mutations; never reuse an ordinary KB.
        if self.fixtures:
            for key in ("source_kb_id", "research_kb_id"):
                kb = self.api.data("GET", "/knowledge-bases/" + self.fixtures[key])
                assert kb["name"].startswith(QA_PREFIX), "refusing a non-QA knowledge base"
            self.current()
            return {"resumed": True}
        label = dt.datetime.now(dt.timezone(dt.timedelta(hours=8))).strftime("%Y%m%d-%H%M%S")
        self.fixtures["actor"] = self.args.actor
        self.fixtures["source_kb_id"] = self.api.data("POST", "/knowledge-bases",
            kb_body(QA_PREFIX + "来源 " + label, False))["id"]
        self.fixtures["research_kb_id"] = self.api.data("POST", "/knowledge-bases",
            kb_body(QA_PREFIX + "文稿 " + label, True))["id"]
        self.fixtures["run_label"] = label
        self.save_fixtures()
        source_ids = []
        for index in range(2):
            document = self.api.data("POST", f"/knowledge-bases/{self.fixtures['source_kb_id']}/knowledge/manual", {
                "title": f"合成写作资料 {index + 1}", "status": "publish",
                "content": f"# 合成写作资料 {index + 1}\n\n本段仅用于平台写作流程验收，全部内容均为合成。\n\n"
                           "样例方案仍处验证阶段。已有材料仅能支持继续小规模验证，不能支持商业成效结论。\n\n"
                           "下一步是核实样例数据来源并补齐风险说明。\n",
                "process_config": {"summary_enabled": False, "graph_enabled": False,
                    "question_generation_config": {"enabled": False}}})
            source_ids.append(document["id"])
        self.fixtures["source_ids"] = source_ids
        self.save_fixtures()
        deadline = time.monotonic() + 150
        while time.monotonic() < deadline:
            statuses = [self.api.data("GET", "/knowledge/" + identifier)["parse_status"] for identifier in source_ids]
            if all(status == "completed" for status in statuses):
                break
            assert "failed" not in statuses, "synthetic source native processing failed"
            time.sleep(1)
        assert all(status == "completed" for status in statuses), "synthetic sources were not processed before deadline"
        before = "# 合成写作流程验收\n\n## 背景\n值得注意的是，样例方案具有十分巨大的潜力，未来有望实现全面的突破，需要进一步做好相关工作。\n"
        draft = self.api.data("POST", self.root + "/drafts", {
            "title": QA_PREFIX + "研究简报 " + label, "template_id": "brief", "content": before,
            "source_scope": "selected", "source_kb_id": self.fixtures["source_kb_id"], "source_ids": source_ids})
        self.fixtures.update({"draft_id": draft["id"], "initial_content": draft["content"],
            "initial_version": draft["version"]})
        self.save_fixtures()
        return {"native_processed_sources": len(source_ids), "new_qa_knowledge_bases": 2}

    def source_selection(self):
        draft = self.current()
        assert {item.split("|", 1)[0] for item in draft["source_refs"]} == set(self.fixtures["source_ids"])
        all_draft = self.api.data("POST", self.root + "/drafts", {
            "title": QA_PREFIX + "全库选择 " + self.fixtures["run_label"], "template_id": "brief",
            "source_scope": "all", "source_kb_id": self.fixtures["source_kb_id"]})
        assert {item.split("|", 1)[0] for item in all_draft["source_refs"]} == set(self.fixtures["source_ids"])
        return {"selected_sources_and_whole_knowledge_base": True}

    def save_journal(self):
        assert self.settings()["enabled"] is False, "learning must default to off"
        self.settings(enabled=False, model_id="")
        before = self.current()
        content = "# 合成写作流程验收\n\n## 核心判断\n继续小规模验证。\n\n## 证据与边界\n材料尚不足以支持商业成效结论。\n\n## 下一步\n核实数据来源，补齐风险说明。\n"
        saved = self.save(content)
        event = self.event_for("save", saved["version"])
        assert event["base_content"] == before["content"] and event["content"] == content
        assert event["base_version"] == before["version"] and event["result_version"] == saved["version"]
        assert event.get("instruction", "") == "", "manual saves must not invent user prompts"
        event_ids = {item["id"] for item in self.events()}
        self.api.data("PUT", self.route, {"base_version": before["version"],
            "title": before["title"], "content": "不得写入的过期合成版本"}, expected=(409,))
        assert {item["id"] for item in self.events()} == event_ids, "failed CAS created a journal event"
        assert self.current()["content"] == content
        self.fixtures["style_save_event_id"] = event["id"]
        self.save_fixtures()
        no_change = self.save(saved["content"])
        assert no_change["version"] == saved["version"], "unchanged save created a new document version"
        return {"event_id": event["id"], "before_after_preserved": True, "stale_save_rejected": True,
                "unchanged_save_did_not_create_revision": True}

    def manual_rules(self):
        before = self.current()
        instruction = "每次写作先给简短结论，再写证据边界和下一步；删除空泛形容词，使用短句。"
        proposal = self.api.data("POST", self.route + "/proposals", {"base_version": before["version"],
            "instruction": instruction, "content": before["content"] + "\n合成审阅补充：区分判断和待验证事项。\n"})
        self.api.data("POST", self.root + "/writing-rules", {"name": "未接受来源不得学习",
            "instruction": instruction, "template_id": "brief", "source_proposal_id": proposal["id"]}, expected=(400,))
        accepted = self.api.data("POST", self.route + f"/proposals/{proposal['id']}/accept", {})
        event = self.event_for("accept", accepted["version"])
        assert event["base_content"] == proposal["base_content"] and event["content"] == proposal["content"]
        candidate = self.api.data("POST", self.root + "/writing-rules", {"name": "合成 · 结论先行",
            "instruction": instruction, "template_id": "brief", "source_proposal_id": proposal["id"]})
        assert candidate["status"] == "candidate" and candidate["version"] == 1
        self.api.data("POST", self.route + "/proposals", {"base_version": accepted["version"],
            "instruction": "合成验收", "writing_rule_ids": [candidate["id"]]}, expected=(400,))
        active = self.api.data("POST", self.root + f"/writing-rules/{candidate['id']}/decide", {
            "base_version": 1, "status": "active"})
        assert active["status"] == "active" and active["version"] == 2
        assert active["metadata"].get("approved_by") and active["metadata"].get("approved_at")
        self.api.data("POST", self.root + f"/writing-rules/{candidate['id']}/decide", {
            "base_version": 1, "status": "revoked"}, expected=(409,))
        self.api.data("POST", self.root + f"/writing-rules/{candidate['id']}/decide", {
            "base_version": 2, "status": "active", "instruction": "不得覆盖启用版本"}, expected=(400,))
        self.api.data("POST", self.route + "/proposals", {"base_version": accepted["version"],
            "content": "手工正文不能伪造实际模型使用", "writing_rule_ids": [active["id"]]}, expected=(400,))
        self.api.data("POST", self.route + f"/proposals/{proposal['id']}/writing-feedback", {
            "rule_id": active["id"], "rating": "useful", "reason": "未使用该经验不能反馈"}, expected=(400,))
        self.fixtures.update({"accepted_manual_proposal_id": proposal["id"], "active_manual_rule": active})
        self.save_fixtures()
        return {"candidate_id": candidate["id"], "accepted_proposal_id": proposal["id"], "cas_and_immutable_active_rule": True}

    def revoke_manual(self):
        original = self.fixtures["active_manual_rule"]
        revoked = self.api.data("POST", self.root + f"/writing-rules/{original['id']}/decide", {
            "base_version": original["version"], "status": "revoked"})
        assert revoked["status"] == "revoked" and revoked["version"] == original["version"] + 1
        assert revoked["metadata"].get("revoked_by") and revoked["metadata"].get("revoked_at")
        draft = self.current()
        self.api.data("POST", self.route + "/proposals", {"base_version": draft["version"],
            "instruction": "合成测试", "writing_rule_ids": [original["id"]]}, expected=(400,))
        proposals = self.api.data("GET", self.route + "/proposals")
        assert next(item for item in proposals if item["id"] == self.fixtures["accepted_manual_proposal_id"])["status"] == "accepted"
        return {"future_use_rejected": True, "old_proposal_retained": True}

    def version_revert_journal(self):
        before = self.current()
        reverted = self.api.data("POST", self.route + "/revert", {
            "version": self.fixtures["initial_version"], "base_version": before["version"]})
        assert reverted["content"] == self.fixtures["initial_content"] and reverted["version"] == before["version"] + 1
        event = self.event_for("revert", reverted["version"])
        assert event["base_content"] == before["content"] and event["content"] == reverted["content"]
        ids = {item["id"] for item in self.events()}
        self.api.data("POST", self.route + "/revert", {
            "version": self.fixtures["initial_version"], "base_version": before["version"]}, expected=(409,))
        assert {item["id"] for item in self.events()} == ids
        return {"restored_initial_content_as_new_version": True, "real_diff_journaled": True}

    def no_model_journal(self):
        if self.active_models:
            return {"not_applicable": "an active real model is configured; live generation is checked in model_real"}
        draft = self.current()
        prompt = "合成提示：请用短句，先结论再给证据边界；保留全部事实与引用。"
        ids = {item["id"] for item in self.events()}
        self.api.data("POST", self.route + "/proposals", {"base_version": draft["version"],
            "instruction": prompt}, expected=(400,))
        added = [item for item in self.events() if item["id"] not in ids]
        submitted = [item for item in added if item["kind"] == "prompt"]
        failed = [item for item in added if item["kind"] == "generation_failed"]
        assert len(submitted) == len(failed) == 1, "failed generation must keep both prompt and failure journal"
        assert submitted[0]["instruction"] == prompt and submitted[0]["base_content"] == draft["content"]
        assert self.current()["version"] == draft["version"], "failed generation changed the document"
        self.api.data("POST", self.route + f"/writing-events/{self.fixtures['style_save_event_id']}/extract", {}, expected=(400,))
        self.settings(enabled=True, model_id="")
        saved = self.save(draft["content"] + "\n合成自动学习失败时，人工内容仍须成功保存。\n")
        event = self.event_for("save", saved["version"])
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            event = next(item for item in self.events() if item["id"] == event["id"])
            if event.get("learning_status") == "failed":
                break
            time.sleep(1)
        assert event.get("learning_status") == "failed", "automatic learning without a model must expose failed status"
        assert self.settings().get("status") == "failed", "learning status hid an automatic extraction failure"
        assert self.current()["content"] == saved["content"], "learning failure rolled back a successful human save"
        self.settings(enabled=False, model_id="")
        disabled_saved = self.save(saved["content"] + "\n合成关闭自动学习后的版本。\n")
        disabled_event = self.event_for("save", disabled_saved["version"])
        assert disabled_event.get("learning_status") not in ("queued", "running", "completed", "failed"), "disabled learning still scheduled extraction"
        return {"prompt_and_generation_failure_recorded": True, "auto_failure_did_not_rollback_save": True,
                "off_stops_automatic_extraction": True}

    def permissions(self):
        # The group is also isolated so a model configured by the UI actor never
        # needs membership changes in an older QA or business organization.
        self.api.data("GET", self.root + "/writing-rules", actor="colleague", expected=(403, 404))
        org_record = self.api.data("POST", "/organizations", {
            "name": QA_PREFIX + "权限组 " + self.fixtures["run_label"],
            "description": "Synthetic writing acceptance only; viewer ACL fixture."})
        org = org_record["id"]
        self.fixtures["organization_id"] = org
        self.save_fixtures()
        # The invite code is used in memory only and excluded from fixtures/report.
        org_record = self.api.data("GET", f"/organizations/{org}")
        self.api.data("POST", "/organizations/join", {"invite_code": org_record["invite_code"]}, actor="colleague")
        tenant = self.api.actors["colleague"]["active_tenant"]
        tenant_id = tenant.get("id") or tenant.get("tenant_id") if isinstance(tenant, dict) else tenant
        self.api.data("PUT", f"/organizations/{org}/members/{tenant_id}", {"role": "viewer"})
        for key in ("source_kb_id", "research_kb_id"):
            share = self.api.data("POST", f"/knowledge-bases/{self.fixtures[key]}/shares", {
                "organization_id": org, "permission": "editor"})
            self.fixtures[key + "_share_id"] = share["id"]
        self.save_fixtures()
        assert isinstance(self.api.data("GET", self.root + "/writing-rules", actor="colleague"), list)
        assert isinstance(self.api.data("GET", self.route + "/writing-events", actor="colleague"), list)
        self.api.data("POST", self.root + "/writing-rules", {"name": "viewer forbidden",
            "instruction": "viewer forbidden", "template_id": "brief"}, actor="colleague", expected=(403,))
        self.api.data("POST", self.route + "/writing-learning", {"enabled": True}, actor="colleague", expected=(403,))
        current = self.current()
        self.api.data("PUT", self.route, {"base_version": current["version"], "title": current["title"],
            "content": "viewer forbidden"}, actor="colleague", expected=(403,))
        rule = self.fixtures["active_manual_rule"]
        self.api.data("POST", self.root + f"/writing-rules/{rule['id']}/decide", {
            "base_version": rule["version"], "status": "active"}, actor="colleague", expected=(403,))
        self.api.data("POST", self.route + f"/writing-events/{self.fixtures['style_save_event_id']}/extract", {},
            actor="colleague", expected=(403,))
        self.api.data("POST", self.route + f"/proposals/{self.fixtures['accepted_manual_proposal_id']}/writing-feedback", {
            "rule_id": rule["id"], "rating": "useful", "reason": "viewer forbidden"}, actor="colleague", expected=(403,))
        return {"private_read_denied": True, "shared_viewer_can_read": True, "viewer_cannot_mutate_or_learn": True}

    def real_event_extraction(self):
        self.require_model()
        # The no-model sample deliberately also corrects an unsupported claim.
        # A real model may correctly decline to learn style from that change.
        # Keep it in history and create a separate fact-preserving manual edit.
        # A fresh event also makes reruns independent of previously revoked
        # candidates without changing the extraction endpoint's idempotency.
        self.settings(enabled=False, model_id=self.model_id)
        verbose, concise = self.manual_style_sample()
        before = self.save(verbose)
        saved = self.save(concise)
        event = self.event_for("save", saved["version"])
        assert event["base_content"] == before["content"] and event["content"] == concise
        event_id = event["id"]
        self.fixtures["real_style_save_event_id"] = event_id
        self.save_fixtures()
        rules = self.api.data("POST", self.route + f"/writing-events/{event_id}/extract", {"model_id": self.model_id})
        self.report["model_inference_tested"] = True
        assert isinstance(rules, list) and len(rules) <= 3
        assert all(rule["status"] == "candidate" and rule["source_event_id"] == event_id for rule in rules)
        assert all(rule.get("metadata", {}).get("model_id") == self.model_id for rule in rules)
        again = self.api.data("POST", self.route + f"/writing-events/{event_id}/extract", {"model_id": self.model_id})
        assert {rule["id"] for rule in again} == {rule["id"] for rule in rules}, "successful event extraction was not idempotent"
        if not rules:
            raise Blocked("Real model returned no reusable style candidates; empty extraction is valid but full candidate reuse remains unverified")
        candidate = rules[0]
        active = self.api.data("POST", self.root + f"/writing-rules/{candidate['id']}/decide", {
            "base_version": candidate["version"], "status": "active"})
        self.fixtures["active_real_rule"] = active
        self.save_fixtures()
        return {"model_id": self.model_id, "event_id": event_id, "candidate_count": len(rules),
                "confirmed_rule_id": active["id"], "fact_preserving_manual_style_sample": True, "idempotent": True}

    def real_reuse_feedback(self):
        self.require_model()
        rule = self.fixtures.get("active_real_rule")
        if not rule:
            raise Blocked("No real extracted candidate has been confirmed")
        draft = self.current()
        prompt = "只依据所选合成知识库材料改写全文。先给简短结论，再列证据边界和下一步；使用短句，去掉空泛形容词。每个事实保留有效引用，不添加新事实。"
        old_events = {event["id"] for event in self.events()}
        proposal = self.api.data("POST", self.route + "/proposals", {"base_version": draft["version"],
            "instruction": prompt, "model_id": self.model_id, "writing_rule_ids": [rule["id"]]})
        self.report["model_inference_tested"] = True
        snapshots = proposal["writing_rules"]
        assert len(snapshots) == 1 and snapshots[0] == {key: rule[key] for key in ("id", "version", "name", "instruction")}
        assert proposal["model_id"] == self.model_id and proposal["instruction"] == prompt
        assert "knowledge://" in proposal["content"], "real generated source-based draft has no evidence citations"
        added = [event for event in self.events() if event["id"] not in old_events]
        submitted = [event for event in added if event["kind"] == "prompt"]
        generated = [event for event in added if event["kind"] == "proposal"]
        assert len(submitted) == len(generated) == 1
        assert submitted[0]["instruction"] == prompt and submitted[0]["base_content"] == draft["content"]
        assert generated[0]["proposal_id"] == proposal["id"] and generated[0]["content"] == proposal["content"]
        accepted = self.api.data("POST", self.route + f"/proposals/{proposal['id']}/accept", {})
        assert accepted["version"] == draft["version"] + 1 and accepted["content"] == proposal["content"]
        for rating in ("useful", "useless", "misapplied", "useful"):
            feedback = self.api.data("POST", self.route + f"/proposals/{proposal['id']}/writing-feedback", {
                "rule_id": rule["id"], "rating": rating, "reason": "合成验收：检验反馈更新与真实使用关联。"})
            assert feedback["rating"] == rating and feedback["is_mine"] is True
        rows = self.api.data("GET", self.route + f"/proposals/{proposal['id']}/writing-feedback")
        own = [item for item in rows if item["rule_id"] == rule["id"] and item["is_mine"]]
        assert len(own) == 1 and own[0]["rating"] == "useful", "feedback upsert created duplicates"
        extracted = self.api.data("POST", self.route + "/writing-rules/extract", {
            "proposal_id": proposal["id"], "model_id": self.model_id})
        assert all(item["status"] == "candidate" for item in extracted), "feedback extraction auto-published rules"
        for candidate in extracted:
            feedback_snapshot = candidate.get("metadata", {}).get("writing_feedback", [])
            assert any(item["rule_id"] == rule["id"] and item["rating"] == "useful" for item in feedback_snapshot), "feedback missing from extraction provenance"
        self.fixtures.update({"accepted_real_proposal_id": proposal["id"], "real_writing_snapshot": snapshots})
        self.save_fixtures()
        return {"proposal_id": proposal["id"], "actual_rule_version_snapshotted": True,
                "feedback_upserted": True, "feedback_extracted_candidates": len(extracted)}

    @staticmethod
    def manual_style_sample():
        verbose = ("# 合成写作流程验收\n\n## 背景与讨论\n"
            "就现有材料而言，样例方案仍然处于验证阶段，材料尚不足以支持商业成效结论。"
            "对于后续推进而言，需要核实数据来源并补齐风险说明。"
            "综合前述情况来看，当前建议是继续小规模验证。\n")
        concise = ("# 合成写作流程验收\n\n## 核心判断\n继续小规模验证。\n\n"
            "## 证据与边界\n样例方案仍处验证阶段。材料不足以支持商业成效结论。\n\n"
            "## 下一步\n核实数据来源。补齐风险说明。\n")
        return verbose, concise

    def real_auto_learning(self):
        self.require_model()
        # Persist a real, deliberately synthetic before/after edit. Appending
        # instructions to an already polished draft is not evidence of a style
        # change and can legitimately yield no candidates.
        self.settings(enabled=False, model_id=self.model_id)
        verbose, concise = self.manual_style_sample()
        before = self.save(verbose)
        self.settings(enabled=True, model_id=self.model_id)
        saved = self.save(concise)
        event = self.event_for("save", saved["version"])
        assert event["base_content"] == before["content"] and event["content"] == concise
        assert event.get("instruction", "") == "", "manual style edit invented a prompt"
        deadline = time.monotonic() + 210
        while time.monotonic() < deadline:
            event = self.api.data("GET", self.route + f"/writing-events/{event['id']}")
            if event.get("learning_status") in ("completed", "failed"):
                break
            time.sleep(1)
        assert event.get("learning_status") == "completed", "real automatic extraction did not complete successfully"
        self.report["model_inference_tested"] = True
        rules = self.api.data("GET", self.root + "/writing-rules")
        extracted = [rule for rule in rules if rule.get("source_event_id") == event["id"]]
        assert all(rule["status"] == "candidate" for rule in extracted), "automatic extraction published active preferences"
        assert all(rule.get("metadata", {}).get("model_id") == self.model_id for rule in extracted), "automatic extraction used another model"
        again = self.api.data("POST", self.route + f"/writing-events/{event['id']}/extract", {"model_id": self.model_id})
        assert {rule["id"] for rule in again} == {rule["id"] for rule in extracted}
        self.settings(enabled=False, model_id=self.model_id)
        disabled = self.save(saved["content"] + "\n合成关闭学习后继续保存。\n")
        assert self.event_for("save", disabled["version"]).get("learning_status") not in ("queued", "running", "completed", "failed")
        self.fixtures.update({"auto_learning_event_id": event["id"],
            "auto_candidate_ids": [rule["id"] for rule in extracted], "auto_sample_version": saved["version"]})
        self.save_fixtures()
        return {"automatic_learning_completed": True, "event_id": event["id"],
                "model_id": self.model_id, "auto_candidates": len(extracted),
                "actual_manual_style_change_journaled": True, "manual_retry_idempotent": True}

    def real_auto_reuse_and_revoke(self):
        self.require_model()
        event_id = self.fixtures.get("auto_learning_event_id")
        candidate_ids = self.fixtures.get("auto_candidate_ids", [])
        if not event_id or not candidate_ids:
            raise Blocked("Automatic extraction produced no reusable candidates; confirmation, reuse and revocation remain unverified")
        rules = self.api.data("GET", self.root + "/writing-rules")
        candidate = next(rule for rule in rules if rule["id"] == candidate_ids[0])
        assert candidate["status"] == "candidate" and candidate["source_event_id"] == event_id
        assert candidate.get("metadata", {}).get("model_id") == self.model_id
        draft = self.current()
        self.api.data("POST", self.route + "/proposals", {"base_version": draft["version"],
            "instruction": "未确认自动候选不能用于写作", "model_id": self.model_id,
            "writing_rule_ids": [candidate["id"]]}, expected=(400,))
        active = self.api.data("POST", self.root + f"/writing-rules/{candidate['id']}/decide", {
            "base_version": candidate["version"], "status": "active"})
        assert active["status"] == "active" and active["version"] == candidate["version"] + 1
        assert active["metadata"].get("approved_by") and active["metadata"].get("approved_at")
        self.fixtures["active_auto_rule"] = active
        self.save_fixtures()
        proposal = self.api.data("POST", self.route + "/proposals", {
            "base_version": draft["version"], "model_id": self.model_id,
            "instruction": "仅依据所选合成资料改写全文，采用本次选用的写作经验；保留事实边界及有效来源引用，不添加事实。",
            "writing_rule_ids": [active["id"]]})
        self.report["model_inference_tested"] = True
        snapshot = [{key: active[key] for key in ("id", "version", "name", "instruction")}]
        assert proposal["writing_rules"] == snapshot and proposal["model_id"] == self.model_id
        assert "knowledge://" in proposal["content"], "automatic-rule reuse has no source citations"
        accepted = self.api.data("POST", self.route + f"/proposals/{proposal['id']}/accept", {})
        assert accepted["content"] == proposal["content"] and accepted["version"] == draft["version"] + 1
        revoked = self.api.data("POST", self.root + f"/writing-rules/{active['id']}/decide", {
            "base_version": active["version"], "status": "revoked"})
        assert revoked["status"] == "revoked" and revoked["version"] == active["version"] + 1
        assert revoked["metadata"].get("revoked_by") and revoked["metadata"].get("revoked_at")
        self.api.data("POST", self.route + "/proposals", {"base_version": accepted["version"],
            "model_id": self.model_id, "instruction": "撤销后的自动经验不得复用",
            "writing_rule_ids": [active["id"]]}, expected=(400,))
        historical = self.api.data("GET", self.route + "/proposals")
        used = next(item for item in historical if item["id"] == proposal["id"])
        assert used["writing_rules"] == snapshot and used["status"] == "accepted"
        assert self.current()["version"] == accepted["version"], "revoked auto rule changed the document"
        source = self.api.data("GET", self.route + f"/writing-events/{event_id}")
        assert source["learning_status"] == "completed" and source["result_version"] == self.fixtures["auto_sample_version"]
        self.fixtures.update({"accepted_auto_proposal_id": proposal["id"], "auto_writing_snapshot": snapshot,
            "revoked_auto_rule_id": revoked["id"]})
        self.save_fixtures()
        return {"auto_rule_id": active["id"], "source_event_id": event_id, "proposal_id": proposal["id"],
                "candidate_rejected_before_confirmation": True, "confirmed_rule_actually_reused": True,
                "future_use_rejected_after_revocation": True, "historical_snapshot_and_source_retained": True}

    def real_revoke_snapshot(self):
        self.require_model()
        rule = self.fixtures.get("active_real_rule")
        if not rule or not self.fixtures.get("accepted_real_proposal_id"):
            raise Blocked("Real confirmed rule and generated proposal are required")
        current = self.current()
        pending = self.api.data("POST", self.route + "/proposals", {"base_version": current["version"],
            "model_id": self.model_id, "instruction": "合成验收：保持事实引用，精简空泛表达。", "writing_rule_ids": [rule["id"]]})
        revoked = self.api.data("POST", self.root + f"/writing-rules/{rule['id']}/decide", {
            "base_version": rule["version"], "status": "revoked"})
        assert revoked["status"] == "revoked"
        self.api.data("POST", self.route + f"/proposals/{pending['id']}/accept", {}, expected=(409,))
        self.api.data("POST", self.route + "/proposals", {"base_version": current["version"],
            "model_id": self.model_id, "instruction": "撤销经验不得复用", "writing_rule_ids": [rule["id"]]}, expected=(400,))
        historical = self.api.data("GET", self.route + "/proposals")
        accepted = next(item for item in historical if item["id"] == self.fixtures["accepted_real_proposal_id"])
        assert accepted["writing_rules"] == self.fixtures["real_writing_snapshot"] and accepted["status"] == "accepted"
        assert self.current()["version"] == current["version"], "revoked pending proposal changed the document"
        return {"pending_accept_rejected_after_revocation": True, "new_use_rejected": True,
                "historical_used_version_retained": True}

    def finish(self):
        if self.fixtures.get("draft_id") and not self.preflight_read_only:
            # End with learning disabled even when a model/API assertion fails.
            try:
                self.settings(enabled=False, model_id=self.model_id)
            except Exception:
                self.report["checks"].append({"name": "disable_fixture_auto_learning_after_suite", "status": "failed"})
        statuses = {check["status"] for check in self.report["checks"]}
        result = "failed" if "failed" in statuses else "blocked" if "blocked" in statuses else "passed"
        self.report.update({"status": result, "finished_at": self.now(), "fixture_ids": {
            key: self.fixtures[key] for key in ("research_kb_id", "source_kb_id", "draft_id") if key in self.fixtures}})
        path = self.args.output / (self.args.module + "-" + self.args.actor + "-report.json")
        path.write_text(json.dumps(self.report, ensure_ascii=False, indent=2))
        print(json.dumps({"status": result, "report": str(path)}, ensure_ascii=False))
        return 1 if result == "failed" else 2 if result == "blocked" else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, default=ROOT / ".research-state.json")
    parser.add_argument("--output", type=Path, default=ROOT / "artifacts/research/writing-memory-live")
    parser.add_argument("--fixtures", type=Path, help="Resume only previously created synthetic fixture IDs")
    parser.add_argument("--new-fixtures", action="store_true", help="Create new QA KBs; existing QA fixtures remain intact")
    parser.add_argument("--module", choices=("inventory", "workflow_no_model", "model_real", "all"), default="all")
    parser.add_argument("--model-id", default="", help="Existing active QA-owned KnowledgeQA model; never creates a model")
    parser.add_argument("--actor", choices=("owner", "ui"), default="owner", help="Existing QA account that owns the configured model; credentials are never copied")
    args = parser.parse_args()
    try:
        suite = Acceptance(args)
        suite.check("existing_synthetic_actor_auth_and_safe_model_inventory", lambda: {"actors": [args.actor, "colleague"], "models": suite.report["models"]})
        if args.module != "inventory" and suite.check("isolated_synthetic_sources_and_research_draft", suite.fixtures_ready):
            if args.module in ("workflow_no_model", "all") or (args.module == "model_real" and not suite.fixtures.get("style_save_event_id")):
                for name, callback in (("select_knowledge_base_sources", suite.source_selection),
                    ("normal_manual_save_journal_and_document_cas", suite.save_journal),
                    ("accepted_sample_manual_candidate_confirmation_and_rule_cas", suite.manual_rules),
                    ("revoke_stops_future_use_and_preserves_history", suite.revoke_manual),
                    ("version_revert_keeps_original_and_journals_real_diff", suite.version_revert_journal),
                    ("failed_prompt_capture_and_auto_learning_toggle_without_model", suite.no_model_journal),
                    ("private_and_shared_viewer_permissions", suite.permissions)):
                    suite.check(name, callback)
            if args.module in ("model_real", "all"):
                for name, callback in (("real_model_event_extraction_confirmation_and_idempotency", suite.real_event_extraction),
                    ("real_model_source_based_reuse_and_feedback_round_trip", suite.real_reuse_feedback),
                    ("real_model_auto_learning_and_off_switch", suite.real_auto_learning),
                    ("real_model_auto_candidate_confirmation_reuse_and_revocation", suite.real_auto_reuse_and_revoke),
                    ("real_model_revocation_preserves_actual_use_snapshot", suite.real_revoke_snapshot)):
                    suite.check(name, callback)
        return suite.finish()
    except Exception as error:
        # Never print arbitrary exception text, HTTP bodies or local account state.
        print(json.dumps({"status": "failed", "error_type": type(error).__name__}), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
