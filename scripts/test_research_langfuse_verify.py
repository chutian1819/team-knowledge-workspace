"""Offline parser/assertion tests only; never reported as live Langfuse proof."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("verify", Path(__file__).with_name("research-langfuse-verify.py"))
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


def fixture():
    query = "PRIVATE_QUERY_SENTINEL"
    qhash = hashlib.sha256(query.encode()).hexdigest()
    snap = "a" * 64
    common = {"kb_id": "kb", "draft_id": "draft", "outcome": "completed"}

    def row(identity, name, trace, parent=None, extra=None, input=None, output=None, model=None, minute=1):
        return {"id": identity, "name": name, "traceId": trace, "parentObservationId": parent, "level": "DEFAULT",
                "startTime": f"2026-10-22T00:{minute:02d}:00Z", "endTime": f"2026-10-22T00:{minute:02d}:01Z",
                "type": "GENERATION" if model else "SPAN", "model": model,
                "metadata": common | (extra or {}), "input": json.dumps(input), "output": json.dumps(output),
                "usageDetails": {"input": 7, "output": 3, "total": 10} if model else {}}

    rows = [
        row("create", "research.draft.create", "trace-create", extra={"retrieval_snapshot_sha256": snap}),
        row("selected", "research.retrieval", "trace-create", "create", {"retrieval_snapshot_sha256": snap, "retrieval_query_sha256": qhash, "retrieval_hit_count": 1,
            "retrieval_hits": [{"chunk_id": "chunk", "knowledge_id": "knowledge", "content_sha256": "b" * 64, "rank": 1, "score": 0.9}]}),
        row("embedding", "embedding.embed", "trace-create", "selected", {"model_id": "embedding-model"}, query, {"dimensions": 4}, "test-embedding"),
        row("retrieve", "retrieve", "trace-create", "selected", {"embedding_model_id": "embedding-model", "has_query_embedding": True},
            {"query_text": query}, {"vector_hits": 1, "total_hits": 1, "top_hits": [{"chunk_id": "chunk", "knowledge_id": "knowledge", "preview": "PRIVATE_SOURCE_SENTINEL"}]}),
        row("save", "research.draft.save", "trace-save", extra={"event_id": "event", "base_version": 1, "result_version": 2}),
        row("enqueue", "research.learning.enqueue", "trace-save", "save", {"event_id": "event", "automatic": True}),
        row("async", "asynq.research:writing-learning", "trace-save", "enqueue"),
        row("worker", "research.learning.worker", "trace-save", "async", {"event_id": "event", "automatic": True}),
        row("extract", "research.rules.extract_event", "trace-save", "worker", {"event_id": "event", "automatic": True, "model_id": "model", "rule_count": 1, "rule_versions": json.dumps({"rule": 1})}),
        row("infer", "research.rules.infer", "trace-save", "extract", {"event_id": "event", "model_id": "model"}),
        row("learning-model", "chat.completion", "trace-save", "infer", {"event_id": "event", "model_id": "model"}, "PRIVATE_PROMPT_SENTINEL", "PRIVATE_RULE_SENTINEL", "test-chat"),
        row("confirm", "research.rules.decide", "trace-confirm", extra={"event_id": "event", "rule_id": "rule", "rule_status": "active", "base_version": 1, "rule_version": 2}, minute=2),
        row("propose", "research.draft.propose", "trace-propose", extra={"proposal_id": "proposal", "prompt_event_id": "prompt", "event_id": "proposal-event", "model_id": "model", "rule_versions": {"rule": 2}, "retrieval_snapshot_sha256": snap}, minute=3),
        row("generation", "chat.completion", "trace-propose", "propose", {"event_id": "prompt", "model_id": "model", "rule_versions": {"rule": 2}, "retrieval_snapshot_sha256": snap}, "PRIVATE_PROMPT_SENTINEL", "PRIVATE_DRAFT_SENTINEL", "test-chat", minute=3),
        row("accept", "research.draft.accept", "trace-accept", extra={"proposal_id": "proposal", "event_id": "accept-event", "base_version": 2, "result_version": 3, "rule_versions": {"rule": 2}}, minute=4),
    ]
    return rows


class ReadbackTests(unittest.TestCase):
    def statuses(self, report):
        return {item["name"]: item["status"] for item in report["checks"]}

    def test_complete_chain_and_safe_output(self):
        rows = fixture()
        rows[0]["metadata"]["unknown_private"] = "PRIVATE_METADATA_SENTINEL"
        rows[0]["statusMessage"] = "PRIVATE_ERROR_SENTINEL"
        result = verify.evaluate(rows, ("draft",))
        self.assertTrue(all(check["status"] == "pass" for check in result["checks"]))
        encoded = json.dumps(result)
        self.assertNotIn("PRIVATE_", encoded)
        self.assertEqual(8, len([check for check in result["checks"] if check["name"] != "draft_scope"]))
        self.assertTrue(any(link["kind"] == "save_to_background_learning" for link in result["links"]))

    def test_same_names_without_parent_chain_do_not_pass(self):
        rows = fixture()
        next(row for row in rows if row["id"] == "worker")["parentObservationId"] = None
        result = self.statuses(verify.evaluate(rows, ("draft",)))
        for name in ("learning", "candidates", "confirmation", "reuse"):
            self.assertEqual("fail", result[name])

    def test_different_query_or_snapshot_is_rejected(self):
        rows = fixture()
        next(row for row in rows if row["id"] == "embedding")["input"] = json.dumps("different query")
        self.assertEqual("fail", self.statuses(verify.evaluate(rows))["query_embedding"])
        rows = fixture()
        next(row for row in rows if row["id"] == "generation")["metadata"]["retrieval_snapshot_sha256"] = "c" * 64
        checks = self.statuses(verify.evaluate(rows))
        self.assertEqual("fail", checks["generation"])
        self.assertEqual("fail", checks["reuse"])

    def test_missing_snapshot_span_is_not_assumed(self):
        rows = [row for row in fixture() if row["id"] != "selected"]
        checks = self.statuses(verify.evaluate(rows))
        for name in ("retrieval", "query_embedding", "generation", "reuse"):
            self.assertEqual("fail", checks[name])

    def test_candidate_is_not_confirmation_and_later_confirmation_is_not_reuse(self):
        rows = fixture()
        decision = next(row for row in rows if row["id"] == "confirm")
        decision["metadata"]["rule_status"] = "candidate"
        checks = self.statuses(verify.evaluate(rows))
        self.assertEqual("fail", checks["confirmation"])
        self.assertEqual("fail", checks["reuse"])
        rows = fixture()
        decision = next(row for row in rows if row["id"] == "confirm")
        decision["endTime"] = "2026-10-22T00:05:00Z"
        self.assertEqual("fail", self.statuses(verify.evaluate(rows))["reuse"])

    def test_unknown_draft_and_empty_data_are_missing(self):
        self.assertEqual(0, verify.evaluate(fixture(), ("other-draft",))["selected_observation_count"])
        self.assertTrue(all(item["status"] == "fail" for item in verify.evaluate([])["checks"]))

    def test_cursor_pagination_complete_and_loop_rejected(self):
        class Reader(verify.LangfuseReader):
            def __init__(self, pages):
                self.pages = iter(pages)
            def page(self, *args, **kwargs):
                return next(self.pages)
        rows = fixture()
        reader = Reader([{"data": rows[:2], "meta": {"cursor": "opaque"}}, {"data": rows[2:], "meta": {}}])
        actual, info = reader.read_all(None, None, 3)
        self.assertEqual(len(rows), len(actual))
        self.assertTrue(info["complete"])
        self.assertEqual(2, info["pages"])
        reader = Reader([{"data": [], "meta": {"cursor": "same"}}, {"data": [], "meta": {"cursor": "same"}}])
        with self.assertRaises(verify.ReadError):
            reader.read_all(None, None, 3)

    def test_credentials_cannot_be_sent_to_remote_url(self):
        with self.assertRaises(ValueError):
            verify.LangfuseReader("https://external.invalid", "/file-that-must-not-be-read")


if __name__ == "__main__":
    unittest.main()
