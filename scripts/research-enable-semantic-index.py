#!/usr/bin/env python3
"""Enable native local embeddings and rebuild only the existing QA source index."""
import argparse
import importlib.util
import json
from pathlib import Path
import time

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "artifacts/research/semantic-langfuse-acceptance"
KB = "9ca4d280-c5ec-4f77-a139-77172a2b61d9"
QA_MODEL = "a0b0af33-2797-4426-a874-5cac8b351f0d"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rebuild", action="store_true")
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location("acceptance", ROOT / "scripts/research-writing-acceptance.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    api = module.API(json.loads((ROOT / ".research-state.json").read_text()), "ui")
    models = api.data("GET", "/models")
    candidates = [m for m in models if m["name"] == "embeddinggemma:300m" and m["type"] == "Embedding"]
    if len(candidates) > 1:
        raise RuntimeError("Ambiguous embedding configuration; no changes made")
    if candidates:
        model = candidates[0]
        if model["source"] != "local" or model["parameters"]["embedding_parameters"]["dimension"] != 768:
            raise RuntimeError("Existing model configuration differs; left unchanged")
    else:
        model = api.data("POST", "/models", {"name": "embeddinggemma:300m", "display_name": "本地语义检索 · EmbeddingGemma",
            "type": "Embedding", "source": "local", "status": "active", "is_default": False,
            "description": "本机多语言向量模型，768维；原生知识库索引与检索使用。",
            # Native local models use the administrator's OLLAMA_BASE_URL;
            # a per-model BaseURL is ignored by that adapter.
            "parameters": {"max_concurrency": 1,
                "embedding_parameters": {"dimension": 768, "truncate_prompt_tokens": 2048,
                                         "supports_dimension_override": False}}})
    kb = api.data("GET", "/knowledge-bases/" + KB)
    if not kb["name"].startswith("验收 · 具身智能"):
        raise RuntimeError("The source KB is not the expected QA fixture")
    if kb.get("embedding_model_id") not in (None, "", model["id"]):
        raise RuntimeError("A different embedding model is already bound; left unchanged")
    chunks = kb.get("chunking_config") or {}
    field_map = {"chunk_size": "chunkSize", "chunk_overlap": "chunkOverlap", "separators": "separators",
        "parser_engine_rules": "parserEngineRules", "enable_parent_child": "enableParentChild",
        "parent_chunk_size": "parentChunkSize", "child_chunk_size": "childChunkSize", "strategy": "strategy",
        "token_limit": "tokenLimit", "languages": "languages", "table_metadata_instructions": "tableMetadataInstructions"}
    extraction = dict(kb.get("extract_config") or {})
    if "custom_instructions" in extraction:
        extraction["customInstructions"] = extraction.pop("custom_instructions")
    question = dict(kb.get("question_generation_config") or {"enabled": False})
    for old, new in (("question_count", "questionCount"), ("custom_instructions", "customInstructions")):
        if old in question:
            question[new] = question.pop(old)
    body = {"llmModelId": kb.get("summary_model_id") or QA_MODEL, "embeddingModelId": model["id"],
        "vlm_config": kb.get("vlm_config"), "asr_config": kb.get("asr_config"),
        "multimodal": {"enabled": bool((kb.get("vlm_config") or {}).get("enabled"))},
        "documentSplitting": {new: chunks[old] for old, new in field_map.items() if old in chunks},
        "storageBackendId": kb.get("storage_backend_id", ""), "nodeExtract": extraction,
        "questionGeneration": question}
    if kb.get("embedding_model_id") != model["id"]:
        api.data("PUT", "/initialization/config/" + KB, body)
    strategy = {"vector_enabled": True, "keyword_enabled": False, "graph_enabled": False, "wiki_enabled": False}
    if kb.get("indexing_strategy") != strategy:
        api.data("PUT", "/knowledge-bases/" + KB, {"name": kb["name"], "description": kb.get("description", ""),
            "config": {"chunking_config": chunks, "indexing_strategy": strategy}})
    current = api.data("GET", "/knowledge-bases/" + KB)
    assert current["vlm_config"] == kb["vlm_config"], "VLM configuration changed unexpectedly"
    assert current["chunking_config"] == chunks, "Chunking configuration changed unexpectedly"
    assert current["embedding_model_id"] == model["id"] and current["indexing_strategy"] == strategy
    report = {"knowledge_base_id": KB, "embedding_model_id": model["id"], "dimension": 768,
        "vector_only": True, "vlm_preserved": True, "chunking_preserved": True}
    OUTPUT.mkdir(parents=True, exist_ok=True)
    (OUTPUT / "index-configuration.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False), flush=True)
    if args.rebuild:
        endpoint = "/knowledge-bases/" + KB + "/rebuild-index"
        progress = api.data("POST", endpoint, {}, expected=(202,))
        deadline = time.monotonic() + 900
        previous = None
        while time.monotonic() < deadline:
            progress = api.data("GET", endpoint)
            (OUTPUT / "index-rebuild.json").write_text(json.dumps(progress, ensure_ascii=False, indent=2))
            summary = {key: progress.get(key) for key in ("status", "documents_total", "documents_indexed", "chunks_indexed", "error")}
            if summary != previous:
                print(json.dumps(summary, ensure_ascii=False), flush=True)
                previous = summary
            if progress.get("status") in ("completed", "failed"):
                break
            time.sleep(5)
        if progress.get("status") != "completed":
            raise RuntimeError("Index rebuild has not completed; inspect the saved progress")


if __name__ == "__main__":
    main()
