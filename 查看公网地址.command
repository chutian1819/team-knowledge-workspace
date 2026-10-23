#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
python3 scripts/research-chmlfrp-tunnel.py status
read -r -p "按回车关闭窗口。" _research_reply
