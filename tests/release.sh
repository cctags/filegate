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
export FILEGATE_RELEASE_TMP_ROOT="$root/tmp"
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
# Dispatch resolves the requested tag even when the workflow branch is newer.
release_commit=$(git rev-parse HEAD)
test "$(bash scripts/resolve-release.sh v9.8.7)" = "$release_commit"
git tag v9.8.7-rc.1
test "$(bash scripts/resolve-release.sh v9.8.7-rc.1)" = "$release_commit"
for invalid in '' ../bad v0.1 --help 'v9.8.7;echo bad' v09.8.7; do
    reject bash scripts/resolve-release.sh "$invalid"
done
git branch v9.8.5
reject bash scripts/resolve-release.sh v9.8.5
reject bash scripts/resolve-release.sh v9.8.4
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
test "$(find tmp/dist -maxdepth 1 -type f -printf '%f\n')" = "$(basename "$archive")"
reject bash scripts/package-release.sh v9.8.7 "$(go env GOHOSTARCH)"
# Cross-compile every other target without claiming native execution.
for other in amd64 arm64 riscv64; do
    if [[ $(go env GOHOSTARCH) == "$other" ]]; then continue; fi
    GORISCV64=rva23u64 bash scripts/package-release.sh v9.8.7 "$other"
    mkdir -p "tmp/cross/$other"
    tar -xzf "tmp/dist/filegate_v9.8.7_linux_${other}.tar.gz" -C "tmp/cross/$other"
    test "$(tar -tzf "tmp/dist/filegate_v9.8.7_linux_${other}.tar.gz")" = filegate
    go version -m "tmp/cross/$other/filegate" | grep -F "GOARCH=$other"
done
mkdir -p tmp/riscv
tar -xzf tmp/dist/filegate_v9.8.7_linux_riscv64.tar.gz -C tmp/riscv
go version -m tmp/riscv/filegate | grep -F 'GORISCV64=rva20u64'
test "$(find tmp/dist -maxdepth 1 -type f | wc -l)" -eq 3
test -z "$(find tmp/dist -maxdepth 1 -type f ! -name '*.tar.gz' -print)"
reject bash scripts/package-release.sh v9.8.7 riscv32
reject bash scripts/smoke-release.sh v9.8.7 amd64 qemu
reject bash scripts/smoke-release.sh v9.8.7 riscv64 invalid
reject env QEMU_RISCV64=false bash scripts/smoke-release.sh v9.8.7 riscv64 qemu
reject env QEMU_RISCV64="$root/tmp/missing-qemu" bash scripts/smoke-release.sh v9.8.7 riscv64 qemu
if [[ ${FILEGATE_RELEASE_QEMU:-0} == 1 ]]; then
    bash scripts/smoke-release.sh v9.8.7 riscv64 qemu
else
    echo 'RISC-V cross compilation passed; optional QEMU runtime not requested.'
fi
# ci-scope must distinguish documentation-only and application changes.
base=$(git rev-parse HEAD)
printf '\nDocumentation edit.\n' >> README.md
git add README.md
git commit -qm 'docs: Exercise scope selection'
test "$(bash scripts/resolve-release.sh v9.8.7)" = "$release_commit"
test "$(git rev-parse HEAD)" != "$release_commit"
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
