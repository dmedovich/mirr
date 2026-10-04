#!/bin/sh
set -eu
export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=/dev/null
cd "$(dirname "$0")/.."
if [ -e .demo ]; then
  echo '.demo already exists; using the existing demo.'
  exit 0
fi
mkdir .demo
git init -q -b main .demo/source
git -C .demo/source -c user.name=Demo -c user.email=demo@example.com commit -q --allow-empty -m 'Initial commit'
git -C .demo/source tag v1.0.0
git -C .demo/source -c user.name=Demo -c user.email=demo@example.com commit -q --allow-empty -m 'Work after release'
git -C .demo/source tag v1.1.0-rc1
git init -q --bare .demo/releases.git
git init -q --bare .demo/continuous.git
cat > .demo/.mirr.yaml <<'YAML'
version: 1
repositories:
  demo:
    source:
      url: ./source
    mirrors:
      releases:
        url: ./releases.git
        mode: release
      backup:
        url: ./continuous.git
        mode: continuous
YAML
printf '%s\n' 'Demo ready: ./bin/mirr -c .demo/.mirr.yaml tui'
