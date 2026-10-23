#!/usr/bin/env python3
"""Round-trip real DOCX/PDF exports through native parsing without fake models."""
import importlib.util
import json
from pathlib import Path
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    spec = importlib.util.spec_from_file_location("capture_acceptance", ROOT / "scripts/research-live-acceptance.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    suite = module.Acceptance()
    kb = suite.qa["native_source_kb_id"]
    state_path = ROOT / ".research-document-state.json"
    state = json.loads(state_path.read_text()) if state_path.exists() else {}
    report = {"scope": "synthetic exported research files, native document reader and chunking; no AI quality claim", "checks": []}
    try:
        for format in ("docx", "pdf"):
            if not state.get(format):
                item = suite.upload("owner", "POST", f"/knowledge-bases/{kb}/knowledge/file",
                    (ROOT / f"artifacts/research/research-draft.{format}").read_bytes(),
                    "round-trip." + format, {"process_config": json.dumps({"summary_enabled": False,
                        "graph_enabled": False, "question_generation_config": {"enabled": False}})}, allowed=(200, 201, 409))
                state[format] = item["id"]
                state_path.write_text(json.dumps(state))
                state_path.chmod(0o600)
            suite.wait_native_document(state[format])
            _, chunks = suite.request("owner", "GET", "/chunks/" + state[format])
            if isinstance(chunks, dict):
                chunks = chunks.get("chunks", [])
            text = "\n".join(chunk.get("content", "") for chunk in chunks)
            assert "人工结论" in text and "中文表格" in text, "Chinese headings/table content did not survive import"
            _, versions = suite.request("owner", "GET", f"/research/capture/{kb}/sources/{state[format]}")
            assert any(version.get("has_original") for version in versions), "Original version missing"
            report["checks"].append({"format": format, "status": "passed", "chunks": len(chunks), "characters": len(text)})
        report["status"] = "passed"
    except Exception as error:
        report["status"] = "failed"
        report["error"] = str(error) if isinstance(error, (module.APIError, AssertionError)) else type(error).__name__
        raise
    finally:
        target = ROOT / "artifacts/research/document-round-trip-report.json"
        target.write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({"report": str(target.relative_to(ROOT)), "status": report["status"]}))


if __name__ == "__main__":
    main()
