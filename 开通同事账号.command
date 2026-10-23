#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
python3 scripts/research-trial-account.py
read -r -p "记下登录信息后，按回车关闭窗口。" _research_reply
