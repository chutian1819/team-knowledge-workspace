#!/usr/bin/env python3
"""Create disposable synthetic QA accounts on loopback, without printing secrets."""
import argparse
import json
import os
from pathlib import Path
import secrets
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://localhost:8090")
    args = parser.parse_args()
    base = args.base_url.rstrip("/")
    if urllib.parse.urlsplit(base).hostname not in ("localhost", "127.0.0.1", "::1"):
        parser.error("QA account creation is restricted to a local test instance")
    path = ROOT / ".research-state.json"
    state = json.loads(path.read_text()) if path.exists() else {"base_url": base, "actors": {}}
    if state["base_url"] != base:
        raise ValueError("existing QA state points at another instance; use its original URL")

    def call(endpoint, body):
        request = urllib.request.Request(base + "/api/v1/auth/" + endpoint,
            data=json.dumps(body, ensure_ascii=False).encode(), method="POST",
            headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            return error.code, {}

    for actor, name in (("owner", "验收负责人"), ("colleague", "验收同事"), ("ui", "产品界面验收")):
        record = state["actors"].get(actor) or {
            "email": "research-qa-" + actor + "@example.invalid",
            "password": secrets.token_urlsafe(30), "username": name}
        if not record["email"].endswith("@example.invalid"):
            raise ValueError("QA scripts must use synthetic accounts")
        body = {key: record[key] for key in ("email", "password")}
        code, payload = call("login", body)
        if code == 401:
            code, _ = call("register", {**body, "username": record.get("username", name)})
            if code not in (200, 201):
                raise RuntimeError("local QA registration failed with HTTP " + str(code))
            code, payload = call("login", body)
        if code != 200:
            raise RuntimeError("local QA login failed with HTTP " + str(code))
        previous_user = record.get("user", {}).get("id")
        if actor == "owner" and previous_user and previous_user != payload["user"]["id"]:
            state.pop("qa", None)
        record.update({key: payload[key] for key in ("user", "active_tenant", "memberships", "token", "refresh_token")})
        state["actors"][actor] = record
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as handle:
        json.dump(state, handle, ensure_ascii=False, indent=2)
    path.chmod(0o600)
    print("本机合成验收账号已就绪；凭据仅保存在忽略文件 .research-state.json。")


if __name__ == "__main__":
    main()
