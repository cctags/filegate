#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
version=${1:-}
if [[ ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
    echo 'Release version must be vMAJOR.MINOR.PATCH, optionally with a prerelease suffix' >&2
    exit 1
fi
if [[ -n $(git status --porcelain --untracked-files=normal) ]]; then
    echo 'Release packaging requires a clean working tree' >&2
    exit 1
fi
if [[ $(git rev-parse "refs/tags/$version^{commit}") != "$(git rev-parse HEAD)" ]]; then
    echo 'Release tag must point to HEAD' >&2
    exit 1
fi
awk -v prefix="## [$version] - " '
    index($0, prefix) == 1 { found = 1 }
    END { exit !found }
' CHANGELOG.md || { echo 'Add a dated CHANGELOG section for this version before tagging' >&2; exit 1; }
