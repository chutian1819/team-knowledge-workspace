#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
docker compose -f compose.research.yaml -f compose.research.public.yaml -f compose.research.chmlfrp.yaml up -d --no-deps public-gateway public-tls
python3 scripts/research-chmlfrp-tunnel.py start
read -r -p "按回车关闭窗口。" _research_reply
