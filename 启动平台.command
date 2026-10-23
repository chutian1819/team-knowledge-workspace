#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
python3 scripts/researchctl.py up --build
open 'http://localhost:8090/platform/research'
