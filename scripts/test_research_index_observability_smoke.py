"""Offline assertion tests; these never log in, call providers, or create data."""
import copy
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest

path = Path(__file__).with_name("research-index-observability-smoke.py")
spec = importlib.util.spec_from_file_location("index_smoke", path)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class IndexSmokeAssertions(unittest.TestCase):
    def setUp(self):
        self.s = smoke.Smoke.__new__(smoke.Smoke)
        self.s.args = SimpleNamespace(model_id="model")
        self.s.lf = smoke.load("lf_index_unit", "research-langfuse-verify.py")
        self.s.f = {"documents": [{"ingest_trace_id": "ingest1"}, {"ingest_trace_id": "ingest2"}],
                    "rebuild_trace_id": "rebuild", "task_id": "task", "model_name": "local-embedding",
                    "rebuild": {"chunks_indexed": 2}}
        self.rows = []
        for trace in ("ingest1", "ingest2", "rebuild"):
            root = {"id": trace + "root", "traceId": trace, "name": "POST /api/v1/knowledge-bases/:id/" + ("rebuild-index" if trace == "rebuild" else "knowledge/manual"), "endTime": "2026-10-22T00:01:00Z", "level": "DEFAULT"}
            worker = {**root, "id": trace + "worker", "parentObservationId": root["id"],
                      "name": "asynq.knowledge:index_rebuild" if trace == "rebuild" else "asynq.manual:process", "metadata": {"task_id": "task"}}
            count = 2 if trace == "rebuild" else 1
            generation = {**root, "id": trace + "generation", "parentObservationId": worker["id"],
                          "name": "embedding.batch_embed", "model": "local-embedding", "metadata": {"model_id": "model"},
                          "input": {"count": count}, "output": {"count": count}, "usageDetails": {"input": count}}
            self.rows.extend((root, worker, generation))

    def test_complete_real_shape_passes(self):
        links = self.s.inspect(self.rows)
        self.assertEqual(3, len(links))
        self.assertEqual(2, links[-1]["embedded_inputs"])

    def test_missing_batch_is_not_a_pass(self):
        with self.assertRaisesRegex(AssertionError, "rebuild_batch_embedding_missing"):
            self.s.inspect(self.rows[:-1])

    def test_same_trace_without_worker_parent_fails(self):
        rows = copy.deepcopy(self.rows)
        rows[-1]["parentObservationId"] = rows[-3]["id"]
        with self.assertRaisesRegex(AssertionError, "rebuild_embedding_parent_mismatch"):
            self.s.inspect(rows)

    def test_partial_batch_coverage_fails(self):
        rows = copy.deepcopy(self.rows)
        rows[-1]["input"]["count"] = rows[-1]["output"]["count"] = 1
        with self.assertRaisesRegex(AssertionError, "rebuild_embedding_coverage_mismatch"):
            self.s.inspect(rows)


if __name__ == "__main__": unittest.main()
