#!/usr/bin/env python3
"""Read-only verification of real Langfuse v4 research observations.

GET requests only; no model calls, platform mutations, or raw I/O in reports.
Exit 0: every requested check passed. Exit 1: populated but incomplete evidence,
invalid/authentication response, or incomplete pagination. Exit 2: no evidence
yet or a temporarily unavailable endpoint (pending, never a successful audit).
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import json
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
FIELDS = "core,basic,metadata,model,usage,trace_context,io"
CHECKS = ("retrieval", "query_embedding", "generation", "revision", "learning", "candidates", "confirmation", "reuse")
SAFE_ID = re.compile(r"^[A-Za-z0-9_.:/@-]{1,200}$")
SAFE_NAME = re.compile(r"^[A-Za-z0-9_./: <>-]{1,240}$")


def utc_now():
    return dt.datetime.now(dt.timezone.utc)


def iso(value):
    return value.astimezone(dt.timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def timestamp(value):
    parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("timestamp requires an explicit timezone")
    return parsed.astimezone(dt.timezone.utc)


def decode(value):
    if isinstance(value, str):
        try:
            return json.loads(value)
        except (ValueError, TypeError):
            pass
    return value


def safe_id(value):
    return value if isinstance(value, str) and SAFE_ID.fullmatch(value) else None


def safe_time(value):
    try:
        return iso(timestamp(value))
    except (AttributeError, ValueError, TypeError):
        return None


def io_summary(value):
    value = decode(value)
    if value is None:
        return {"present": False, "bytes": 0, "sha256": None}
    if isinstance(value, str):
        encoded, representation = value.encode(), "utf8"
    else:
        encoded = json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()
        representation = "canonical_json"
    return {"present": True, "bytes": len(encoded), "sha256": hashlib.sha256(encoded).hexdigest(), "representation": representation}


def numeric(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def clean_numeric_map(value):
    value = decode(value)
    if not isinstance(value, dict):
        return {}
    return {key: number for key, number in value.items() if re.fullmatch(r"[A-Za-z0-9_.-]{1,80}", key) and numeric(number)}


def metadata(row):
    value = decode(row.get("metadata"))
    if not isinstance(value, dict):
        return {}
    # v4 maps explicit langfuse.observation.metadata.<key> attributes here.
    # Do not treat arbitrary metadata.attributes as business correlation.
    return {key: decode(item) for key, item in value.items()}


def versions(row):
    value = metadata(row).get("rule_versions")
    if not isinstance(value, dict):
        return {}
    return {key: version for key, version in value.items() if safe_id(key) and isinstance(version, int) and not isinstance(version, bool) and version > 0}


def successful(row):
    return bool(row.get("endTime")) and str(row.get("level", "DEFAULT")).upper() != "ERROR" and metadata(row).get("outcome", "completed") == "completed"


def has_usage(row):
    details = clean_numeric_map(row.get("usageDetails"))
    return any(value > 0 for value in details.values()) or any(numeric(row.get(key)) and row[key] > 0 for key in ("inputUsage", "outputUsage", "totalUsage"))


def precedes(first, second):
    try:
        return timestamp(first["endTime"]) <= timestamp(second["startTime"])
    except (KeyError, ValueError, TypeError):
        return False


class ReadError(Exception):
    def __init__(self, kind, status=None, temporary=False):
        self.kind, self.status, self.temporary = kind, status, temporary
        super().__init__(kind)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        return None


class LangfuseReader:
    def __init__(self, base, env_file, timeout=20):
        parsed = urllib.parse.urlsplit(base)
        if parsed.scheme not in ("http", "https") or parsed.hostname not in ("localhost", "127.0.0.1", "::1") or parsed.username or parsed.password or parsed.query or parsed.fragment:
            raise ValueError("only a credential-free loopback Langfuse URL is allowed")
        self.base = base.rstrip("/")
        self.timeout = timeout
        self.opener = urllib.request.build_opener(NoRedirect())
        values = {}
        # Parse two exact values as data; never source an env file as shell code.
        for line in Path(env_file).read_text().splitlines():
            if "=" not in line or line.lstrip().startswith("#"):
                continue
            key, value = line.split("=", 1)
            key, value = key.strip().removeprefix("export "), value.strip()
            if key not in ("LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY"):
                continue
            if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
                value = value[1:-1]
            values[key] = value
        if not all(values.get(key) for key in ("LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY")):
            raise ValueError("Langfuse project credentials are not configured")
        self.authorization = "Basic " + base64.b64encode((values["LANGFUSE_PUBLIC_KEY"] + ":" + values["LANGFUSE_SECRET_KEY"]).encode()).decode()

    def page(self, since, until, cursor=None, limit=1000):
        query = {"fields": FIELDS, "fromStartTime": iso(since), "toStartTime": iso(until), "limit": limit}
        if cursor:
            query["cursor"] = cursor
        request = urllib.request.Request(self.base + "/api/public/v2/observations?" + urllib.parse.urlencode(query),
                                         headers={"Authorization": self.authorization, "Accept": "application/json"}, method="GET")
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                content = response.read(64 * 1024 * 1024 + 1)
                if len(content) > 64 * 1024 * 1024:
                    raise ReadError("response_exceeds_limit")
        except urllib.error.HTTPError as error:
            # Neither response bodies nor exception URLs/headers enter reports.
            raise ReadError("http_error", error.code, error.code >= 500 or error.code == 429) from None
        except (urllib.error.URLError, TimeoutError, OSError):
            raise ReadError("transport_unavailable", temporary=True) from None
        try:
            body = json.loads(content)
        except (ValueError, UnicodeError):
            raise ReadError("invalid_json") from None
        if not isinstance(body, dict) or not isinstance(body.get("data"), list) or not isinstance(body.get("meta", {}), dict):
            raise ReadError("unexpected_observations_schema")
        for row in body["data"]:
            if not isinstance(row, dict) or not safe_id(row.get("id")) or not safe_id(row.get("traceId")):
                raise ReadError("invalid_observation_identity")
        return body

    def read_all(self, since, until, max_pages):
        rows, cursor, cursors, duplicates = {}, None, set(), set()
        for page_number in range(1, max_pages + 1):
            body = self.page(since, until, cursor)
            for row in body["data"]:
                if row["id"] in rows:
                    duplicates.add(row["id"])
                rows[row["id"]] = row
                if len(rows) > 20000:
                    raise ReadError("observation_limit_reached_narrow_time_window")
            cursor = body.get("meta", {}).get("cursor")
            if cursor is None or cursor == "":
                return list(rows.values()), {"pages": page_number, "complete": True, "duplicate_observation_ids": sorted(duplicates)}
            if not isinstance(cursor, str) or cursor in cursors:
                raise ReadError("pagination_cursor_invalid_or_repeated")
            cursors.add(cursor)
        raise ReadError("pagination_limit_reached")


def scoped_rows(rows, draft_ids):
    if draft_ids:
        traces = {row["traceId"] for row in rows if metadata(row).get("draft_id") in draft_ids or any(str(row.get("sessionId", "")).endswith(":" + item) for item in draft_ids)}
    else:
        traces = {row["traceId"] for row in rows if str(row.get("name", "")).startswith("research.")}
    return [row for row in rows if row["traceId"] in traces]


def is_descendant(row, ancestor, by_id):
    if row.get("traceId") != ancestor.get("traceId"):
        return False
    current, seen = row, set()
    while current and current.get("parentObservationId") and current["id"] not in seen:
        seen.add(current["id"])
        parent = current["parentObservationId"]
        if parent == ancestor["id"]:
            return True
        current = by_id.get(parent)
        if current and current.get("traceId") != row.get("traceId"):
            return False
    return False


def summarize_observation(row):
    meta = metadata(row)
    correlation = {}
    for key in ("kb_id", "draft_id", "proposal_id", "event_id", "prompt_event_id", "parent_event_id", "model_id", "embedding_model_id", "primary_kb_id", "source_kb_id", "rule_id", "source_versions_sha256", "retrieval_snapshot_sha256", "retrieval_query_sha256"):
        if safe_id(meta.get(key)):
            correlation[key] = meta[key]
    for key in ("base_version", "result_version", "restore_version", "rule_version", "rule_count", "source_count", "dimensions", "batch_size", "retrieval_query_bytes", "retrieval_hit_count"):
        if numeric(meta.get(key)):
            correlation[key] = meta[key]
    for key in ("automatic", "has_query_embedding", "learning_enabled", "idempotent_replay"):
        if isinstance(meta.get(key), bool):
            correlation[key] = meta[key]
    for key in ("research_operation", "outcome", "event_kind", "event_status", "rule_status", "requested_status", "call_purpose", "proposal_status", "retrieval_mode"):
        if safe_id(meta.get(key)):
            correlation[key] = meta[key]
    if versions(row):
        correlation["rule_versions"] = versions(row)
    if isinstance(meta.get("retrieval_hits"), list):
        correlation["retrieval_hits"] = [{key: item[key] for key in ("knowledge_id", "chunk_id", "content_sha256", "match_type") if safe_id(item.get(key))} |
                                        {key: item[key] for key in ("rank", "score", "match_type") if numeric(item.get(key))}
                                        for item in meta["retrieval_hits"] if isinstance(item, dict)]
    name = row.get("name", "")
    result = {"id": row["id"], "trace_id": row["traceId"], "parent_observation_id": safe_id(row.get("parentObservationId")),
              "name": name if isinstance(name, str) and SAFE_NAME.fullmatch(name) else "[non-structural name omitted]",
              "type": safe_id(row.get("type")), "level": safe_id(row.get("level")), "completed": bool(row.get("endTime")),
              "start_time": safe_time(row.get("startTime")), "end_time": safe_time(row.get("endTime")),
              "user_id": safe_id(row.get("userId")), "session_id": safe_id(row.get("sessionId")), "model": safe_id(row.get("model")),
              "metadata": correlation, "usage": clean_numeric_map(row.get("usageDetails")),
              "input": io_summary(row.get("input")), "output": io_summary(row.get("output"))}
    for key in ("inputUsage", "outputUsage", "totalUsage"):
        if numeric(row.get(key)):
            result["usage"][key] = row[key]
    if str(name).startswith("embedding."):
        result["usage_note"] = "Platform embedding wrapper estimates tokens; this is not provider billing evidence."
    if name == "retrieve":
        request, output = decode(row.get("input")), decode(row.get("output"))
        if isinstance(request, dict):
            result["query"] = io_summary(request.get("query_text"))
        if isinstance(output, dict):
            result["retrieval_counts"] = {key: output[key] for key in ("total_hits", "vector_hits", "keyword_hits") if numeric(output.get(key))}
            result["hit_ids"] = [{key: item[key] for key in ("chunk_id", "knowledge_id") if safe_id(item.get(key))}
                                 for item in output.get("top_hits", []) if isinstance(item, dict)]
    return result


def evaluate(rows, draft_ids=(), required=CHECKS):
    selected = scoped_rows(rows, draft_ids)
    by_id = {row["id"]: row for row in selected}
    named = lambda name: [row for row in selected if row.get("name") == name and successful(row)]
    checks, links = [], []

    def check(name, matches, reason):
        if name in required:
            checks.append({"name": name, "status": "pass" if matches else "fail", "matched_observation_ids": sorted({row["id"] for row in matches}), "reason": None if matches else reason})

    for draft in draft_ids:
        found = [row for row in selected if metadata(row).get("draft_id") == draft]
        checks.append({"name": "draft_scope", "draft_id": draft, "status": "pass" if found else "fail", "matched_observation_ids": sorted(row["id"] for row in found), "reason": None if found else "No observations carry this exact draft ID in the requested window."})

    retrieves, retrieval_snapshots = [], {}
    for row in named("retrieve"):
        request, output = decode(row.get("input")), decode(row.get("output"))
        if isinstance(request, dict) and isinstance(request.get("query_text"), str) and request["query_text"].strip() and isinstance(output, dict) and numeric(output.get("vector_hits")) and output["vector_hits"] > 0 and metadata(row).get("has_query_embedding") is True:
            for operation in named("research.retrieval"):
                details = metadata(operation)
                hits = details.get("retrieval_hits")
                if is_descendant(row, operation, by_id) and isinstance(hits, list) and len(hits) > 0 and len(hits) == details.get("retrieval_hit_count") and safe_id(details.get("retrieval_snapshot_sha256")) and details.get("retrieval_query_sha256") == io_summary(request["query_text"])["sha256"]:
                    retrieves.append(row)
                    retrieval_snapshots[row["id"]] = operation
                    links.append({"kind": "selected_retrieval_snapshot", "trace_id": row["traceId"], "retrieval_observation_id": row["id"], "research_retrieval_observation_id": operation["id"], "snapshot_sha256": details["retrieval_snapshot_sha256"], "selected_chunk_ids": [hit["chunk_id"] for hit in hits if isinstance(hit, dict) and safe_id(hit.get("chunk_id"))]})
    check("retrieval", retrieves, "No completed research.retrieval snapshot is linked to actual native vector hits, query and selected evidence IDs/hashes.")
    query_embeddings = []
    for retrieval in retrieves:
        query = decode(retrieval.get("input"))["query_text"]
        for embedding in named("embedding.embed"):
            if is_descendant(embedding, retrieval_snapshots[retrieval["id"]], by_id) and decode(embedding.get("input")) == query and metadata(embedding).get("model_id") == metadata(retrieval).get("embedding_model_id") and safe_id(embedding.get("model")) and has_usage(embedding):
                query_embeddings.append(embedding)
                links.append({"kind": "query_embedding_to_retrieval", "trace_id": retrieval["traceId"], "embedding_observation_id": embedding["id"], "retrieval_observation_id": retrieval["id"], "query_sha256": io_summary(query)["sha256"]})
    check("query_embedding", query_embeddings, "No actual embedding call matches retrieval query/model in the same trace (cached vectors alone do not prove a live embedding call).")

    proposals, generations = named("research.draft.propose"), named("chat.completion")
    generated = []
    proposal_generations = {}
    for proposal in proposals:
        pmeta = metadata(proposal)
        evidence_links = []
        for retrieval in retrieves:
            operation = retrieval_snapshots[retrieval["id"]]
            owners = [row for row in selected if row["traceId"] == retrieval["traceId"] and metadata(row).get("draft_id") == pmeta.get("draft_id")]
            if owners and metadata(operation).get("retrieval_snapshot_sha256") == pmeta.get("retrieval_snapshot_sha256"):
                evidence_links.append(retrieval)
        if "retrieval" in required and not evidence_links:
            continue
        for generation in generations:
            gmeta = metadata(generation)
            if is_descendant(generation, proposal, by_id) and pmeta.get("proposal_id") and pmeta.get("prompt_event_id") == gmeta.get("event_id") and pmeta.get("draft_id") == gmeta.get("draft_id") and pmeta.get("model_id") == gmeta.get("model_id") and ("retrieval" not in required or pmeta.get("retrieval_snapshot_sha256") == gmeta.get("retrieval_snapshot_sha256")) and safe_id(generation.get("model")) and has_usage(generation) and io_summary(generation.get("output"))["bytes"] > 0:
                generated.append(generation)
                proposal_generations[proposal["id"]] = generation
                links.append({"kind": "generation_to_proposal", "trace_id": proposal["traceId"], "generation_observation_id": generation["id"], "proposal_observation_id": proposal["id"], "proposal_id": pmeta["proposal_id"], "prompt_event_id": pmeta["prompt_event_id"]})
                for retrieval in evidence_links:
                    links.append({"kind": "retrieval_to_proposal", "retrieval_observation_id": retrieval["id"], "proposal_observation_id": proposal["id"], "draft_id": pmeta.get("draft_id"), "retrieval_snapshot_sha256": pmeta.get("retrieval_snapshot_sha256"), "same_trace": retrieval["traceId"] == proposal["traceId"]})
    check("generation", generated, "No model generation has a valid parent chain, prompt event, draft, model, usage, completed proposal and requested retrieval evidence association.")
    revisions = [row for name in ("research.draft.save", "research.draft.accept", "research.draft.revert") for row in named(name)
                 if safe_id(metadata(row).get("event_id")) and numeric(metadata(row).get("base_version")) and numeric(metadata(row).get("result_version")) and metadata(row)["result_version"] >= metadata(row)["base_version"]]
    check("revision", revisions, "No committed save/accept/revert observation has event ID and base/result versions.")

    learning, extracted = [], []
    for enqueue in named("research.learning.enqueue"):
        event_id = metadata(enqueue).get("event_id")
        saves = [row for row in revisions if metadata(row).get("event_id") == event_id and is_descendant(enqueue, row, by_id)]
        if not saves:
            continue
        for worker in named("research.learning.worker"):
            if metadata(worker).get("event_id") != event_id or not is_descendant(worker, enqueue, by_id):
                continue
            for extraction in named("research.rules.extract_event"):
                emeta = metadata(extraction)
                if emeta.get("event_id") != event_id or emeta.get("automatic") is not True or not is_descendant(extraction, worker, by_id):
                    continue
                calls = [row for row in generations if is_descendant(row, extraction, by_id) and metadata(row).get("event_id") == event_id and metadata(row).get("model_id") == emeta.get("model_id") and has_usage(row)]
                if calls:
                    learning.append(worker)
                    extracted.append(extraction)
                    links.append({"kind": "save_to_background_learning", "trace_id": enqueue["traceId"], "event_id": event_id,
                                  "save_observation_id": saves[0]["id"], "enqueue_observation_id": enqueue["id"], "worker_observation_id": worker["id"], "extraction_observation_id": extraction["id"], "generation_observation_ids": [row["id"] for row in calls]})
    check("learning", learning, "Missing same-trace parent chain from committed save through enqueue, async worker, event extraction and model call.")
    candidates = [row for row in extracted if versions(row) and metadata(row).get("rule_count") == len(versions(row))]
    check("candidates", candidates, "Automatic extraction has no recorded candidate rule IDs/versions; an empty valid rules array is not candidate creation evidence.")
    confirmed = []
    candidate_for_rule = {rule_id: row for row in candidates for rule_id in versions(row)}
    for decision in named("research.rules.decide"):
        dmeta = metadata(decision)
        candidate = candidate_for_rule.get(dmeta.get("rule_id"))
        if candidate and precedes(candidate, decision) and dmeta.get("rule_status") == "active" and dmeta.get("event_id") == metadata(candidate).get("event_id") and dmeta.get("base_version") == versions(candidate)[dmeta["rule_id"]] and dmeta.get("rule_version") == dmeta["base_version"] + 1:
            confirmed.append(decision)
            links.append({"kind": "candidate_confirmation", "candidate_observation_id": candidate["id"], "confirmation_observation_id": decision["id"], "rule_id": dmeta["rule_id"], "active_version": dmeta["rule_version"]})
    check("confirmation", confirmed, "No explicit active-rule decision matches an automatic candidate and increments its recorded version.")
    reused = []
    for decision in confirmed:
        dmeta = metadata(decision)
        for proposal in proposals:
            pmeta = metadata(proposal)
            generation = proposal_generations.get(proposal["id"])
            if not generation or not precedes(decision, proposal) or versions(proposal).get(dmeta["rule_id"]) != dmeta["rule_version"] or versions(generation).get(dmeta["rule_id"]) != dmeta["rule_version"]:
                continue
            for acceptance in named("research.draft.accept"):
                if precedes(proposal, acceptance) and metadata(acceptance).get("proposal_id") == pmeta.get("proposal_id") and versions(acceptance).get(dmeta["rule_id"]) == dmeta["rule_version"]:
                    reused.append(proposal)
                    links.append({"kind": "confirmed_rule_reused_and_accepted", "rule_id": dmeta["rule_id"], "rule_version": dmeta["rule_version"], "proposal_id": pmeta["proposal_id"], "confirmation_observation_id": decision["id"], "generation_observation_id": generation["id"], "proposal_observation_id": proposal["id"], "acceptance_observation_id": acceptance["id"]})
    check("reuse", reused, "No confirmed automatic rule version appears in both a real proposal generation and its accepted revision.")
    return {"selected_observation_count": len(selected), "trace_count": len({row["traceId"] for row in selected}), "checks": checks, "links": links,
            "observations": [summarize_observation(row) for row in sorted(selected, key=lambda item: (item.get("startTime", ""), item["id"]))]}


def write_report(path, report):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(path)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://localhost:3000")
    parser.add_argument("--env-file", type=Path, default=ROOT / ".research-observability.env")
    parser.add_argument("--since", help="inclusive ISO8601 start time with timezone; capture before generating activity")
    parser.add_argument("--until", help="exclusive ISO8601 end time; default advances with polling")
    parser.add_argument("--draft-id", action="append", default=[])
    parser.add_argument("--require", default=",".join(CHECKS), help="comma-separated required checks: " + ",".join(CHECKS))
    parser.add_argument("--wait-seconds", type=float, default=120)
    parser.add_argument("--poll-seconds", type=float, default=5)
    parser.add_argument("--max-pages", type=int, default=100)
    parser.add_argument("--probe", action="store_true", help="only inspect authenticated API shape; never reports workflow success")
    parser.add_argument("--output", type=Path, default=ROOT / "artifacts/research/semantic-langfuse-acceptance/langfuse-verification.json")
    args = parser.parse_args(argv)
    report = {"schema_version": 1, "api": "/api/public/v2/observations", "read_only": True, "real_endpoint": True,
              "checked_at": iso(utc_now()), "status": "pending", "checks": [], "limitations": ["Observation presence proves instrumentation and correlation, not retrieval relevance or long-term writing quality.", "Embedding token usage is estimated by the platform wrapper."]}
    try:
        if not args.since and not args.probe:
            raise ValueError("--since is required for a complete workflow audit")
        since = timestamp(args.since) if args.since else utc_now() - dt.timedelta(hours=1)
        until = timestamp(args.until) if args.until else None
        if until and until <= since:
            raise ValueError("--until must be later than --since")
        if args.wait_seconds < 0 or not 1 <= args.poll_seconds <= 30 or not 1 <= args.max_pages <= 1000:
            raise ValueError("invalid polling/pagination bounds")
        if any(not safe_id(value) for value in args.draft_id):
            raise ValueError("draft IDs must contain only identifier characters")
        required = tuple(value for value in args.require.split(",") if value)
        if not required or any(value not in CHECKS for value in required):
            raise ValueError("unknown or empty required check list")
        reader = LangfuseReader(args.base_url, args.env_file)
        report.update({"from_start_time": iso(since), "draft_ids": args.draft_id, "required_checks": list(required)})
        deadline, attempts, last_error = time.monotonic() + args.wait_seconds, 0, None
        while True:
            attempts += 1
            upper = until or utc_now() + dt.timedelta(seconds=1)
            try:
                if args.probe:
                    body = reader.page(since, upper, limit=1)
                    report.update({"status": "api_ready" if body["data"] else "pending", "workflow_verified": False, "api_status": 200,
                                   "response_keys": sorted(body), "meta_keys": sorted(body.get("meta", {})), "returned_rows": len(body["data"]),
                                   "observation_fields": sorted(body["data"][0]) if body["data"] else []})
                    break
                rows, pagination = reader.read_all(since, upper, args.max_pages)
                report.update(evaluate(rows, args.draft_id, required))
                report.update({"api_status": 200, "pagination": pagination, "fetched_observation_count": len(rows), "to_start_time": iso(upper)})
                last_error = None
                passed = bool(report["selected_observation_count"]) and all(item["status"] == "pass" for item in report["checks"])
                if passed:
                    report["status"] = "pass"
                    break
                report["status"] = "fail" if report["selected_observation_count"] else "pending"
            except ReadError as error:
                last_error = {"kind": error.kind, "http_status": error.status}
                report["status"] = "pending" if error.temporary else "fail"
                if not error.temporary:
                    break
            if time.monotonic() >= deadline:
                break
            time.sleep(min(args.poll_seconds, max(0, deadline - time.monotonic())))
        report["attempts"] = attempts
        if last_error:
            report["read_error"] = last_error
        report["checked_at"] = iso(utc_now())
    except (ValueError, OSError, KeyError) as error:
        # Configuration values and local file contents never enter diagnostics.
        report.update({"status": "fail", "configuration_error_type": type(error).__name__})
    write_report(args.output, report)
    print(json.dumps({"status": report["status"], "observations": report.get("selected_observation_count", report.get("returned_rows", 0)),
                      "checks_passed": sum(item["status"] == "pass" for item in report["checks"]), "checks_failed": sum(item["status"] == "fail" for item in report["checks"]),
                      "read_error": report.get("read_error"), "report": str(args.output)}, ensure_ascii=False))
    return 0 if report["status"] in ("pass", "api_ready") else 2 if report["status"] == "pending" else 1


if __name__ == "__main__":
    sys.exit(main())
