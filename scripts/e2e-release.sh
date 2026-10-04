#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT HUP INT TERM
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null XDG_CACHE_HOME="$workdir/cache"
export GIT_AUTHOR_NAME=E2E GIT_AUTHOR_EMAIL=e2e@example.invalid
export GIT_COMMITTER_NAME=E2E GIT_COMMITTER_EMAIL=e2e@example.invalid

source="$workdir/source"
mirror="$workdir/mirror.git"
config="$workdir/mirr.yaml"
git init -q -b main "$source"
git init -q --bare "$mirror"
git -C "$source" commit -q --allow-empty -m 'First release'
git -C "$source" tag -a v1.0.0 -m 'First release'
git -C "$source" tag v1.1.0-rc1
git -C "$source" tag internal

cat > "$config" <<YAML
version: 1
repositories:
  release_e2e:
    source:
      url: $source
    mirrors:
      target:
        url: $mirror
        mode: release
YAML

./bin/mirr -c "$config" validate >/dev/null
plan=$(./bin/mirr -c "$config" plan release_e2e)
printf '%s\n' "$plan" | grep -q 'refs/tags/v1.0.0'
if printf '%s\n' "$plan" | grep -Eq 'refs/heads/main|refs/tags/v1.1.0-rc1|refs/tags/internal'; then
  echo 'release plan includes an excluded ref' >&2
  exit 1
fi

./bin/mirr -c "$config" sync release_e2e >/dev/null
original=$(git -C "$source" rev-parse refs/tags/v1.0.0)
test "$(git -C "$mirror" rev-parse refs/tags/v1.0.0)" = "$original"
test "$(git -C "$mirror" for-each-ref --format='%(refname)' | wc -l)" -eq 1
./bin/mirr -c "$config" sync release_e2e | grep -q 'current'

git -C "$source" commit -q --allow-empty -m 'Next release'
git -C "$source" tag -f v1.0.0 >/dev/null
git -C "$source" tag v1.1.0
if ./bin/mirr -c "$config" sync release_e2e >/dev/null 2>&1; then
  echo 'rewritten release tag was accepted without allow_force' >&2
  exit 1
fi
test "$(git -C "$mirror" rev-parse refs/tags/v1.0.0)" = "$original"
if git -C "$mirror" show-ref --verify --quiet refs/tags/v1.1.0; then
  echo 'conflicting release caused a partial push' >&2
  exit 1
fi

git -C "$source" update-ref refs/tags/v1.0.0 "$original"
./bin/mirr -c "$config" sync release_e2e >/dev/null
test "$(git -C "$mirror" rev-parse refs/tags/v1.1.0)" = "$(git -C "$source" rev-parse refs/tags/v1.1.0)"
if git -C "$mirror" show-ref --verify --quiet refs/heads/main; then
  echo 'release mode pushed a branch' >&2
  exit 1
fi
echo 'Release E2E passed.'
