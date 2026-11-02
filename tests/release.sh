#!/usr/bin/env bash
# Test release guards and archive isolation in a disposable Git repository.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p tmp
fixture=$(mktemp -d "$PWD/tmp/release-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT
for file in cmd internal scripts deploy docs tests Makefile go.mod .gitignore \
    README.md LICENSE CHANGELOG.md SECURITY.md SECURITY_REVIEW.md TEST_REVIEW.md; do
    cp -R "$file" "$fixture/"
done
cd "$fixture"
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
git init -q
git config user.name 'Filegate release tests'
git config user.email 'release-tests@example.invalid'
git config commit.gpgsign false
git config tag.gpgsign false
git config core.hooksPath /dev/null
printf '\n## [v9.8.7] - 2000-01-01\n\nFixture release notes.\n' >> CHANGELOG.md
git add .
git commit -qm 'test: Create isolated packaging fixture'
git tag -a v9.8.7 -m 'Isolated release fixture'
bash scripts/validate-release.sh v9.8.7
bash scripts/release-notes.sh v9.8.7 | grep -Fx 'Fixture release notes.'
reject() {
    if "$@" > /dev/null 2>&1; then echo "Unexpected success: $*" >&2; exit 1; fi
}
reject bash scripts/validate-release.sh v0.1
reject bash scripts/validate-release.sh '../bad'
git tag v9.8.6
reject bash scripts/validate-release.sh v9.8.6
printf '\nDirty fixture.\n' >> README.md
reject bash scripts/validate-release.sh v9.8.7
git restore README.md
# Runtime decoys are ignored by Git but must never enter an archive.
mkdir -p tmp/demo state content logs
printf 'fixture secret' > state/state.json
printf 'fixture secret' > tmp/demo/config.json
printf 'fixture secret' > content/private.txt
printf 'fixture secret' > logs/filegate.log
bash scripts/package-release.sh v9.8.7 "$(go env GOHOSTARCH)"
bash scripts/smoke-release.sh v9.8.7 "$(go env GOHOSTARCH)"
archive="tmp/dist/filegate_v9.8.7_linux_$(go env GOHOSTARCH).tar.gz"
tar -tzf "$archive" > tmp/archive-list.txt
if [[ $(cat tmp/archive-list.txt) != filegate ]]; then
    echo 'Release archive must contain only the executable at its root' >&2
    exit 1
fi
buildinfo="tmp/dist/filegate_v9.8.7_linux_$(go env GOHOSTARCH).BUILDINFO"
grep -Fx 'version: v9.8.7' "$buildinfo"
grep -Fx "commit: $(git rev-parse HEAD)" "$buildinfo"
reject bash scripts/package-release.sh v9.8.7 "$(go env GOHOSTARCH)"
mv "$archive" tmp/native-archive.tar.gz
reject bash scripts/package-release.sh v9.8.7 "$(go env GOHOSTARCH)"
mv tmp/native-archive.tar.gz "$archive"
# Compile the other supported architecture without claiming it was executed.
other=arm64
if [[ $(go env GOHOSTARCH) == arm64 ]]; then other=amd64; fi
bash scripts/package-release.sh v9.8.7 "$other"
mkdir tmp/cross
tar -xzf "tmp/dist/filegate_v9.8.7_linux_${other}.tar.gz" -C tmp/cross
test "$(tar -tzf "tmp/dist/filegate_v9.8.7_linux_${other}.tar.gz")" = filegate
go version -m tmp/cross/filegate | grep -F "GOARCH=$other"
grep -Fx "platform: linux/$other" "tmp/dist/filegate_v9.8.7_linux_${other}.BUILDINFO"
# ci-scope must distinguish documentation-only and application changes.
base=$(git rev-parse HEAD)
printf '\nDocumentation edit.\n' >> README.md
git add README.md
git commit -qm 'docs: Exercise scope selection'
scope=$(bash scripts/ci-scope.sh "$base")
grep -Fx 'basic=false' <<< "$scope"
printf '\n// Scope fixture.\n' >> internal/gate/config.go
git add internal/gate/config.go
git commit -qm 'test: Exercise application scope'
scope=$(bash scripts/ci-scope.sh "$base")
for expected in basic=true process=true browser=true; do grep -Fx "$expected" <<< "$scope"; done
scope=$(bash scripts/ci-scope.sh 0000000000000000000000000000000000000000)
grep -Fx 'basic=true' <<< "$scope"
reject bash scripts/validate-release.sh v9.8.7
echo 'Release packaging and CI scope regressions passed.'
