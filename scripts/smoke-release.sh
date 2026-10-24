#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
version=${1:-}
arch=${2:-}
case "$arch" in amd64|arm64) ;; *) exit 1 ;; esac
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || exit 1
test "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" = "linux/$arch"
mkdir -p tmp
stage=$(mktemp -d "$PWD/tmp/release-smoke.XXXXXX")
trap 'rm -rf "$stage"' EXIT
tar -xzf "tmp/dist/filegate_${version}_linux_${arch}.tar.gz" -C "$stage" --no-same-owner
binary="$stage/filegate_${version}_linux_${arch}/filegate"
output=$("$binary" --version)
grep -Fx "filegate $version" <<< "$output"
grep -Fx 'modified: false' <<< "$output"
grep -Fx "platform: linux/$arch" <<< "$output"
go version -m "$binary" | grep -F 'CGO_ENABLED=0'
"$binary" --help > /dev/null
"$binary" --config "$stage/runtime/config.json" init
"$binary" --config "$stage/runtime/config.json" check
test ! -e "$stage/runtime/state/state.json"
test ! -e "$stage/runtime/logs/filegate.log"
