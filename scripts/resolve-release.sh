#!/usr/bin/env bash
# Resolve only an existing version tag; never create tags or fall back to a branch.
set -euo pipefail
cd "$(dirname "$0")/.."
version=${1:-}
if [[ ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
    echo 'Release version must be vMAJOR.MINOR.PATCH, optionally with a prerelease suffix' >&2
    exit 1
fi
git rev-parse --verify "refs/tags/$version^{commit}"
