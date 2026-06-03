#!/usr/bin/env bash
# Shared desktop node build: repo-root cwd, embedded dashboard, git commit in UI.
# shellcheck disable=SC2034
# Usage: source scripts/ops/desktop_node_build.sh
#        desktop_node_build "$ROOT/bin/hackme-node-desktop" [go build tags]

desktop_node_build_ldflags() {
  local commit date ver
  ver="$(grep -oE 'RELEASE_VER = "[^"]+"' "${ROOT:?ROOT unset}/web/site/assets/app.js" 2>/dev/null | sed 's/.*"\([^"]*\)".*/\1/' || true)"
  [[ -z "$ver" ]] && ver="dev"
  commit="$(git -C "${ROOT:?ROOT unset}" rev-parse --short HEAD 2>/dev/null || echo nogit)"
  date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '%s' "-X main.Version=${ver} -X main.Commit=${commit} -X main.BuildDate=${date}"
}

desktop_node_needs_rebuild() {
  local bin="${1:?binary path}"
  [[ ! -x "$bin" ]] && return 0
  [[ "${HACKME_DESKTOP_FORCE_REBUILD:-0}" == "1" ]] && return 0
  [[ "${HACKME_DESKTOP_ALWAYS_REBUILD:-0}" == "1" ]] && return 0
  [[ -f "$ROOT/dashboard.html" && "$ROOT/dashboard.html" -nt "$bin" ]] && return 0
  find "$ROOT" \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) -newer "$bin" -print -quit 2>/dev/null | grep -q .
}

desktop_node_build() {
  local out="${1:?output binary}"
  local tags="${2:-}"
  mkdir -p "$(dirname "$out")"
  local -a args=(go build -trimpath -ldflags "$(desktop_node_build_ldflags)" -o "$out" .)
  if [[ -n "$tags" ]]; then
    args=(go build -trimpath -tags "$tags" -ldflags "$(desktop_node_build_ldflags)" -o "$out" .)
  fi
  echo "[desktop-build] ${args[*]}"
  (cd "$ROOT" && "${args[@]}")
  chmod 755 "$out"
  go build -trimpath -o "$ROOT/bin/minersign" ./cmd/minersign
  chmod 755 "$ROOT/bin/minersign"
  cp -f "$ROOT/bin/minersign" "$ROOT/minersign" 2>/dev/null || true
}

desktop_node_export_runtime() {
  export HACKME_WORKING_DIR="${HACKME_WORKING_DIR:-$ROOT}"
  export HACKME_REPO_ROOT="${HACKME_REPO_ROOT:-$ROOT}"
  export HACKME_DESKTOP_MODE="${HACKME_DESKTOP_MODE:-1}"
}
