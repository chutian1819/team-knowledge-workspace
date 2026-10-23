#!/usr/bin/env python3
"""Exercise first-run defaults and real chat using an isolated synthetic account.

Credentials stay in a mode-0600 ignored local file. Reports contain only this
account's synthetic fixtures, model IDs and test results, never credentials.
"""
import json
import secrets
import struct
import time
import urllib.request
import uuid
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "artifacts/research/trial-audit/closure"
PRIVATE = ROOT / ".research-private/trial-closure-account.json"
BASE = "https://team-aiorigin.owo.lat/api/v1"
HEADERS = {}


def call(method, path, body=None, base=BASE):
    headers = dict(HEADERS)
    if body is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(base + path, method=method, headers=headers,
        data=json.dumps(body, ensure_ascii=False).encode() if body is not None else None)
    with urllib.request.urlopen(request, timeout=180) as response:
        payload = json.load(response)
    return payload.get("data", payload)


def two_color_png():
    def chunk(kind, data):
        return struct.pack("!I", len(data)) + kind + data + struct.pack("!I", zlib.crc32(kind + data))
    width, height = 320, 160
    rows = []
    for y in range(height):
        row = bytearray(b"\0")
        for x in range(width):
            color = (255, 255, 255)
            if 20 <= y < 140:
                if 20 <= x < 140:
                    color = (230, 20, 20)
                elif 180 <= x < 300:
                    color = (20, 40, 230)
            row.extend(color)
        rows.append(bytes(row))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack("!2I5B", width, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(b"".join(rows))) + chunk(b"IEND", b"")


def chat(title, query, image=False):
    session = call("POST", "/sessions", {"title": "验收 · " + title + " · 合成"})
    body = {"query": query, "agent_id": "builtin-quick-answer", "web_search_enabled": False, "channel": "web"}
    if image:
        # Exercise the current UI's multipart upload and attachment_ids path.
        boundary = "trial" + uuid.uuid4().hex
        content = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"agent_id\"\r\n\r\nbuiltin-quick-answer\r\n"
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"synthetic-colors.png\"\r\nContent-Type: image/png\r\n\r\n").encode()
        content += two_color_png() + f"\r\n--{boundary}--\r\n".encode()
        request = urllib.request.Request(BASE + "/sessions/" + session["id"] + "/attachments", method="POST", data=content,
            headers={**HEADERS, "Content-Type": "multipart/form-data; boundary=" + boundary})
        with urllib.request.urlopen(request, timeout=60) as response:
            attachment = json.load(response)["data"]
        body["attachment_ids"] = [attachment["id"]]
    request = urllib.request.Request(BASE + "/knowledge-chat/" + session["id"], method="POST",
        data=json.dumps(body, ensure_ascii=False).encode(), headers={**HEADERS, "Content-Type": "application/json"})
    events = []
    with urllib.request.urlopen(request, timeout=180) as response:
        for line in response:
            if not line.startswith(b"data:"):
                continue
            event = json.loads(line[5:])
            events.append(event)
            if event.get("response_type") in ("error", "complete"):
                break
    (OUT / (title + "-events.json")).write_text(json.dumps(events, ensure_ascii=False, indent=2))
    errors = [event.get("content") for event in events if event.get("response_type") == "error"]
    assert not errors, errors
    messages = call("GET", "/messages/" + session["id"] + "/load?limit=10")
    answer = next(item for item in messages if item["role"] == "assistant")
    assert answer["is_completed"] and answer["content"].strip(), "chat answer was not persisted"
    return {"session_id": session["id"], "answer": answer["content"], "completed": True}


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    PRIVATE.parent.mkdir(parents=True, exist_ok=True)
    if not PRIVATE.exists():
        account = {"email": "trial-closure-" + uuid.uuid4().hex[:8] + "@example.invalid",
            "username": "验收 · 新同事闭环 · 合成", "password": "Qa2!" + secrets.token_urlsafe(18)}
        PRIVATE.touch(mode=0o600)
        PRIVATE.write_text(json.dumps(account))
    account = json.loads(PRIVATE.read_text())
    if not account.get("registered"):
        if len(account["password"]) > 32:
            account["password"] = "Qa2!" + secrets.token_urlsafe(18)
        call("POST", "/auth/register", {k: account[k] for k in ("username", "email", "password")},
            base="http://127.0.0.1:8090/api/v1")
        account["registered"] = True
        PRIVATE.write_text(json.dumps(account))
    auth = call("POST", "/auth/login", {k: account[k] for k in ("email", "password")})
    HEADERS["Authorization"] = "Bearer " + auth["token"]
    tenant = auth["active_tenant"]
    HEADERS["X-Tenant-ID"] = str(tenant.get("id") or tenant.get("tenant_id"))
    report = {"public_https": True, "synthetic_account": True, "checks": []}

    def check(name, action):
        try:
            evidence = action()
            report["checks"].append({"name": name, "passed": True, "evidence": evidence})
        except Exception as error:
            report["checks"].append({"name": name, "passed": False,
                "error": str(error) if isinstance(error, AssertionError) else type(error).__name__})
        (OUT / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({"check": name, "passed": report["checks"][-1]["passed"]}, ensure_ascii=False), flush=True)

    def text_chat():
        answer = chat("文字对话", "请只回复：对话链路正常")
        assert "对话链路正常" in answer["answer"]
        return answer

    def image_chat():
        answer = chat("图片对话", "图片中左侧和右侧的方块分别是什么颜色？请简短回答。", True)
        assert "红" in answer["answer"] and "蓝" in answer["answer"]
        return answer

    def inbox():
        initial = call("GET", "/research/capture/inbox")
        kb = call("POST", "/research/capture/inbox", {})
        assert kb["embedding_model_id"] and kb["summary_model_id"]
        assert kb["vlm_config"]["enabled"] and kb["vlm_config"]["model_id"]
        call("PUT", "/knowledge-bases/" + kb["id"], {"name": "验收 · 改名后的收件箱 · 合成"})
        found = call("GET", "/research/capture/inbox")
        repeated = call("POST", "/research/capture/inbox", {})
        assert found["id"] == repeated["id"] == kb["id"]
        document = call("POST", "/knowledge-bases/" + kb["id"] + "/knowledge/manual", {
            "title": "验收 · 默认模型索引 · 合成", "status": "publish",
            "content": "# 合成验收资料\n\n本流程的识别码为 TRIAL-CLOSURE-7319。负责人为验收账号。该资料仅供测试。"})
        for _ in range(100):
            doc = call("GET", "/knowledge/" + document["id"])
            if doc.get("parse_status") in ("completed", "failed"):
                break
            time.sleep(2)
        assert doc.get("parse_status") == "completed", doc.get("parse_status")
        return {"initially_empty": initial is None, "inbox_id": kb["id"], "rename_binding_preserved": True,
            "embedding": kb["embedding_model_id"], "chat": kb["summary_model_id"], "vision": kb["vlm_config"]["model_id"],
            "document_id": document["id"], "parse_status": doc["parse_status"]}

    check("new_account_text_chat", text_chat)
    check("new_account_image_attachment_chat", image_chat)
    check("inbox_defaults_rename_and_indexing", inbox)
    raise SystemExit(0 if all(item["passed"] for item in report["checks"]) else 1)


if __name__ == "__main__":
    main()
