#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/validate-release.sh "${1:-}"
awk -v prefix="## [$1] - " '
    /^## / { if (active) exit; if (index($0, prefix) == 1) active = 1 }
    active { print }
' CHANGELOG.md
