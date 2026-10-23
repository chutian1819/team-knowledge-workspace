#!/usr/bin/env python3
"""Offline mechanics only: no platform requests and no simulated model results."""
import importlib.util
import io
import json
from email.parser import BytesParser
from email import policy
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch, Mock

SPEC = importlib.util.spec_from_file_location("capacity", Path(__file__).with_name("research-capacity-test.py"))
capacity = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(capacity)


class CapacityMechanics(unittest.TestCase):
    def test_upload_metadata_matches_string_map_api_contract(self):
        body, content_type = capacity.multipart("sample.txt", b"source text")
        message = BytesParser(policy=policy.default).parsebytes(
            ("Content-Type: " + content_type + "\r\nMIME-Version: 1.0\r\n\r\n").encode() + body)
        fields = {part.get_param("name", header="content-disposition"): part.get_payload(decode=True)
                  for part in message.iter_parts()}
        metadata = json.loads(fields["metadata"])
        self.assertTrue(all(isinstance(value, str) for value in metadata.values()))
        self.assertEqual(fields["file"], b"source text")

    def test_plan_is_offline_and_counts_default_budget(self):
        args = capacity.arguments(["plan"])
        with patch.object(capacity, "login", side_effect=AssertionError("must not authenticate")):
            with patch("sys.stdout", new_callable=io.StringIO) as output:
                self.assertEqual(capacity.main(["plan"]), 0)
            plan = json.loads(output.getvalue())
        self.assertEqual(sum(s["by_kind"]["writing"] for s in plan["stages"]), 30)
        self.assertEqual(sum(s["by_kind"]["search"] for s in plan["stages"]), 60)
        self.assertEqual(sum(s["by_kind"]["upload"] for s in plan["stages"]), 15)
        self.assertEqual(capacity.costs("upload", args)["embedding"], 48)

    def test_search_default_has_40_per_level(self):
        args = capacity.arguments(["plan", "--kind", "search"])
        self.assertEqual([s["operations"] for s in capacity.plan(args)["stages"]], [40] * 4)
        self.assertEqual(len({capacity.query_variant(i, "varied") for i in range(80)}), 80)
        self.assertEqual(capacity.query_variant(0, "varied"), capacity.query_variant(80, "varied"))

    def test_percentiles_and_real_active_intervals(self):
        self.assertEqual(capacity.percentile(list(range(1, 41)), .95), 38)
        self.assertIsNone(capacity.percentile([], .95))
        rows = [{"client_started_epoch": 0, "client_finished_epoch": 2}, {"client_started_epoch": 1, "client_finished_epoch": 3}]
        measured = capacity.concurrency_metrics(rows)
        self.assertEqual(measured["max"], 2)
        self.assertAlmostEqual(measured["time_weighted_mean"], 4 / 3)

    def test_failed_samples_are_retained(self):
        rows = [{"status": "success", "client_end_to_end_seconds": 1},
                {"status": "hard_deadline", "client_end_to_end_seconds": 9}]
        metrics = capacity.workload_metrics(rows, [], 10)
        self.assertEqual(metrics["success_rate"], .5)
        self.assertEqual(metrics["end_to_end_all_including_censored_failures"]["p95_seconds"], 9)
        self.assertEqual(metrics["end_to_end_successes"]["p95_seconds"], 1)

    def test_inflight_is_not_failed_and_unknown_stops(self):
        suite = capacity.Suite.__new__(capacity.Suite)
        suite.args = capacity.arguments([])
        self.assertIsNone(suite.stop_reason([{"status": "success"}] + [{"status": "dispatched"}] * 7))
        self.assertIsNone(suite.stop_reason([{"status": "success"}] * 8 + [{"status": "running"}] * 8))
        self.assertEqual(suite.stop_reason([{"status": "hard_deadline", "backend_outcome_unknown": True}]), "backend_outcome_unknown")
        self.assertEqual(suite.stop_reason([{"status": "http_error", "http_status": 429}]), "http_429_threshold")

    def test_persistent_budget_no_refund_on_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            args = capacity.arguments(["plan", "--output", temporary, "--max-writing-calls", "2"])
            suite = capacity.Suite(args, authenticate=False)
            suite.reserve({"writing": 2, "embedding": 0})
            resumed = capacity.Suite(args, authenticate=False)
            with self.assertRaisesRegex(ValueError, "model_budget_exhausted"):
                resumed.reserve({"writing": 1, "embedding": 0})
            self.assertEqual(resumed.f["budget_reserved"]["writing"], 2)
            self.assertEqual(suite.path.stat().st_mode & 0o777, 0o600)

    def test_hard_deadline_kills_worker_keeps_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            capacity.save(Path(temporary) / "checkpoint.json", {"knowledge_id": "existing-test-id"})
            process = Mock(returncode=-9)
            process.communicate.side_effect = [subprocess.TimeoutExpired("worker", 1), ("", "")]
            job = {"directory": temporary, "timeout": .1, "operation_id": "x", "kind": "upload", "trace_id": "1" * 32}
            with patch.object(capacity.subprocess, "Popen", return_value=process):
                result = capacity.run_child(job)
            process.kill.assert_called_once()
            self.assertEqual(result["status"], "hard_deadline")
            self.assertEqual(result["knowledge_id"], "existing-test-id")
            self.assertTrue(result["backend_outcome_unknown"])

    def test_bounds_and_explicit_steady_budget(self):
        for arguments in (["--max-concurrency", "33"], ["--max-writing-calls", "129"],
                          ["--run-label", "../../escape"], ["--duration-seconds", "600"]):
            with self.subTest(arguments=arguments), self.assertRaises(ValueError): capacity.arguments(arguments)
        args = capacity.arguments(["stage", "--kind", "writing", "--level", "8", "--operations", "8", "--max-writing-calls", "128"])
        self.assertEqual(capacity.plan(args)["stages"][0]["by_kind"]["writing"], 8)

    def test_trace_failure_does_not_expect_successful_generation(self):
        failed = capacity.trace_expectation({"kind": "writing", "status": "http_error", "operation_id": "x", "trace_id": "a" * 32})
        self.assertEqual(len(failed["expected_minimum_observation_names"]), 1)
        success = capacity.trace_expectation({"kind": "upload", "status": "success", "operation_id": "x", "trace_id": "b" * 32, "indexed_chunks": 3})
        self.assertEqual(success["expected_embedding_inputs"], 3)
        self.assertIn("embedding.batch_embed", success["expected_minimum_observation_names"])


if __name__ == "__main__": unittest.main()
