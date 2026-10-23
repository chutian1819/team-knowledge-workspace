#!/usr/bin/env python3
"""Install the pinned public Ollama embedding model, verifying every blob.

The host downloader works with macOS transparent proxies; it does not disable
Ollama's redirect validation or expose the model service on a host port.
"""
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
CACHE = ROOT / ".research-model-cache" / "embeddinggemma-300m"
REGISTRY = "https://registry.ollama.ai/v2/library/embeddinggemma"
MODEL_DIGEST = "sha256:0800cbac9c2064dde519420e75e512a83cb360de3ad5df176185dc69652fc515"


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return "sha256:" + value.hexdigest()


def fetch(url, path):
    temporary = path.with_suffix(".partial")
    subprocess.run(["curl", "-fsSL", "--proto", "=https", "--proto-redir", "=https",
                    "--retry", "2", "--max-time", "900", url, "-o", str(temporary)], check=True)
    temporary.replace(path)


def main():
    CACHE.mkdir(parents=True, exist_ok=True)
    manifest = CACHE / "manifest.json"
    fetch(REGISTRY + "/manifests/300m", manifest)
    data = json.loads(manifest.read_text())
    layers = data["layers"] + [data["config"]]
    if not any(item["digest"] == MODEL_DIGEST for item in layers):
        raise RuntimeError("Upstream model changed; review the new digest before installing")
    for item in layers:
        expected = item["digest"]
        if len(expected) != 71 or not expected.startswith("sha256:") or any(c not in "0123456789abcdef" for c in expected[7:]):
            raise RuntimeError("Invalid registry digest")
        path = CACHE / expected.replace(":", "-")
        if not path.exists() or path.stat().st_size != item["size"] or digest(path) != expected:
            fetch(REGISTRY + "/blobs/" + expected, path)
        if path.stat().st_size != item["size"] or digest(path) != expected:
            raise RuntimeError("Embedding blob integrity check failed")
        subprocess.run(["docker", "exec", "research-embedding", "mkdir", "-p", "/root/.ollama/models/blobs"], check=True)
        subprocess.run(["docker", "cp", str(path), "research-embedding:/root/.ollama/models/blobs/" + path.name], check=True)
        print(json.dumps({"verified_blob": expected, "bytes": item["size"]}), flush=True)
    destination = "/root/.ollama/models/manifests/registry.ollama.ai/library/embeddinggemma"
    subprocess.run(["docker", "exec", "research-embedding", "mkdir", "-p", destination], check=True)
    subprocess.run(["docker", "cp", str(manifest), "research-embedding:" + destination + "/300m"], check=True)
    print(json.dumps({"model": "embeddinggemma:300m", "verified": True}))


if __name__ == "__main__":
    main()
