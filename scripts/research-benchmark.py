#!/usr/bin/env python3
"""Measure unchanged native metadata search; never claim semantic RAG coverage.

Run --label baseline before enabling custom binaries and --label custom after.
Requires the synthetic 50-document fixtures created by research-live-acceptance.
The two measurements must run with idle builds/converters and the same volumes.
"""
import argparse
import datetime
import hashlib
import http.client
import json
import math
from pathlib import Path
import re
import statistics
import time
import urllib.parse

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--label", choices=("baseline", "custom"), required=True)
    parser.add_argument("--samples", type=int, default=300)
    parser.add_argument("--base-url", help="Use a restored local instance with the same test accounts")
    parser.add_argument("--series", default="", help="Separate matched-corpus runs from initial measurements")
    args = parser.parse_args()
    if args.series and not re.fullmatch(r"[a-z0-9-]+", args.series):
        parser.error("series must use lowercase letters, digits and hyphens")
    if args.samples < 100:
        parser.error("use at least 100 samples")
    state = json.loads((ROOT / ".research-state.json").read_text())
    actor, qa = state["actors"]["owner"], state["qa"]
    base = urllib.parse.urlsplit(args.base_url or state["base_url"])
    connection_type = http.client.HTTPSConnection if base.scheme == "https" else http.client.HTTPConnection
    headers = {"Authorization": "Bearer " + actor["token"],
               "X-Tenant-ID": str(actor["active_tenant"]["id"])}
    queries = {
        "knowledge_list_keyword": f'/api/v1/knowledge-bases/{qa["source_kb_id"]}/knowledge?page=1&page_size=20&keyword=' + urllib.parse.quote("合成验收资料"),
        "global_title_search": "/api/v1/knowledge/search?limit=20&keyword=" + urllib.parse.quote("合成验收资料"),
    }
    report = {"created_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "label": args.label, "samples_per_round": args.samples, "rounds": 3,
              "scope": "native metadata/title search on 50 synthetic unpublished documents; no model calls; does not certify semantic retrieval P95",
              "fixture_hash": hashlib.sha256(json.dumps(sorted(qa["fixture_ids"])).encode()).hexdigest(),
              "workloads": {}}
    for name, path in queries.items():
        rounds, latencies = [], []
        for _ in range(3):
            connection = connection_type(base.hostname, base.port, timeout=30)
            times = []
            for index in range(args.samples + 30):
                start = time.perf_counter()
                connection.request("GET", path, headers=headers)
                response = connection.getresponse()
                payload = response.read()
                elapsed = (time.perf_counter() - start) * 1000
                if response.status != 200:
                    raise RuntimeError(name + " returned HTTP " + str(response.status))
                data = json.loads(payload)
                if not data.get("data"):
                    raise RuntimeError(name + " returned no matching fixtures")
                if index >= 30:
                    times.append(elapsed)
            connection.close()
            rounds.append(sorted(times)[math.ceil(len(times) * .95) - 1])
            latencies.extend(times)
        report["workloads"][name] = {"p50_ms": statistics.median(latencies),
            "p95_ms": statistics.median(rounds), "round_p95_ms": rounds}
    output = ROOT / "artifacts/research"
    output.mkdir(parents=True, exist_ok=True)
    prefix = "benchmark-" + (args.series + "-" if args.series else "")
    target = output / (prefix + args.label + ".json")
    if args.label == "custom" and (output / (prefix + "baseline.json")).exists():
        baseline = json.loads((output / (prefix + "baseline.json")).read_text())
        if baseline["fixture_hash"] != report["fixture_hash"]:
            raise RuntimeError("fixture set changed; baseline comparison is invalid")
        for name, workload in report["workloads"].items():
            workload["p95_change_percent"] = (workload["p95_ms"] / baseline["workloads"][name]["p95_ms"] - 1) * 100
            workload["within_10_percent"] = workload["p95_change_percent"] <= 10
    target.write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps({"report": str(target.relative_to(ROOT)), "workloads": report["workloads"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
