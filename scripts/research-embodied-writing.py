#!/usr/bin/env python3
"""Verify a clearly labelled manual writing workflow over real embodied-AI sources.

The text is supplied explicitly through native manual-content proposals. It is
not model output. The script never configures a model, enables automatic learning,
or activates a writing rule. Separate fixtures avoid concurrent ingestion writes.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import time
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "artifacts/research/product-audit"
KB_NAME = "验收 · 具身智能调研 · 研究稿"
TITLE = "具身智能调研 · 流程验收稿（模型待接入）"
NOTICE = "> 人工流程验收稿：正文由验收脚本明确提供，用于验证引用、提案、修订历史与导出；不是 Kimi 生成，不是经过模型验证的研究结论。"
INSTRUCTIONS = [
    "人工验收修订一：先写审慎结论，再分别列出两份来源所支持的内容；保留来源引用，区分资料陈述与待验证判断。",
    "人工验收修订二：把下一步改为可执行核验表，每项列出所需证据、负责人和状态；删除无证据的商业化结论，并保留模型尚未接入的说明。",
]


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def save_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(".tmp")
    fd = os.open(temp, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as handle:
        json.dump(value, handle, ensure_ascii=False, indent=2)
    temp.replace(path)


class Acceptance:
    def __init__(self, args):
        spec = importlib.util.spec_from_file_location("writing_acceptance", ROOT / "scripts/research-writing-acceptance.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        state = json.loads(args.state.read_text())
        location = urllib.parse.urlsplit(state["base_url"])
        assert location.hostname in ("localhost", "127.0.0.1", "::1") and location.port == 8090
        self.api = module.API(state, "ui")
        self.args = args
        self.sources = json.loads(args.sources.read_text())
        self.fixture_path = args.output / "embodied-writing-fixtures.json"
        self.report_path = args.output / "embodied-writing-report.json"
        self.fixtures = json.loads(self.fixture_path.read_text()) if self.fixture_path.exists() else {
            "actor": "ui", "created_at": now(), "source_kb_id": self.sources["source_kb_id"], "manual_rounds": []}
        assert self.fixtures["actor"] == "ui"
        assert self.fixtures["source_kb_id"] == self.sources["source_kb_id"]
        self.report = {"suite": "embodied-manual-writing-workflow", "started_at": now(), "checks": [],
            "content_origin": "explicit manual acceptance text", "model_generation_succeeded": False,
            "automatic_learning_verified": False, "user_preferences_activated": False,
            "real_public_sources": True, "business_documents_modified": False}

    def persist(self):
        save_json(self.fixture_path, self.fixtures)
        save_json(self.report_path, self.report)

    def passed(self, name, **details):
        self.report["checks"].append({"name": name, "status": "passed", **details})
        self.persist()
        print(json.dumps({"name": name, "status": "passed"}), flush=True)

    @property
    def root(self):
        return f"/knowledgebase/{self.fixtures['research_kb_id']}/research"

    @property
    def route(self):
        return self.root + "/drafts/" + self.fixtures["draft_id"]

    def current(self):
        return self.api.data("GET", self.route)

    def events(self):
        return self.api.data("GET", self.route + "/writing-events")

    def contents(self):
        pi0 = self.sources["sources"]["pi0_pdf"]["knowledge_id"]
        buddy = self.sources["sources"]["digitalbuddy_html"]["knowledge_id"]
        citation = f"[π0 论文](knowledge://{pi0})"
        official = f"[DigitalBuddy 官方文章存档](knowledge://{buddy})"
        initial = f"""# {TITLE}

{NOTICE}

## 研究问题
具身智能的通用任务能力与仿真数据扩增分别解决什么问题，需要怎样的证据才能判断真实部署价值？

## 已保存的证据
- π0 论文展示了移动操作机器人完成衣物处理任务的案例。{citation}
- 上海人工智能实验室介绍了 DigitalBuddy 与 Real-to-Sim-to-Real 技术路径。{official}

## 下一步
核对来源实验设置，并补充真实部署环境、失败案例和外部复现材料。

## 验收边界
已入库的是公开 PDF 与官方网页保存件；公众号直连受本机网络解析限制。正文和原件可读取不代表模型理解或检索质量通过。
"""
        first = f"""# {TITLE}

{NOTICE}

## 核心判断
当前材料适合形成技术路线的初步研究问题；仅凭这两份材料，不能判断商业化成熟度或跨场景部署效果。这是人工审阅边界，不是模型结论。

## 来源支持的内容
- **机器人通用任务能力**：π0 论文提供移动操作机器人处理衣物的案例。该案例用于定位后续要核对的任务、机器人本体与实验条件。{citation}
- **仿真数据与真实部署衔接**：DigitalBuddy 官方介绍围绕 Real-to-Sim-to-Real 展开，明确包含场景数字化、仿真训练以及真实环境部署。{official}

## 证据与不确定性
两份来源的任务、评测口径和数据条件不同，本稿不直接合并或比较它们的成功率。官方介绍中的性能与成本陈述仍需对应实验设置、样本量及独立复现证据。

## 下一步
补充任务定义、训练与测试划分、跨本体泛化、失败案例、真实部署持续运行记录。

## 验收边界
本轮为人工提案与接受流程验收，保留两份真实来源关联；未调用 Kimi 生成。公众号采用明确标注的官方网页保存件作为阅读路径，独立图片理解尚未验证。
"""
        second = first.replace("## 下一步\n补充任务定义、训练与测试划分、跨本体泛化、失败案例、真实部署持续运行记录。", """## 下一步核验表
| 待验证问题 | 所需证据 | 负责人 | 状态 |
| --- | --- | --- | --- |
| 实验结论适用于哪些任务与机器人本体？ | 原文实验设置、任务列表及测试集划分 | 待分配 | 待核实 |
| 仿真数据带来的收益能否复现？ | 数据构成、对照实验、消融实验与复现记录 | 待分配 | 待核实 |
| 能否支持真实场景的稳定部署？ | 连续运行时间、失败与人工接管记录 | 待分配 | 待补充 |
| 是否具备商业化判断所需的证据？ | 客户使用范围、成本口径与外部验证 | 待分配 | 证据不足 |""")
        return initial, first, second

    def create(self):
        kb = self.api.data("GET", "/knowledge-bases/" + self.sources["source_kb_id"])
        assert kb["name"] == "验收 · 具身智能调研 · 资料"
        ids = [self.sources["sources"][key]["knowledge_id"] for key in ("pi0_pdf", "digitalbuddy_html")]
        for knowledge_id in ids:
            document = self.api.data("GET", "/knowledge/" + knowledge_id)
            assert document["parse_status"] == "completed"
        if not self.fixtures.get("research_kb_id"):
            created = self.api.data("POST", "/knowledge-bases", {"name": KB_NAME,
                "description": "人工流程验收专用研究稿；不代表模型生成或用户真实写作偏好。",
                "type": "document", "embedding_model_id": "", "summary_model_id": "",
                "indexing_strategy": {"vector_enabled": False, "keyword_enabled": False,
                                      "wiki_enabled": True, "graph_enabled": False},
                "extract_config": {"enabled": False}, "wiki_config": {"research_only": True}})
            self.fixtures["research_kb_id"] = created["id"]
            self.persist()
        research = self.api.data("GET", "/knowledge-bases/" + self.fixtures["research_kb_id"])
        assert research["name"] == KB_NAME and research["wiki_config"]["research_only"]
        if not self.fixtures.get("draft_id"):
            draft = self.api.data("POST", self.root + "/drafts", {"title": TITLE, "template_id": "brief",
                "content": self.contents()[0], "source_ids": ids, "source_scope": "selected",
                "source_kb_id": self.sources["source_kb_id"]})
            self.fixtures.update({"draft_id": draft["id"], "initial_version": draft["version"], "source_ids": ids})
            self.persist()
        draft = self.current()
        assert draft["title"] == TITLE and "人工流程验收稿" in draft["content"]
        assert {ref.split("|", 1)[0] for ref in draft["source_refs"]} == set(ids)
        assert self.api.data("GET", self.route + "/writing-learning")["enabled"] is False
        self.passed("manual_draft_has_real_sources_and_learning_is_off", source_count=len(ids))

    def revise(self):
        if not self.fixtures.get("stale_proposal_id"):
            draft = self.current()
            stale = self.api.data("POST", self.route + "/proposals", {"base_version": draft["version"],
                "instruction": "人工并发保护验收：本过期提案不得覆盖新版本。", "content": "# 过期人工验收提案\n不得覆盖正式验收稿。"})
            self.fixtures["stale_proposal_id"] = stale["id"]
            self.persist()
        for index, instruction in enumerate(INSTRUCTIONS):
            if len(self.fixtures["manual_rounds"]) <= index:
                draft = self.current()
                proposal = self.api.data("POST", self.route + "/proposals", {"base_version": draft["version"],
                    "instruction": instruction, "content": self.contents()[index + 1]})
                self.fixtures["manual_rounds"].append({"proposal_id": proposal["id"],
                    "base_version": draft["version"], "instruction": instruction})
                self.persist()
            record = self.fixtures["manual_rounds"][index]
            proposal = next(p for p in self.api.data("GET", self.route + "/proposals") if p["id"] == record["proposal_id"])
            if proposal["status"] == "pending":
                accepted = self.api.data("POST", self.route + f"/proposals/{proposal['id']}/accept", {})
                record["accepted_version"] = accepted["version"]
                self.persist()
            else:
                assert proposal["status"] == "accepted"
            assert not proposal.get("model_id"), "manual proposal incorrectly claims model provenance"
            assert proposal["instruction"] == instruction
            matching = [event for event in self.events() if event.get("proposal_id") == proposal["id"]]
            assert {event["kind"] for event in matching} >= {"proposal", "accept"}
            assert all(event["instruction"] == instruction for event in matching if event["kind"] in ("proposal", "accept"))
            assert all(event["base_content"] == proposal["base_content"] and event["content"] == proposal["content"] for event in matching)
            self.passed("manual_revision_" + str(index + 1) + "_accepted_and_journaled", proposal_id=proposal["id"])
        assert self.current()["content"] == self.contents()[2]

    def conflict_and_history(self):
        before = self.current()
        events_before = {event["id"] for event in self.events()}
        stale_id = self.fixtures["stale_proposal_id"]
        self.api.raw("POST", self.route + f"/proposals/{stale_id}/accept", {}, "owner", expected=(409,))
        self.api.raw("PUT", self.route, {"base_version": self.fixtures["initial_version"],
            "title": TITLE, "content": "不得写入的过期版本"}, "owner", expected=(409,))
        after = self.current()
        assert before["content"] == after["content"] and before["version"] == after["version"]
        assert events_before == {event["id"] for event in self.events()}
        stale = next(p for p in self.api.data("GET", self.route + "/proposals") if p["id"] == stale_id)
        if stale["status"] == "pending":
            self.api.data("POST", self.route + f"/proposals/{stale_id}/reject", {})
        history = self.api.data("GET", self.route + "/revisions")
        revisions = history["revisions"]
        assert len(revisions) >= 2
        # Native revision listings deliberately omit full bodies. Full before/
        # after text is read through the authenticated writing-event journal.
        assert {revision["version"] for revision in revisions} >= {1, 2}
        contents = {event.get("content", "") for event in self.events() if event["kind"] in ("create", "accept")}
        assert self.contents()[0] in contents and self.contents()[1] in contents
        self.fixtures["final_version"] = after["version"]
        self.passed("stale_accept_and_save_cannot_overwrite_history", current_version=after["version"], revision_count=len(revisions))

    def candidate(self):
        if not self.fixtures.get("candidate_rule_id"):
            candidate = self.api.data("POST", self.root + "/writing-rules", {
                "name": "人工验收候选 · 结论与证据边界", "template_id": "brief",
                "instruction": "人工验收示例：先给审慎结论，再列来源支持的内容和待核验事项；不同实验口径不直接比较。此候选尚未由用户确认。",
                "source_proposal_id": self.fixtures["manual_rounds"][-1]["proposal_id"]})
            self.fixtures["candidate_rule_id"] = candidate["id"]
            self.persist()
        rules = self.api.data("GET", self.root + "/writing-rules")
        rule = next(item for item in rules if item["id"] == self.fixtures["candidate_rule_id"])
        assert rule["status"] == "candidate" and rule["version"] == 1
        assert not any(item["status"] == "active" for item in rules)
        self.passed("manual_candidate_saved_without_activating_user_preferences", rule_id=rule["id"])

    def generation_failure(self):
        before = self.current()
        if not self.fixtures.get("no_model_attempt"):
            models = self.api.data("GET", "/models")
            available = [model for model in models if model.get("type") == "KnowledgeQA" and model.get("status") == "active"]
            if available:
                raise RuntimeError("A model is now available; this manual-only script does not assert an expected no-model failure")
            instruction = "模型接入验收：请根据已关联资料生成具身智能研究简报，保留逐项引用并明确证据边界。"
            status, _ = self.api.raw("POST", self.route + "/proposals", {
                "base_version": before["version"], "instruction": instruction}, "owner", expected=(400,))
            self.fixtures["no_model_attempt"] = {"http_status": status, "instruction": instruction}
            self.persist()
        instruction = self.fixtures["no_model_attempt"]["instruction"]
        journal = self.events()
        assert any(event["kind"] == "prompt" and event.get("instruction") == instruction for event in journal)
        assert any(event["kind"] == "generation_failed" and event.get("instruction") == instruction for event in journal)
        after = self.current()
        assert before["version"] == after["version"] and before["content"] == after["content"]
        self.passed("missing_model_failure_preserves_prompt_and_document", http_status=400)
        self.report["checks"].append({"name": "kimi_generation_and_automatic_learning", "status": "blocked",
            "reason": "UI QA account has no configured KnowledgeQA model. Manual content/history checks do not verify Kimi generation or automatic learning."})
        self.persist()

    def export(self):
        if not self.fixtures.get("export_id"):
            exported = self.api.data("POST", self.route + "/exports", {"format": "md"})
            self.fixtures["export_id"] = exported["id"]
            self.persist()
        route = self.root + "/exports/" + self.fixtures["export_id"]
        deadline = time.monotonic() + 90
        while True:
            exported = self.api.data("GET", route)
            if exported["status"] == "ready":
                break
            assert exported["status"] != "failed", "Markdown export failed"
            assert time.monotonic() < deadline, "Markdown export timed out"
            time.sleep(2)
        auth = self.api.actors["owner"]
        tenant = auth["active_tenant"]
        if isinstance(tenant, dict):
            tenant = tenant.get("id") or tenant.get("tenant_id")
        request = urllib.request.Request(self.api.base + route + "/download", headers={
            "Authorization": "Bearer " + auth["token"], "X-Tenant-ID": str(tenant)})
        with urllib.request.urlopen(request, timeout=30) as response:
            content = response.read()
        text = content.decode("utf-8")
        assert self.current()["content"] in text
        assert all(knowledge_id in text for knowledge_id in self.fixtures["source_ids"])
        target = self.args.output / "embodied-workflow-draft.md"
        target.write_bytes(content)
        self.fixtures["markdown_export"] = str(target)
        self.passed("markdown_export_preserves_final_text_and_source_references", bytes=len(content), sha256=hashlib.sha256(content).hexdigest())

    def run(self):
        try:
            for step in (self.create, self.revise, self.conflict_and_history, self.candidate, self.generation_failure, self.export):
                step()
            self.report["status"] = "blocked"
        except Exception as error:
            self.report["status"] = "failed"
            self.report["error"] = str(error) if isinstance(error, (AssertionError, RuntimeError)) else type(error).__name__
            raise
        finally:
            self.report["finished_at"] = now()
            self.persist()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, default=ROOT / ".research-state.json")
    parser.add_argument("--sources", type=Path, default=OUTPUT / "embodied-fixtures.json")
    parser.add_argument("--output", type=Path, default=OUTPUT)
    suite = Acceptance(parser.parse_args())
    suite.run()
    print(json.dumps({"status": suite.report["status"], "research_kb_id": suite.fixtures["research_kb_id"],
                      "draft_id": suite.fixtures["draft_id"]}, ensure_ascii=False))
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
