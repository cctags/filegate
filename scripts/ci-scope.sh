#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p tmp
changes=$(mktemp "$PWD/tmp/ci-changes.XXXXXX")
trap 'rm -f "$changes"' EXIT
base=${1:-}
if [[ -n "$base" && ! "$base" =~ ^[0-9a-f]{40}$ ]]; then
    echo 'Invalid CI base commit' >&2
    exit 1
fi
if [[ -z "$base" || "$base" =~ ^0+$ ]] || ! git cat-file -e "$base^{commit}" 2>/dev/null; then
    git ls-files -z > "$changes"
else
    git diff --check "$base" HEAD
    git diff --name-only -z "$base" HEAD > "$changes"
fi
basic=false
process=false
browser=false
while IFS= read -r -d '' file; do
    case "$file" in *.md|LICENSE) continue ;; esac
    basic=true
    case "$file" in
        cmd/*|internal/*|tests/*|scripts/*|.github/*|Makefile|go.mod) process=true ;;
    esac
    case "$file" in
        internal/gate/*|cmd/filegate/main.go|tests/*.cjs|scripts/*|.github/*|Makefile|go.mod) browser=true ;;
    esac
done < "$changes"
printf 'basic=%s\nprocess=%s\nbrowser=%s\n' "$basic" "$process" "$browser"
