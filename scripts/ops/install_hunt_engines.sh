#!/usr/bin/env bash
# Install Hunt coverage engines for hybrid workers (libFuzzer via clang + bundled AFL++).
# Does NOT require miners to git clone AFL++ from GitHub — downloads a pinned release tarball.
#
# Usage:
#   bash scripts/ops/install_hunt_engines.sh --desktop
#   sudo bash scripts/ops/install_hunt_engines.sh --system --prefix /opt/hackme
#   bash scripts/ops/install_hunt_engines.sh --check-only
#
# Env:
#   AFLPP_VERSION   default 4.21c (release tag without leading v)
#   SKIP_AFLPP=1    only ensure clang
#   SKIP_CLANG=1    only install AFL++
#   SKIP_BWRAP=1    skip bubblewrap package install
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Release layout: script next to hackme (/opt/hackme). Repo layout: scripts/ops/.
if [[ -x "${SCRIPT_DIR}/hackme" || -f "${SCRIPT_DIR}/BUILD_INFO.txt" ]]; then
  ROOT_DIR="$SCRIPT_DIR"
elif [[ -d "${SCRIPT_DIR}/../../internal/hunt" ]]; then
  ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
else
  ROOT_DIR="$SCRIPT_DIR"
fi
AFLPP_VERSION="${AFLPP_VERSION:-4.21c}"
MODE_DESKTOP=0
MODE_SYSTEM=0
CHECK_ONLY=0
HACKME_PREFIX=""
SKIP_AFLPP="${SKIP_AFLPP:-0}"
SKIP_CLANG="${SKIP_CLANG:-0}"
SKIP_BWRAP="${SKIP_BWRAP:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --desktop) MODE_DESKTOP=1; shift ;;
    --system) MODE_SYSTEM=1; shift ;;
    --check-only) CHECK_ONLY=1; shift ;;
    --prefix) HACKME_PREFIX="${2:-}"; shift 2 ;;
    *) echo "[hunt-engines] unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$HACKME_PREFIX" ]]; then
  if [[ "$MODE_DESKTOP" == "1" ]]; then
    HACKME_PREFIX="${HACKME_PREFIX:-$ROOT_DIR/logs/desktop/toolchains}"
  elif [[ "$(id -u)" -eq 0 ]] || [[ "$MODE_SYSTEM" == "1" ]]; then
    HACKME_PREFIX="${HACKME_PREFIX:-/opt/hackme}"
  else
    HACKME_PREFIX="${HACKME_PREFIX:-$HOME/.local/share/hackme/toolchains}"
  fi
fi

log() { echo "[hunt-engines] $*"; }
warn() { echo "[hunt-engines] WARN: $*" >&2; }

have() { command -v "$1" >/dev/null 2>&1; }

afl_bin() { echo "$HACKME_PREFIX/aflpp/bin/afl-fuzz"; }

bwrap_probe() {
  bwrap --ro-bind / / --dev /dev --proc /proc --unshare-pid --unshare-net --die-with-parent -- \
    "$(command -v true 2>/dev/null || echo /bin/true)" >/dev/null 2>&1
}

check_status() {
  local ok=1
  if have clang; then
    log "clang: $(command -v clang) ($(clang --version 2>/dev/null | head -1))"
  else
    warn "clang missing — libFuzzer Hunt engine unavailable"
    ok=0
  fi
  if [[ -x "$(afl_bin)" ]] || have afl-fuzz; then
    local p
    p="$(afl_bin)"
    [[ -x "$p" ]] || p="$(command -v afl-fuzz)"
    log "afl-fuzz: $p"
  else
    warn "afl-fuzz missing — AFL++ Deep Hunt engine unavailable"
    ok=0
  fi
  if have bwrap; then
    log "bwrap: $(command -v bwrap)"
    # Probe the cage (nested userns / AppArmor may leave bwrap on PATH but unusable).
    if bwrap_probe; then
      log "bwrap: probe OK (userns cage)"
    else
      warn "bwrap present but probe FAILED (uid_map/userns/AppArmor) — Hunt STRICT will soft-skip"
      warn "Ubuntu 24.04+: rerun with HACKME_FIX_APPARMOR=1 to allow user namespaces for bwrap only"
      ok=0
    fi
  else
    warn "bubblewrap missing — Hunt STRICT refuses bare exec (no unsandboxed foreign harness)"
    ok=0
  fi
  return $((1 - ok))
}

if [[ "$CHECK_ONLY" == "1" ]]; then
  check_status && exit 0 || exit 1
fi

mkdir -p "$HACKME_PREFIX/bin" "$HACKME_PREFIX/aflpp"

# as_root runs a command as root: directly, via passwordless sudo, or via sudo with a
# password prompt when a terminal is attached. Never prompts from a background launcher.
as_root() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
  elif have sudo && sudo -n true 2>/dev/null; then
    sudo -n "$@"
  elif have sudo && [[ -t 0 && -t 1 ]]; then
    sudo "$@"
  else
    return 126
  fi
}

# pkg_install installs distro packages (apt / dnf / pacman / zypper). Args are
# "apt:pkgs|dnf:pkgs|pacman:pkgs|zypper:pkgs" lists.
PKG_UPDATED=0
pkg_install() {
  local apt="$1" dnf="$2" pac="$3" zyp="$4" rc=1
  if have apt-get; then
    if [[ "$PKG_UPDATED" == "0" ]]; then
      as_root env DEBIAN_FRONTEND=noninteractive apt-get update -qq >/dev/null 2>&1 || true
      PKG_UPDATED=1
    fi
    # shellcheck disable=SC2086
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq $apt; rc=$?
  elif have dnf; then
    # shellcheck disable=SC2086
    as_root dnf install -y -q $dnf; rc=$?
  elif have pacman; then
    # shellcheck disable=SC2086
    as_root pacman -S --needed --noconfirm $pac; rc=$?
  elif have zypper; then
    # shellcheck disable=SC2086
    as_root zypper --non-interactive install $zyp; rc=$?
  fi
  return $rc
}

ensure_clang() {
  if [[ "$SKIP_CLANG" == "1" ]]; then
    return 0
  fi
  if have clang; then
    log "clang already present"
    return 0
  fi
  log "installing clang + libFuzzer runtime (needs sudo)"
  pkg_install "clang lld llvm make" "clang compiler-rt llvm lld make" "clang compiler-rt llvm lld make" "clang lld llvm make" \
    || warn "could not install clang automatically — install it with your package manager (Ubuntu: sudo apt install clang lld llvm)"
}

ensure_bubblewrap() {
  if [[ "$SKIP_BWRAP" == "1" ]]; then
    return 0
  fi
  if have bwrap; then
    log "bubblewrap already present"
    return 0
  fi
  log "installing bubblewrap (Hunt sandbox, needs sudo)"
  pkg_install bubblewrap bubblewrap bubblewrap bubblewrap \
    || warn "could not install bubblewrap automatically — sudo apt install bubblewrap"
}

# Ubuntu 24.04+ blocks unprivileged user namespaces for unconfined programs
# (kernel.apparmor_restrict_unprivileged_userns=1), which breaks bwrap and with it every
# local Hunt. The narrow fix is an AppArmor profile that grants userns to /usr/bin/bwrap
# only (what Ubuntu's own bwrap-userns-restrict / Flatpak setups do) — not a global sysctl.
# Asked interactively, or HACKME_FIX_APPARMOR=1 for unattended installs.
ensure_bwrap_userns() {
  have bwrap || return 0
  bwrap_probe && return 0
  local restrict
  restrict="$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns 2>/dev/null || echo 0)"
  [[ "$restrict" == "1" ]] || return 0
  local bw prof=/etc/apparmor.d/hackme-bwrap
  bw="$(readlink -f "$(command -v bwrap)")"
  if [[ -f "$prof" ]]; then
    as_root apparmor_parser -r "$prof" >/dev/null 2>&1 || true
    bwrap_probe && { log "bwrap: AppArmor profile reloaded"; return 0; }
  fi
  local ok="${HACKME_FIX_APPARMOR:-}"
  if [[ -z "$ok" && -t 0 && -t 1 ]]; then
    echo "[hunt-engines] This system (Ubuntu 24.04+) blocks user namespaces, so the Hunt sandbox (bubblewrap) cannot start."
    echo "[hunt-engines] Fix: add an AppArmor profile that allows user namespaces for $bw only (no global change)."
    read -r -p "[hunt-engines] Install it now with sudo? [y/N] " ok || ok=""
  fi
  case "$ok" in
    1|y|Y|yes|YES) ;;
    *)
      warn "Hunt sandbox blocked by AppArmor — rerun with HACKME_FIX_APPARMOR=1 (adds $prof for $bw only)"
      return 0
      ;;
  esac
  local tmp
  tmp="$(mktemp)"
  cat >"$tmp" <<PROFILE
# HackMe: allow user namespaces for bubblewrap only (Hunt sandbox). Remove this file and
# run 'sudo systemctl reload apparmor' to undo.
abi <abi/4.0>,
include <tunables/global>

profile hackme-bwrap $bw flags=(unconfined) {
  userns,
  include if exists <local/hackme-bwrap>
}
PROFILE
  if as_root install -m 0644 "$tmp" "$prof" && as_root apparmor_parser -r "$prof"; then
    rm -f "$tmp"
    if bwrap_probe; then
      log "bwrap: AppArmor profile installed — sandbox works"
    else
      warn "AppArmor profile installed but bwrap still fails — check 'sudo dmesg | grep apparmor'"
    fi
  else
    rm -f "$tmp"
    warn "could not install $prof (sudo needed)"
  fi
}

install_aflpp_bundle() {
  if [[ "$SKIP_AFLPP" == "1" ]]; then
    return 0
  fi
  local dest="$HACKME_PREFIX/aflpp"
  if [[ -x "$dest/bin/afl-fuzz" ]]; then
    log "AFL++ already at $dest/bin/afl-fuzz"
    ln -sfn "$dest/bin/afl-fuzz" "$HACKME_PREFIX/bin/afl-fuzz"
    return 0
  fi
  if have afl-fuzz; then
    log "system afl-fuzz present: $(command -v afl-fuzz)"
    return 0
  fi

  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="x86_64" ;;
    aarch64|arm64) arch="aarch64" ;;
    *) warn "unsupported arch $arch for prebuilt AFL++ — skip"; return 0 ;;
  esac

  # Prefer a pre-vendored tarball in the release/hybrid bundle (no GitHub clone).
  local local_tb=""
  for cand in \
    "$ROOT_DIR/third_party/aflpp/aflpp-${AFLPP_VERSION}-linux-${arch}.tar.gz" \
    "$ROOT_DIR/dist/toolchains/aflpp-${AFLPP_VERSION}-linux-${arch}.tar.gz" \
    "$ROOT_DIR/aflpp-${AFLPP_VERSION}-linux-${arch}.tar.gz" \
    "$HACKME_PREFIX/third_party/aflpp/aflpp-${AFLPP_VERSION}-linux-${arch}.tar.gz" \
    "${HACKME_AFLPP_TARBALL:-}"; do
    [[ -n "$cand" && -f "$cand" ]] && { local_tb="$cand"; break; }
  done

  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  if [[ -n "$local_tb" ]]; then
    log "extracting bundled AFL++ from $local_tb"
    tar -xzf "$local_tb" -C "$tmp"
  else
    # Fallback: download official release source tarball (not git clone) and build.
    local url="https://github.com/AFLplusplus/AFLplusplus/archive/refs/tags/v${AFLPP_VERSION}.tar.gz"
    log "downloading AFL++ v${AFLPP_VERSION} release tarball (no git clone)"
    if have curl; then
      curl -fsSL "$url" -o "$tmp/aflpp.tgz" || { warn "download failed"; return 0; }
    elif have wget; then
      wget -qO "$tmp/aflpp.tgz" "$url" || { warn "download failed"; return 0; }
    else
      warn "curl/wget missing — cannot fetch AFL++"
      return 0
    fi
    tar -xzf "$tmp/aflpp.tgz" -C "$tmp"
    local src
    src="$(find "$tmp" -maxdepth 1 -type d -name 'AFLplusplus-*' | head -1)"
    if [[ -z "$src" ]]; then
      warn "AFL++ extract failed"
      return 0
    fi
    if ! have clang; then
      warn "clang required to build AFL++ from source"
      return 0
    fi
    # Prefer llvm-config-N when bare llvm-config is missing (Ubuntu packages).
    local llvm_config=""
    if have llvm-config; then
      llvm_config="$(command -v llvm-config)"
    else
      for cand in llvm-config-18 llvm-config-17 llvm-config-16 llvm-config-15; do
        if have "$cand"; then llvm_config="$(command -v "$cand")"; break; fi
      done
    fi
    log "building AFL++ PREFIX=$dest LLVM_CONFIG=${llvm_config:-none}"
    # Optional gcc_plugin/LTO may fail — still harvest afl-fuzz if the main tools built.
    set +e
    if [[ -n "$llvm_config" ]]; then
      make -C "$src" all PREFIX="$dest" -j"$(nproc 2>/dev/null || echo 2)" LLVM_CONFIG="$llvm_config"
    else
      make -C "$src" all PREFIX="$dest" -j"$(nproc 2>/dev/null || echo 2)"
    fi
    local make_rc=$?
    if [[ -x "$src/afl-fuzz" ]]; then
      mkdir -p "$dest/bin"
      # install target can fail on optional bits; copy core tools explicitly
      make -C "$src" install PREFIX="$dest" LLVM_CONFIG="${llvm_config:-llvm-config}" >/dev/null 2>&1
      for b in afl-fuzz afl-showmap afl-clang-fast afl-cc afl-c++ afl-clang-fast++; do
        [[ -x "$src/$b" ]] && cp -a "$src/$b" "$dest/bin/" || true
      done
      # supporting libs next to prefix when present
      for f in afl-compiler-rt.o libAFLDriver.a; do
        [[ -f "$src/$f" ]] && cp -a "$src/$f" "$dest/" || true
      done
      make_rc=0
    fi
    set -e
    if [[ "$make_rc" -ne 0 && ! -x "$dest/bin/afl-fuzz" && ! -x "$src/afl-fuzz" ]]; then
      warn "AFL++ build failed"
      return 0
    fi
  fi

  # If tarball laid out bin/ at top level or build left afl-fuzz in src tree:
  if [[ ! -x "$dest/bin/afl-fuzz" ]]; then
    local found
    found="$(find "$tmp" -type f -name afl-fuzz -perm -111 2>/dev/null | head -1 || true)"
    if [[ -n "$found" ]]; then
      mkdir -p "$dest/bin"
      cp -a "$(dirname "$found")/." "$dest/bin/" || true
    fi
  fi
  # Last-resort: copy a working system/hub afl-fuzz into the prefix (no GitHub clone).
  if [[ ! -x "$dest/bin/afl-fuzz" ]] && have afl-fuzz; then
    mkdir -p "$dest/bin"
    cp -a "$(command -v afl-fuzz)" "$dest/bin/afl-fuzz" || true
    log "copied system afl-fuzz → $dest/bin/afl-fuzz"
  fi
  if [[ -x "$dest/bin/afl-fuzz" ]]; then
    ln -sfn "$dest/bin/afl-fuzz" "$HACKME_PREFIX/bin/afl-fuzz"
    log "AFL++ installed → $dest/bin/afl-fuzz"
  else
    warn "afl-fuzz still missing after install attempt"
  fi
}

write_env() {
  local envf="$HACKME_PREFIX/.env.hunt-engines"
  cat >"$envf" <<EOF
# Generated by install_hunt_engines.sh
# Literal PATH: systemd EnvironmentFile= does not expand \${PATH}; the old line left
# workerfuzz units without /usr/bin (no clang/bwrap → never Hunt-capable).
PATH=${HACKME_PREFIX}/bin:${HACKME_PREFIX}/aflpp/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
HACKME_AFL_FUZZ=${HACKME_PREFIX}/aflpp/bin/afl-fuzz
HACKME_PREFIX=${HACKME_PREFIX}
EOF
  chmod 644 "$envf" 2>/dev/null || true
  log "wrote $envf"
}

ensure_clang
ensure_bubblewrap
ensure_bwrap_userns
install_aflpp_bundle
write_env
# Best-effort miner cache hygiene after engine install (idempotent).
if [[ -x "$ROOT_DIR/scripts/ops/prune_hunt_miner_caches.sh" ]]; then
  bash "$ROOT_DIR/scripts/ops/prune_hunt_miner_caches.sh" --repo "$ROOT_DIR" 2>/dev/null || true
elif [[ -x "$SCRIPT_DIR/prune_hunt_miner_caches.sh" ]]; then
  bash "$SCRIPT_DIR/prune_hunt_miner_caches.sh" --repo "$ROOT_DIR" 2>/dev/null || true
fi
if check_status; then
  log "done (clang + AFL++ + bwrap ready for Hunt)"
else
  if [[ "${HACKME_SKIP_HUNT_ENGINES:-0}" == "1" || "${MODE_DESKTOP}" == "1" ]]; then
    warn "partial install — Hunt is NOT available on this machine yet; Dig orders work"
    warn "fix the WARN lines above, then rerun: bash $0 --desktop"
    log "done (partial — Hunt unavailable)"
  else
    # System/miner install: fail closed so STRICT isolate does not silent-starve Hunt.
    echo "[hunt-engines] ERROR: clang and/or afl-fuzz and/or bwrap missing after install" >&2
    echo "[hunt-engines] ERROR: fix apt (sudo) or set HACKME_SKIP_HUNT_ENGINES=1" >&2
    exit 1
  fi
fi
