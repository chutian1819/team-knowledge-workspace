#!/usr/bin/env python3
"""Read-only preservation and index checks against the semantic acceptance baseline.

SQL returns only identifiers, counts, booleans and hashes, never model parameters,
credentials, source text, draft bodies or private writing instructions.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_DIR = ROOT / "artifacts/research/semantic-langfuse-acceptance"
EXPECTED_COUNTS = {"chunks": 220, "sources": 6, "versions": 11, "drafts": 3}
SOURCE_EXCLUSIONS = ["embedding_model_id", "enable_status", "updated_at", "process_state", "process_config"]


def uuid_literal(value):
    return "'" + str(uuid.UUID(value)) + "'"


def sql(query):
    command = ["docker", "exec", "-i", "context-workspace-postgres", "sh", "-c",
               'exec psql -X -q -t -A -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"']
    result = subprocess.run(command, input="BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n" + query + "\nCOMMIT;",
                            capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise RuntimeError("read-only database verification failed (database output withheld)")
    # PostgreSQL json_agg(record) may put line breaks between array elements.
    pending, decoded, decoder = result.stdout.strip(), [], json.JSONDecoder()
    while pending:
        value, end = decoder.raw_decode(pending)
        decoded.append(value)
        pending = pending[end:].lstrip()
    return decoded


def compare_rows(baseline, current):
    expected = {item["id"]: item["digest"] for item in baseline}
    actual = {item["id"]: item["digest"] for item in current}
    missing = sorted(set(expected) - set(actual))
    changed = sorted(key for key in expected.keys() & actual.keys() if expected[key] != actual[key])
    return {"expected_count": len(expected), "found_count": len(actual), "missing_ids": missing,
            "changed_ids": changed, "pass": not missing and not changed and len(actual) == len(expected)}


def verify_files(baseline):
    if len(baseline) != 122 or len({item["path"] for item in baseline}) != len(baseline):
        raise ValueError("unexpected file baseline cardinality")
    for item in baseline:
        if not item["path"].startswith("/data/files/") or any(c in item["path"] for c in "\n\r\x00\\"):
            raise ValueError("unsupported original file path in baseline")
        if not re.fullmatch(r"[0-9a-f]{64}", item["sha256"]):
            raise ValueError("invalid file digest in baseline")
    result = subprocess.run(["docker", "exec", "context-workspace-app", "sha256sum", "--",
                             *[item["path"] for item in baseline]], capture_output=True, text=True, timeout=120)
    actual = {}
    for line in result.stdout.splitlines():
        if len(line) >= 67 and re.fullmatch(r"[0-9a-f]{64}", line[:64]) and line[64:66] in ("  ", " *"):
            actual[line[66:]] = line[:64]
    missing = [item["path"] for item in baseline if item["path"] not in actual]
    changed = [item["path"] for item in baseline if item["path"] in actual and actual[item["path"]] != item["sha256"]]
    return {"expected_count": len(baseline), "found_count": len(actual), "missing_paths": missing,
            "changed_paths": changed, "pass": result.returncode == 0 and not missing and not changed,
            "additional_files_allowed": True}


def fingerprint_query(category, table, expression, baseline):
    ids = ",".join(uuid_literal(item["id"]) for item in baseline)
    return ("SELECT json_build_object('category','" + category + "','rows',coalesce(json_agg(r ORDER BY id),'[]'::json)) "
            "FROM (SELECT id,md5((" + expression + ")::text) AS digest FROM public." + table +
            " t WHERE id IN (" + ids + ")) r;")


def verify_database(evidence, preservation, config):
    for category, count in EXPECTED_COUNTS.items():
        if len(evidence[category]) != count or len({item["id"] for item in evidence[category]}) != count:
            raise ValueError("unexpected evidence baseline cardinality: " + category)
    schema = sql("SELECT json_agg(column_name ORDER BY ordinal_position) FROM information_schema.columns "
                 "WHERE table_schema='public' AND table_name='models';")[0]
    old_columns = preservation["columns"]["models"]
    if not set(old_columns).issubset(schema):
        raise RuntimeError("original model columns are missing")
    added_columns = sorted(set(schema) - set(old_columns))
    if any(not re.fullmatch(r"[a-z_][a-z0-9_]*", name) for name in added_columns):
        raise ValueError("unsupported model column identifier")
    model_expr = "to_jsonb(t)"
    if added_columns:
        model_expr += " - ARRAY[" + ",".join("'" + name + "'" for name in added_columns) + "]::text[]"
    kb, model = uuid_literal(config["knowledge_base_id"]), uuid_literal(config["embedding_model_id"])
    expressions = {
        "chunks": ("chunks", "json_build_array(id,knowledge_id,knowledge_base_id,chunk_type,content,start_at,end_at)"),
        "sources": ("knowledges", "to_jsonb(t)-ARRAY[" + ",".join("'" + name + "'" for name in SOURCE_EXCLUSIONS) + "]"),
        "versions": ("research_source_versions", "to_jsonb(t)"),
        "drafts": ("wiki_pages", "to_jsonb(t)"),
    }
    queries = [fingerprint_query(category, table, expression, evidence[category])
               for category, (table, expression) in expressions.items()]
    queries.append("SELECT json_build_object('category','models','count',count(*),'digest',"
                   "md5(coalesce(string_agg(md5((" + model_expr + ")::text),'' ORDER BY (" + model_expr + ")::text),''))) "
                   "FROM public.models t WHERE id <> " + model + ";")
    queries.append("""
SELECT json_build_object('category','index',
 'knowledge_base_id', %(kb)s, 'embedding_model_id', %(model)s,
 'source_count',(SELECT count(*) FROM knowledges WHERE knowledge_base_id=%(kb)s),
 'sources_with_current_model',(SELECT count(*) FROM knowledges WHERE knowledge_base_id=%(kb)s AND embedding_model_id=%(model)s),
 'chunk_count',(SELECT count(*) FROM chunks WHERE knowledge_base_id=%(kb)s),
 'ready_chunk_count',(SELECT count(*) FROM chunks WHERE knowledge_base_id=%(kb)s AND index_status='ready'),
 'embedding_count',count(*),'distinct_chunk_ids',count(DISTINCT e.chunk_id),
 'enabled_embedding_count',count(*) FILTER (WHERE e.is_enabled),
 'dimension_values',array_agg(DISTINCT e.dimension),
 'actual_vector_dimensions',array_agg(DISTINCT vector_dims(e.embedding)),
 'canonical_ready_bindings',count(*) FILTER (WHERE c.id=e.chunk_id AND c.knowledge_id=e.knowledge_id
    AND c.knowledge_base_id=e.knowledge_base_id AND c.index_status='ready'),
 'sources', (SELECT coalesce(json_agg(s ORDER BY s.knowledge_id),'[]'::json) FROM
   (SELECT e2.knowledge_id,count(*) AS embedding_count,count(DISTINCT e2.chunk_id) AS distinct_chunk_ids
    FROM embeddings e2 WHERE e2.knowledge_base_id=%(kb)s GROUP BY e2.knowledge_id) s))
FROM embeddings e LEFT JOIN chunks c ON c.id=e.chunk_id WHERE e.knowledge_base_id=%(kb)s;
""" % {"kb": kb, "model": model})
    rows = {item["category"]: item for item in sql("\n".join(queries))}
    result = {category: compare_rows(evidence[category], rows[category]["rows"]) for category in expressions}
    expected_models = next(item for item in preservation["tables"] if item["table"] == "models")
    result["models"] = {"expected_count": expected_models["count"], "found_count": rows["models"]["count"],
                        "expected_digest": expected_models["digest"], "actual_digest": rows["models"]["digest"],
                        "excluded_new_model_id": config["embedding_model_id"], "ignored_new_columns": added_columns,
                        "pass": all(rows["models"][field] == expected_models[field] for field in ("count", "digest"))}
    index = rows["index"]
    index.pop("category")
    index["pass"] = (index["source_count"] == index["sources_with_current_model"] == EXPECTED_COUNTS["sources"]
                     and all(index[field] == EXPECTED_COUNTS["chunks"] for field in
                             ("chunk_count", "ready_chunk_count", "embedding_count", "distinct_chunk_ids",
                              "enabled_embedding_count", "canonical_ready_bindings"))
                     and index["dimension_values"] == index["actual_vector_dimensions"] == [config["dimension"]]
                     and {item["knowledge_id"] for item in index["sources"]} == {item["id"] for item in evidence["sources"]})
    result["index"] = index
    return result


def json_literal(value):
    # JSON is passed to psql stdin, never shell interpolation or command arguments.
    return "'" + json.dumps(value, ensure_ascii=False).replace("'", "''") + "'::jsonb"


def verify_writing(draft_id, writing_dir):
    draft = uuid_literal(draft_id)
    proposal_fields = ["id", "tenant_id", "knowledge_base_id", "page_id", "base_version", "base_content",
                       "content", "instruction", "model_id", "source_refs", "source_versions", "writing_rules"]
    event_fields = ["id", "knowledge_base_id", "page_id", "kind", "instruction", "base_version", "result_version",
                    "base_content", "content", "model_id", "proposal_id", "parent_event_id", "prompt_event_id", "status"]
    expected_proposals, expected_events, artifacts = [], [], []
    for path in sorted((writing_dir / "rounds").glob("*/proposal.json")):
        proposal = json.loads(path.read_text())
        if proposal["page_id"] != draft_id:
            raise ValueError("writing artifact belongs to another draft")
        expected_proposals.append({key: proposal[key] for key in proposal_fields})
        events_path = path.with_name("events.json")
        if not events_path.exists():
            raise RuntimeError("writing round has no completed event evidence: " + path.parent.name)
        events = json.loads(events_path.read_text())
        if len(events) != 3 or {item["kind"] for item in events} != {"prompt", "proposal", "accept"}:
            raise ValueError("writing round must contain prompt, proposal and accept evidence")
        expected_events.extend({key: item.get(key, "") for key in event_fields} for item in events)
        artifacts.append({"round": path.parent.name, "proposal_id": proposal["id"],
                          "captured_proposal_status": proposal["status"],
                          "proposal_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                          "events_sha256": hashlib.sha256(events_path.read_bytes()).hexdigest()})
    if not expected_proposals or len({item["id"] for item in expected_proposals}) != len(expected_proposals):
        raise ValueError("missing or duplicate writing proposal artifacts")
    if len({item["id"] for item in expected_events}) != len(expected_events):
        raise ValueError("duplicate writing event artifacts")
    queries = []
    for category, table, expected in [("proposal_artifacts", "research_proposals", expected_proposals),
                                      ("public_event_artifacts", "research_writing_events", expected_events)]:
        queries.append("WITH expected AS (SELECT value AS j FROM jsonb_array_elements(" + json_literal(expected) + ")) "
                       "SELECT json_build_object('category','" + category + "','rows',coalesce(json_agg(r),'[]'::json)) FROM "
                       "(SELECT expected.j->>'id' AS id,t.id IS NOT NULL AS found,"
                       "NOT EXISTS (SELECT 1 FROM jsonb_each(expected.j) f WHERE to_jsonb(t)->f.key IS DISTINCT FROM f.value) "
                       "AS matches FROM expected LEFT JOIN " + table + " t ON t.id=expected.j->>'id') r;")
    queries.append("""
SELECT json_build_object('category','private_event_snapshots','rows',coalesce(json_agg(r ORDER BY r.base_version),'[]'::json))
FROM (SELECT p.id AS proposal_id,p.base_version,p.status,p.model_id,
 md5(p.base_content) AS base_content_md5,md5(p.content) AS content_md5,
 md5(p.source_versions::jsonb::text) AS source_versions_md5,md5(p.writing_rules::jsonb::text) AS writing_rules_md5,
 jsonb_array_length(coalesce(p.writing_rules::jsonb,'[]'::jsonb)) AS writing_rule_count,
 (SELECT count(*) FROM research_writing_events WHERE proposal_id=p.id AND kind='proposal') AS proposal_event_count,
 (SELECT count(*) FROM research_writing_events WHERE proposal_id=p.id AND kind='accept') AS accept_event_count,
 (SELECT count(*) FROM research_writing_events e JOIN research_writing_events q ON q.id=e.prompt_event_id
    WHERE e.proposal_id=p.id AND e.kind='proposal' AND q.kind='prompt' AND q.page_id=p.page_id) AS prompt_event_count,
 NOT EXISTS (SELECT 1 FROM research_writing_events e WHERE e.proposal_id=p.id AND e.kind IN ('proposal','accept') AND
   (e.source_versions::jsonb IS DISTINCT FROM p.source_versions::jsonb
    OR e.writing_rules::jsonb IS DISTINCT FROM p.writing_rules::jsonb
    OR e.base_content IS DISTINCT FROM p.base_content OR e.content IS DISTINCT FROM p.content
    OR e.model_id IS DISTINCT FROM p.model_id OR e.instruction IS DISTINCT FROM p.instruction
    OR e.base_version IS DISTINCT FROM p.base_version OR e.page_id IS DISTINCT FROM p.page_id
    OR e.source_refs::jsonb IS DISTINCT FROM p.source_refs::jsonb OR e.status <> 'completed'
    OR (e.kind='accept' AND e.result_version<>p.base_version+1)
    OR (e.kind='proposal' AND e.result_version<>0))) AS proposal_accept_snapshots_match,
 NOT EXISTS (SELECT 1 FROM research_writing_events e JOIN research_writing_events q ON q.id=e.prompt_event_id
    WHERE e.proposal_id=p.id AND e.kind='proposal' AND
   (q.source_versions::jsonb IS DISTINCT FROM p.source_versions::jsonb
    OR q.writing_rules::jsonb IS DISTINCT FROM p.writing_rules::jsonb
    OR q.base_content IS DISTINCT FROM p.base_content OR q.content IS DISTINCT FROM ''
    OR q.model_id IS DISTINCT FROM p.model_id OR q.instruction IS DISTINCT FROM p.instruction
    OR q.base_version IS DISTINCT FROM p.base_version OR q.source_refs::jsonb IS DISTINCT FROM p.source_refs::jsonb
    OR q.result_version<>0 OR q.status<>'submitted')) AS prompt_snapshot_matches,
 p.source_versions::jsonb->'_retrieval'->>'mode' AS retrieval_mode,
 jsonb_array_length(p.source_versions::jsonb->'_retrieval'->'results') AS retrieval_chunk_count
FROM research_proposals p WHERE p.page_id=%(draft)s) r;
SELECT json_build_object('category','current_draft','rows',coalesce(json_agg(r),'[]'::json)) FROM
 (SELECT p.id AS page_id,p.version,
   (SELECT count(*) FROM research_writing_events WHERE page_id=p.id) AS event_count,
   (SELECT count(*) FROM wiki_page_revisions WHERE page_id=p.id) AS revision_count,
   e.id AS latest_accept_event_id,
   e.kind='accept' AND e.result_version=p.version AND e.content=p.content
      AND e.source_versions::jsonb IS NOT DISTINCT FROM p.page_metadata::jsonb->'source_versions' AS accepted_page_snapshot_matches
  FROM wiki_pages p LEFT JOIN LATERAL
   (SELECT * FROM research_writing_events WHERE page_id=p.id AND kind='accept' ORDER BY result_version DESC LIMIT 1) e ON true
  WHERE p.id=%(draft)s) r;
SELECT json_build_object('category','frozen_rule_provenance','rows',coalesce(json_agg(r),'[]'::json)) FROM
 (SELECT p.id AS proposal_id,snapshot->>'id' AS rule_id,(snapshot->>'version')::int AS frozen_version,
   rules.version AS current_version,rules.status AS current_status,rules.source_page_id,
   md5(snapshot->>'instruction') AS frozen_instruction_md5,
   rules.id IS NOT NULL AND rules.knowledge_base_id=p.knowledge_base_id AND rules.source_page_id=p.page_id AS provenance_matches
  FROM research_proposals p CROSS JOIN LATERAL jsonb_array_elements(coalesce(p.writing_rules::jsonb,'[]'::jsonb)) snapshot
  LEFT JOIN research_writing_rules rules ON rules.id=snapshot->>'id'
  WHERE p.page_id=%(draft)s) r;
""" % {"draft": draft})
    rows = {item["category"]: item["rows"] for item in sql("\n".join(queries))}
    proposal_count = len(expected_proposals)
    checks = {
        "api_proposals_match_database": len(rows["proposal_artifacts"]) == proposal_count and
            all(item["found"] and item["matches"] for item in rows["proposal_artifacts"]),
        "api_events_match_database": len(rows["public_event_artifacts"]) == len(expected_events) and
            all(item["found"] and item["matches"] for item in rows["public_event_artifacts"]),
        "all_round_private_snapshots_match": len(rows["private_event_snapshots"]) == proposal_count and
            all(item["status"] == "accepted" and item["proposal_event_count"] == item["accept_event_count"] == item["prompt_event_count"] == 1
                and item["proposal_accept_snapshots_match"] and item["prompt_snapshot_matches"]
                and item["retrieval_mode"] == "vector_only" and item["retrieval_chunk_count"] > 0
                for item in rows["private_event_snapshots"]),
        "accepted_draft_and_history_match": len(rows["current_draft"]) == 1 and
            rows["current_draft"][0]["accepted_page_snapshot_matches"] is True and
            rows["current_draft"][0]["version"] == proposal_count + 1 and
            rows["current_draft"][0]["revision_count"] == proposal_count and
            rows["current_draft"][0]["event_count"] == proposal_count * 3 + 1,
        "frozen_rules_retain_provenance": all(item["provenance_matches"] for item in rows["frozen_rule_provenance"]),
    }
    return {"pass": all(checks.values()), "draft_id": draft_id, "round_count": proposal_count,
            "artifact_status_policy": "proposal API evidence may precede acceptance; immutable fields match exactly and current accepted status is checked separately",
            "expected_workflow_event_count": len(expected_events), "checks": checks, "artifacts": artifacts, **rows}


def write_private_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix="." + path.name + ".", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-dir", type=Path, default=DEFAULT_DIR)
    parser.add_argument("--output", type=Path, default=DEFAULT_DIR / "preservation-after-index.json")
    parser.add_argument("--draft-id", help="Also compare private writing snapshots with completed API round evidence")
    parser.add_argument("--writing-dir", type=Path, default=ROOT / "artifacts/research/semantic-acceptance/writing/semantic")
    args = parser.parse_args()
    names = ["evidence-baseline.json", "file-baseline.json", "preservation-before.json", "index-configuration.json"]
    loaded = {name: json.loads((args.baseline_dir / name).read_text()) for name in names}
    report = {"checked_at": datetime.now(timezone.utc).isoformat(), "read_only": True,
              "baseline_sha256": {name: hashlib.sha256((args.baseline_dir / name).read_bytes()).hexdigest() for name in names},
              "scope": {"chunk_fields": ["id", "knowledge_id", "knowledge_base_id", "chunk_type", "content", "start_at", "end_at"],
                        "source_excluded_fields": SOURCE_EXCLUSIONS,
                        "source_versions_and_original_drafts": "complete original rows",
                        "models": "original full-table fingerprint, excluding only the added embedding model",
                        "files": "all original paths and SHA-256; additional files allowed",
                        "index": "database vector dimensions and canonical ready chunk/source bindings; index content intentionally adds source/section context"}}
    report["checks"] = verify_database(loaded["evidence-baseline.json"], loaded["preservation-before.json"], loaded["index-configuration.json"])
    report["checks"]["original_files"] = verify_files(loaded["file-baseline.json"])
    if args.draft_id:
        report["checks"]["writing_snapshots"] = verify_writing(args.draft_id, args.writing_dir)
    report["pass"] = all(item["pass"] for item in report["checks"].values())
    write_private_json(args.output, report)
    print(json.dumps({"pass": report["pass"], "checks": {name: item["pass"] for name, item in report["checks"].items()},
                      "report": str(args.output)}, ensure_ascii=False))
    raise SystemExit(0 if report["pass"] else 1)


if __name__ == "__main__":
    main()
