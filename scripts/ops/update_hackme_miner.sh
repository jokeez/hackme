#!/usr/bin/env bash
# HackMe miner/node self-update (L1) — Linux desktop + /opt/hackme (+ HackMe OS O1).
#
# Reads latest.json, downloads linux tar.gz, verifies SHA256, replaces binaries,
# never touches .env / data / logs / wallet seeds. Keeps one previous/ backup.
#
# Usage:
#   bash scripts/ops/update_hackme_miner.sh
#   bash scripts/ops/update_hackme_miner.sh --install-dir /opt/hackme
#   bash scripts/ops/update_hackme_miner.sh --dry-run
#   HACKME_LATEST_URL=file:///tmp/latest.json bash scripts/ops/update_hackme_miner.sh --install-dir ./myinstall
#
# Env:
#   HACKME_LATEST_URL   default: https://hackme.tech/dist/latest.json
#                       fallbacks tried automatically (GitHub release latest.json)
#   HACKME_INSTALL_DIR  default: script's parent if it looks like an install, else /opt/hackme
#   HACKME_UPDATE_RESTART=1  stop/start via stop_hackme_miner.sh + start_hackme_miner.sh when present
set -euo pipefail

UPDATER_VERSION=1
log() { echo "[hackme-update $(date -u +%H:%M:%S)] $*"; }
die() { echo "[hackme-update] ERROR: $*" >&2; exit 1; }

DRY_RUN=0
INSTALL_DIR="${HACKME_INSTALL_DIR:-}"
FORCE=0
RESTART="${HACKME_UPDATE_RESTART:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir|-d) INSTALL_DIR="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --force) FORCE=1; shift ;;
    --restart) RESTART=1; shift ;;
    --help|-h)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *) die "unknown arg: $1" ;;
  esac
done

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}
require_cmd curl
require_cmd jq
require_cmd tar
require_cmd sha256sum
require_cmd mktemp

# Resolve install dir
if [[ -z "$INSTALL_DIR" ]]; then
  HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [[ -x "${HERE}/hackme" || -x "${HERE}/bin/hackme" ]]; then
    INSTALL_DIR="$HERE"
  elif [[ -x /opt/hackme/hackme ]]; then
    INSTALL_DIR=/opt/hackme
  else
    INSTALL_DIR="$HERE"
  fi
fi
INSTALL_DIR="$(cd "$INSTALL_DIR" 2>/dev/null && pwd)" || die "install-dir not found: $INSTALL_DIR"
log "install_dir=$INSTALL_DIR dry_run=$DRY_RUN"

# Refuse if apt/dpkg owns this prefix (L3 later) — optional soft warn
if command -v dpkg >/dev/null 2>&1; then
  if dpkg -S "${INSTALL_DIR}/hackme" >/dev/null 2>&1; then
    log "WARN: ${INSTALL_DIR}/hackme is owned by dpkg — prefer: sudo apt upgrade hackme-node (when apt repo is live)"
    [[ "$FORCE" == "1" ]] || die "refusing to fight dpkg (pass --force to override)"
  fi
fi

LATEST_URLS=()
if [[ -n "${HACKME_LATEST_URL:-}" ]]; then
  LATEST_URLS+=("$HACKME_LATEST_URL")
fi
LATEST_URLS+=(
  "https://hackme.tech/dist/latest.json"
  "https://github.com/jokeez/hackme/releases/latest/download/latest.json"
)

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
LATEST_FILE="${TMP}/latest.json"

fetch_ok=0
for u in "${LATEST_URLS[@]}"; do
  log "fetch latest.json ← $u"
  if [[ "$u" == file://* ]]; then
    cp "${u#file://}" "$LATEST_FILE" && fetch_ok=1 && break
  elif curl -fsSL --connect-timeout 15 --max-time 60 -o "$LATEST_FILE" "$u"; then
    fetch_ok=1
    break
  else
    log "miss: $u"
  fi
done
[[ "$fetch_ok" == "1" ]] || die "could not fetch latest.json from any URL"

schema="$(jq -r '.schema // empty' "$LATEST_FILE")"
[[ "$schema" == "hackme.release.latest.v1" ]] || die "unsupported latest.json schema: ${schema:-empty}"
min_up="$(jq -r '.min_updater // 1' "$LATEST_FILE")"
if [[ "$min_up" =~ ^[0-9]+$ ]] && [[ "$UPDATER_VERSION" -lt "$min_up" ]]; then
  die "updater protocol too old (local=$UPDATER_VERSION need>=$min_up) — re-download update_hackme_miner.sh from the release"
fi
remote_ver="$(jq -r '.version' "$LATEST_FILE")"
[[ -n "$remote_ver" && "$remote_ver" != null ]] || die "latest.json missing version"

local_ver=""
if [[ -x "${INSTALL_DIR}/hackme" ]]; then
  local_ver="$("${INSTALL_DIR}/hackme" --version 2>/dev/null | head -1 || true)"
  # tolerate "HackMe 0.1.0-rc15" or bare version
  local_ver="$(echo "$local_ver" | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+[^[:space:]]*' | head -1 || true)"
fi
if [[ -z "$local_ver" && -f "${INSTALL_DIR}/BUILD_INFO.txt" ]]; then
  local_ver="$(awk -F= '/^version=/{print $2; exit}' "${INSTALL_DIR}/BUILD_INFO.txt" | tr -d '\r')"
fi
log "local=${local_ver:-unknown} remote=$remote_ver"

if [[ -n "$local_ver" && "$local_ver" == "$remote_ver" && "$FORCE" != "1" ]]; then
  log "already up to date ($local_ver) — nothing to do"
  exit 0
fi

plat="$(jq -c '.platforms[] | select(.id=="linux")' "$LATEST_FILE" | head -1)"
[[ -n "$plat" ]] || die "latest.json has no linux platform"
file="$(jq -r '.file' <<<"$plat")"
sha="$(jq -r '.sha256' <<<"$plat")"
url="$(jq -r '.url' <<<"$plat")"
mirror="$(jq -r '.mirror_url // empty' <<<"$plat")"
[[ -n "$file" && -n "$sha" && -n "$url" ]] || die "linux platform incomplete"

ARCHIVE="${TMP}/${file}"
download_ok=0
for u in "$url" "$mirror"; do
  [[ -n "$u" && "$u" != null ]] || continue
  log "download $u"
  if [[ "$u" == file://* ]]; then
    cp "${u#file://}" "$ARCHIVE" && download_ok=1 && break
  elif curl -fL --connect-timeout 20 --max-time 600 -o "$ARCHIVE" "$u"; then
    download_ok=1
    break
  fi
  log "download miss: $u"
done
[[ "$download_ok" == "1" ]] || die "failed to download $file"

got="$(sha256sum "$ARCHIVE" | awk '{print $1}')"
[[ "$got" == "$sha" ]] || die "SHA256 mismatch (got $got want $sha)"
log "sha256 ok"

EXTRACT="${TMP}/extract"
mkdir -p "$EXTRACT"
tar -xzf "$ARCHIVE" -C "$EXTRACT"
PAYLOAD="$(find "$EXTRACT" -maxdepth 2 -type d -name linux | head -1)"
[[ -n "$PAYLOAD" && -f "${PAYLOAD}/hackme" ]] || die "linux/hackme not found in archive"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY-RUN would install binaries from $PAYLOAD → $INSTALL_DIR (preserve .env data logs)"
  ls -la "$PAYLOAD" | head -20
  exit 0
fi

# Optional stop
if [[ "$RESTART" == "1" && -x "${INSTALL_DIR}/stop_hackme_miner.sh" ]]; then
  log "stopping miner via stop_hackme_miner.sh"
  bash "${INSTALL_DIR}/stop_hackme_miner.sh" || true
fi

PREV="${INSTALL_DIR}/previous"
mkdir -p "$PREV"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP="${PREV}/${stamp}"
mkdir -p "$BACKUP"

# Binaries / layout to refresh (never .env / data / logs)
replace_list=(
  hackme
  minersign
  workerpoh
  workerfuzz
  workerpoh-opencl
  workerpoh-cuda
  fleetplan
  install_hackme.sh
  setup_hackme_miner.sh
  start_hackme_miner.sh
  stop_hackme_miner.sh
  update_hackme_miner.sh
  update_hackme_os_binaries.sh
  install_menu_entry.sh
  hackme.desktop.template
  hackme-dashboard.desktop.template
  RELEASE_QUICKSTART.md
  README.md
)

log "backing up → $BACKUP"
for name in "${replace_list[@]}"; do
  if [[ -e "${INSTALL_DIR}/${name}" ]]; then
    cp -a "${INSTALL_DIR}/${name}" "${BACKUP}/" 2>/dev/null || true
  fi
done
if [[ -d "${INSTALL_DIR}/bin" ]]; then
  mkdir -p "${BACKUP}/bin"
  cp -a "${INSTALL_DIR}/bin/." "${BACKUP}/bin/" 2>/dev/null || true
fi
if [[ -d "${INSTALL_DIR}/lib" ]]; then
  mkdir -p "${BACKUP}/lib"
  cp -a "${INSTALL_DIR}/lib/." "${BACKUP}/lib/" 2>/dev/null || true
fi

log "installing binaries"
install -m 0755 "${PAYLOAD}/hackme" "${INSTALL_DIR}/hackme"
for name in minersign workerpoh workerfuzz workerpoh-opencl workerpoh-cuda fleetplan; do
  if [[ -f "${PAYLOAD}/${name}" ]]; then
    install -m 0755 "${PAYLOAD}/${name}" "${INSTALL_DIR}/${name}"
  fi
done
if [[ -d "${PAYLOAD}/bin" ]]; then
  mkdir -p "${INSTALL_DIR}/bin"
  cp -a "${PAYLOAD}/bin/." "${INSTALL_DIR}/bin/"
  chmod -R a+rX "${INSTALL_DIR}/bin" || true
  find "${INSTALL_DIR}/bin" -type f -exec chmod a+x {} + 2>/dev/null || true
fi
if [[ -d "${PAYLOAD}/lib" ]]; then
  mkdir -p "${INSTALL_DIR}/lib"
  cp -a "${PAYLOAD}/lib/." "${INSTALL_DIR}/lib/"
fi
# Keep updater script current when shipped in payload
if [[ -f "${PAYLOAD}/update_hackme_miner.sh" ]]; then
  install -m 0755 "${PAYLOAD}/update_hackme_miner.sh" "${INSTALL_DIR}/update_hackme_miner.sh"
fi
for helper in install_hackme.sh setup_hackme_miner.sh start_hackme_miner.sh stop_hackme_miner.sh RELEASE_QUICKSTART.md; do
  if [[ -f "${PAYLOAD}/${helper}" ]]; then
    install -m 0644 "${PAYLOAD}/${helper}" "${INSTALL_DIR}/${helper}" 2>/dev/null || \
      install -m 0755 "${PAYLOAD}/${helper}" "${INSTALL_DIR}/${helper}"
  fi
done
[[ -f "${PAYLOAD}/start_hackme_miner.sh" ]] && chmod +x "${INSTALL_DIR}/start_hackme_miner.sh" || true
[[ -f "${PAYLOAD}/stop_hackme_miner.sh" ]] && chmod +x "${INSTALL_DIR}/stop_hackme_miner.sh" || true

if [[ -d "${PAYLOAD}/icons" ]]; then
  mkdir -p "${INSTALL_DIR}/icons"
  cp -a "${PAYLOAD}/icons/." "${INSTALL_DIR}/icons/"
fi
for helper in install_menu_entry.sh hackme.desktop.template hackme-dashboard.desktop.template; do
  if [[ -f "${PAYLOAD}/${helper}" ]]; then
    install -m 0755 "${PAYLOAD}/${helper}" "${INSTALL_DIR}/${helper}" 2>/dev/null || \
      install -m 0644 "${PAYLOAD}/${helper}" "${INSTALL_DIR}/${helper}"
  fi
done
[[ -f "${INSTALL_DIR}/install_menu_entry.sh" ]] && chmod +x "${INSTALL_DIR}/install_menu_entry.sh" || true
# Refresh branded menu entry when running as root (system install)
if [[ -x "${INSTALL_DIR}/install_menu_entry.sh" && "$(id -u)" == "0" ]]; then
  INSTALL_DIR="${INSTALL_DIR}" bash "${INSTALL_DIR}/install_menu_entry.sh" \
    --install-dir "${INSTALL_DIR}" --payload-dir "${INSTALL_DIR}" 2>/dev/null || true
fi

# Version stamp for next compare
{
  echo "version=${remote_ver}"
  echo "updated_utc=${stamp}"
  echo "source_sha256=${sha}"
} >"${INSTALL_DIR}/BUILD_INFO.txt"

# Preserve markers — assert we did not wipe env
for keep in .env .env.desktop .env.vps data logs; do
  if [[ -e "${INSTALL_DIR}/${keep}" ]]; then
    log "preserved: $keep"
  fi
done

if [[ "$RESTART" == "1" && -x "${INSTALL_DIR}/start_hackme_miner.sh" ]]; then
  log "starting miner via start_hackme_miner.sh"
  bash "${INSTALL_DIR}/start_hackme_miner.sh" || log "WARN: start returned non-zero"
fi

log "OK updated → $remote_ver (backup $BACKUP)"
