#!/usr/bin/env bash
# Build hackme-fuzzing CLI for linux amd64 + windows amd64 into dist/.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VER="${DEPLOY_VERSION:-}"
if [[ -z "$VER" ]] && [[ -f web/site/assets/app.js ]]; then
  VER="$(grep -oE 'const RELEASE_VER = "[^"]+"' web/site/assets/app.js | sed -n 's/.*"\([^"]*\)".*/\1/p')"
fi
VER="${VER:-dev}"
mkdir -p dist
echo "[build-fuzzing-client] version=$VER"
export CGO_ENABLED=0
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "dist/hackme-fuzzing-${VER}-linux-amd64" ./cmd/fuzzingclient/
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "dist/hackme-fuzzing-${VER}-windows-amd64.exe" ./cmd/fuzzingclient/
chmod +x "dist/hackme-fuzzing-${VER}-linux-amd64"
REL="dist/release_${VER}"
if [[ -d "$REL" ]]; then
  cp -f "dist/hackme-fuzzing-${VER}-linux-amd64" "$REL/"
  cp -f "dist/hackme-fuzzing-${VER}-windows-amd64.exe" "$REL/"
  echo "[build-fuzzing-client] copied into $REL"
fi
ls -la dist/hackme-fuzzing-"${VER}"-*
