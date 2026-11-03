#!/usr/bin/env bash
set -euo pipefail
umask 022
cd "$(dirname "$0")/.."
version=${1:-}
arch=${2:-}
case "$arch" in amd64|arm64|riscv64) ;; *) echo 'Architecture must be amd64, arm64 or riscv64' >&2; exit 1 ;; esac
bash scripts/validate-release.sh "$version"
mkdir -p tmp/dist tmp/build tmp/go-cache tmp/test-tmp
export TMPDIR="$PWD/tmp/test-tmp" GOTMPDIR="$PWD/tmp/build"
export GOCACHE="${GOCACHE:-$PWD/tmp/go-cache}"
export GOTOOLCHAIN=local
# Pin the baseline rather than inherit a newer developer CPU profile.
export GORISCV64=rva20u64
name="filegate_${version}_linux_${arch}"
archive="$PWD/tmp/dist/$name.tar.gz"
if [[ -e "$archive" ]]; then
    echo "Refusing to overwrite release outputs for $name" >&2
    exit 1
fi
stage=$(mktemp -d "$PWD/tmp/package.XXXXXX")
trap 'rm -rf "$stage"' EXIT
epoch=$(git show -s --format=%ct HEAD)
built=$(date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ)
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
    -ldflags="-s -w -X main.version=$version -X main.buildTime=$built" \
    -o "$stage/filegate" ./cmd/filegate
# Archive exactly the executable; documentation and license stay in the source tree.
tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
    -C "$stage" -cf - filegate | gzip -n > "$stage/archive.tar.gz"
mv "$stage/archive.tar.gz" "$archive"
printf '%s\n' "$archive"
