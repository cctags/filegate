#!/usr/bin/env bash
set -euo pipefail
umask 022
cd "$(dirname "$0")/.."
version=${1:-}
arch=${2:-}
case "$arch" in amd64|arm64) ;; *) echo 'Architecture must be amd64 or arm64' >&2; exit 1 ;; esac
bash scripts/validate-release.sh "$version"
mkdir -p tmp/dist tmp/build tmp/go-cache tmp/test-tmp
export TMPDIR="$PWD/tmp/test-tmp" GOTMPDIR="$PWD/tmp/build"
export GOCACHE="${GOCACHE:-$PWD/tmp/go-cache}"
export GOTOOLCHAIN=local
name="filegate_${version}_linux_${arch}"
archive="$PWD/tmp/dist/$name.tar.gz"
if [[ -e "$archive" ]]; then
    echo "Refusing to overwrite $archive" >&2
    exit 1
fi
stage=$(mktemp -d "$PWD/tmp/package.XXXXXX")
trap 'rm -rf "$stage"' EXIT
package="$stage/$name"
mkdir -p "$package/deploy" "$package/docs" "$package/tests"
commit=$(git rev-parse HEAD)
epoch=$(git show -s --format=%ct HEAD)
built=$(date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ)
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
    -ldflags="-s -w -X main.version=$version -X main.buildTime=$built" \
    -o "$package/filegate" ./cmd/filegate
# An explicit allowlist prevents runtime credentials and arbitrary tmp files
# from entering a release. init generates a new commented config after install.
for file in README.md LICENSE CHANGELOG.md SECURITY.md SECURITY_REVIEW.md TEST_REVIEW.md \
    deploy/README.md deploy/filegate.service docs/PROJECT_REVIEW.md docs/RELEASING.md docs/OPERATIONS.md tests/README.md; do
    [[ -f "$file" && ! -L "$file" ]] || { echo "Missing regular release input: $file" >&2; exit 1; }
    install -m 0644 "$file" "$package/$file"
done
printf 'version: %s\ncommit: %s\nbuilt: %s\ngo: %s\nplatform: linux/%s\nCGO_ENABLED: 0\n' \
    "$version" "$commit" "$built" "$(go env GOVERSION)" "$arch" > "$package/BUILDINFO"
tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
    -C "$stage" -cf - "$name" | gzip -n > "$stage/archive.tar.gz"
mv "$stage/archive.tar.gz" "$archive"
printf '%s\n' "$archive"
