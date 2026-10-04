#!/bin/sh
set -eu

export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=/dev/null

cd "$(dirname "$0")/.."
base="$(pwd)/.demo/showcase"
if [ -e "$base" ]; then
  echo "$base already exists; leaving it unchanged."
  exit 0
fi
mkdir -p .demo
mkdir "$base"

source_repo="$base/root"
staging_repo="$base/staging.git"
releases_repo="$base/releases.git"
backup_repo="$base/backup.git"

git init -q -b main "$source_repo"
commit() {
  git -C "$source_repo" -c user.name=Demo -c user.email=demo@example.com \
    -c commit.gpgsign=false commit -q --allow-empty -m "$1"
}

commit 'Start empty repository'
git -C "$source_repo" tag v0.9.0
commit 'Prepare first release'
git -C "$source_repo" tag v1.0.0
git -C "$source_repo" branch release/1.0
commit 'Add search API'
git -C "$source_repo" tag v1.1.0
git -C "$source_repo" branch develop
git -C "$source_repo" branch feature/search
commit 'Prepare dashboard preview'
git -C "$source_repo" tag v1.2.0-rc1
git -C "$source_repo" branch feature/dashboard
git -C "$source_repo" branch chore/ci
commit 'Publish dashboard'
git -C "$source_repo" tag v1.2.0
git -C "$source_repo" branch release/1.2
git -C "$source_repo" branch hotfix/login
commit 'Try streaming API'
git -C "$source_repo" tag v2.0.0-rc1
git -C "$source_repo" branch experiment/streaming
commit 'Publish streaming API'
git -C "$source_repo" tag v2.0.0
git -C "$source_repo" branch feature/api

git init -q --bare "$staging_repo"
git init -q --bare "$releases_repo"
git init -q --bare "$backup_repo"

git -C "$source_repo" push -q "$staging_repo" \
  v1.0.0:refs/heads/main \
  release/1.0:refs/heads/release/1.0 \
  v0.9.0:refs/heads/legacy \
  refs/tags/v0.9.0 refs/tags/v1.0.0
git -C "$source_repo" push -q "$releases_repo" \
  refs/tags/v0.9.0 refs/tags/v1.0.0 refs/tags/v1.1.0
git -C "$source_repo" push -q --all "$backup_repo"
git -C "$source_repo" push -q --tags "$backup_repo"

cat > "$source_repo/.mirr.yaml" <<'YAML'
version: 1
repositories:
  showcase:
    source:
      url: .
    defaults:
      mode: continuous
    mirrors:
      staging:
        url: ../staging.git
      releases:
        url: ../releases.git
        mode: release
      backup:
        url: ../backup.git
YAML

printf '%s\n' 'Showcase ready: cd .demo/showcase/root && mirr'
