#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
version=${1:-}
arch=${2:-}
case "$arch" in amd64|arm64|riscv64) ;; *) exit 1 ;; esac
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || exit 1
runner=()
case "${3:-native}" in
    native) test "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" = "linux/$arch" ;;
    qemu)
        test "$arch" = riscv64
        test "$(go env GOHOSTOS)" = linux
        runner=("${QEMU_RISCV64:-qemu-riscv64}")
        command -v "${runner[0]}" > /dev/null
        ;;
    *) echo 'Smoke mode must be native or qemu' >&2; exit 1 ;;
esac
test "$(id -u)" -ne 0
command -v node > /dev/null
printf 'Release smoke: linux/%s mode=%s\n' "$arch" "${3:-native}"
mkdir -p tmp
stage=$(mktemp -d "${FILEGATE_RELEASE_TMP_ROOT:-$PWD/tmp}/rs.XXXXXX")
trap 'rm -rf "$stage"' EXIT
archive="tmp/dist/filegate_${version}_linux_${arch}.tar.gz"
test "$(tar -tzf "$archive")" = filegate
tar -xzf "$archive" -C "$stage" --no-same-owner
binary="$stage/filegate"
output=$("${runner[@]}" "$binary" --version)
grep -Fx "filegate $version" <<< "$output"
grep -Fx 'modified: false' <<< "$output"
grep -Fx "platform: linux/$arch" <<< "$output"
go version -m "$binary" | grep -F 'CGO_ENABLED=0'
if [[ "$arch" == riscv64 ]]; then
    go version -m "$binary" | grep -F 'GORISCV64=rva20u64'
fi
"${runner[@]}" "$binary" --help > /dev/null
"${runner[@]}" "$binary" --config "$stage/runtime/config.json" init
"${runner[@]}" "$binary" --config "$stage/runtime/config.json" check
test ! -e "$stage/runtime/state/state.json"
test ! -e "$stage/runtime/logs/filegate.log"

node tests/release-runtime.cjs "$binary" "$stage/runtime/config.json" "${runner[@]}"
