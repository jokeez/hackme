#!/usr/bin/env bash
set -euo pipefail

# Build polished release bundle for Windows + Linux.
#
# Usage:
#   VERSION=1.0.0 bash scripts/release/make_release_bundle.sh
#
# Optional:
#   APP_NAME=HackMe
#   WIN_ARCH=amd64
#   LINUX_ARCH=amd64
#   CGO_ENABLED=0

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[release] missing command: $1" >&2
    exit 1
  }
}

require_cmd go
require_cmd tar
require_cmd zip
require_cmd sha256sum
require_cmd stat
require_cmd jq

APP_NAME="${APP_NAME:-HackMe}"
VERSION="${VERSION:-0.1.0-dev}"
WIN_ARCH="${WIN_ARCH:-amd64}"
LINUX_ARCH="${LINUX_ARCH:-amd64}"
CGO_ENABLED="${CGO_ENABLED:-0}"
BUILD_DATE_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
COMMIT_SHA="${COMMIT_SHA:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo "nogit")}"

DIST_DIR="${ROOT_DIR}/dist/release_${VERSION}"
WIN_DIR="${DIST_DIR}/windows"
LINUX_DIR="${DIST_DIR}/linux"
rm -rf "${WIN_DIR}" "${LINUX_DIR}"
mkdir -p "${WIN_DIR}" "${LINUX_DIR}"

LDFLAGS="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT_SHA} -X main.BuildDate=${BUILD_DATE_UTC}"

echo "[release] generating windows PE resources"
VERSION="${VERSION}" bash "${ROOT_DIR}/scripts/release/windows/build_windows_resources.sh"

echo "[release] building windows binary"
GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -ldflags "${LDFLAGS}" -o "${WIN_DIR}/hackme.exe" ./

echo "[release] building linux binary"
GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -ldflags "${LDFLAGS}" -o "${LINUX_DIR}/hackme" ./

echo "[release] building minersign helper (linux + windows)"
GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -ldflags "-s -w" -o "${LINUX_DIR}/minersign" ./cmd/minersign
GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -ldflags "-s -w" -o "${WIN_DIR}/minersign.exe" ./cmd/minersign

echo "[release] building workerpoh (pool worker; required for Windows dashboard worker start)"
GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -buildvcs=true -ldflags "${LDFLAGS}" -o "${LINUX_DIR}/workerpoh" ./cmd/workerpoh
GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -buildvcs=true -ldflags "${LDFLAGS}" -o "${WIN_DIR}/workerpoh.exe" ./cmd/workerpoh

# Windows OpenCL worker (AMD RX 580 etc.): docker mingw or host mingw when available.
if [[ -x "${ROOT_DIR}/scripts/release/windows/build_workerpoh_opencl.sh" ]] && command -v docker >/dev/null 2>&1; then
  if [[ ! -f "${WIN_DIR}/workerpoh-opencl.exe" ]]; then
    bash "${ROOT_DIR}/scripts/release/windows/build_workerpoh_opencl.sh" "${WIN_DIR}/workerpoh-opencl.exe" || \
      echo "[release] WARN: workerpoh-opencl.exe build failed" >&2
  fi
fi
# Prefer shipping a freshly built GPU worker. Only fall back to bin/ when the
# fresh build is absent AND bin/ passes marker checks (never silently keep a
# stale LINUX_DIR leftover from a previous release dir).
ship_linux_gpu_worker() {
  local name="$1"
  if [[ -x "${LINUX_DIR}/${name}" ]]; then
    return 0
  fi
  if [[ -x "${ROOT_DIR}/bin/${name}" ]]; then
    install -m 0755 "${ROOT_DIR}/bin/${name}" "${LINUX_DIR}/${name}"
    return 0
  fi
  return 1
}

# Fail closed if a GPU/CPU worker is missing required markers (stale bins).
assert_bin_markers() {
  local bin="$1"
  shift
  local needle
  if [[ ! -f "$bin" ]]; then
    echo "[release] ERROR: missing binary $bin" >&2
    return 1
  fi
  for needle in "$@"; do
    if ! grep -a -F -q -- "$needle" "$bin"; then
      echo "[release] ERROR: $bin missing required marker '$needle' (stale GPU worker?)" >&2
      return 1
    fi
  done
  return 0
}

# Require wasm-validate child hook + matching vcs revision when buildvcs is present.
assert_worker_fresh() {
  local bin="$1"
  assert_bin_markers "$bin" "-hackme-wasm-validate" "share_scan_max" || return 1
  if command -v go >/dev/null 2>&1; then
    local vcs
    # go version -m lines look like: $'\t'build$'\t'vcs.revision=<hash>
    vcs="$(go version -m "$bin" 2>/dev/null | sed -n 's/.*vcs\.revision=//p' | head -n1 | tr -d '[:space:]')"
    if [[ -n "$vcs" && -n "${COMMIT_SHA}" && "${COMMIT_SHA}" != "nogit" ]]; then
      # COMMIT_SHA may be short; accept prefix match either way.
      if [[ "$vcs" != "${COMMIT_SHA}"* && "${COMMIT_SHA}" != "$vcs"* ]]; then
        echo "[release] ERROR: $bin vcs.revision=${vcs} != build COMMIT_SHA=${COMMIT_SHA}" >&2
        return 1
      fi
    fi
  fi
  return 0
}

if [[ "${CGO_ENABLED}" == "1" ]] && (pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]] || [[ -f /tmp/hm-prefix/usr/include/CL/cl.h ]]); then
  echo "[release] building workerpoh-opencl (AMD/Intel/NVIDIA via OpenCL ICD)"
  if [[ -f /tmp/hm-prefix/usr/include/CL/cl.h && ! -f /usr/include/CL/cl.h ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I/tmp/hm-prefix/usr/include"
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} -L/tmp/hm-prefix/usr/lib/x86_64-linux-gnu -lOpenCL"
  fi
  # Never leave a previous release's OpenCL binary in place if this build fails.
  rm -f "${LINUX_DIR}/workerpoh-opencl" "${ROOT_DIR}/bin/workerpoh-opencl"
  GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED=1 \
    go build -tags opencl -trimpath -buildvcs=true -ldflags "${LDFLAGS}" \
    -o "${LINUX_DIR}/workerpoh-opencl" ./cmd/workerpoh
  install -m 0755 "${LINUX_DIR}/workerpoh-opencl" "${ROOT_DIR}/bin/workerpoh-opencl"
  assert_worker_fresh "${LINUX_DIR}/workerpoh-opencl" || exit 1
  if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
    echo "[release] building workerpoh-opencl.exe (mingw cross)"
    rm -f "${WIN_DIR}/workerpoh-opencl.exe"
    CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
      GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED=1 \
      go build -tags opencl -trimpath -buildvcs=true -ldflags "${LDFLAGS}" -o "${WIN_DIR}/workerpoh-opencl.exe" ./cmd/workerpoh
  elif command -v docker >/dev/null 2>&1 && [[ -f "${ROOT_DIR}/scripts/release/windows/build_workerpoh_opencl.sh" ]]; then
    echo "[release] building workerpoh-opencl.exe (docker mingw)"
    rm -f "${WIN_DIR}/workerpoh-opencl.exe"
    bash "${ROOT_DIR}/scripts/release/windows/build_workerpoh_opencl.sh" "${WIN_DIR}/workerpoh-opencl.exe"
  else
    echo "[release] WARN: skip workerpoh-opencl.exe (install mingw-w64 or docker for Windows OpenCL build)" >&2
  fi
elif [[ "${CGO_ENABLED}" == "1" ]]; then
  echo "[release] ERROR: CGO_ENABLED=1 but OpenCL headers/ICD not found — cannot ship AMD workerpoh-opencl" >&2
  exit 1
else
  echo "[release] ERROR: CGO_ENABLED=0 — AMD workerpoh-opencl requires CGO_ENABLED=1 (OpenCL ICD)" >&2
  exit 1
fi


if [[ -x "${ROOT_DIR}/scripts/ops/build_cuda_worker.sh" ]]; then
  echo "[release] building workerpoh-cuda (NVIDIA native; Linux only)"
  # Do not swallow stderr — silent failure previously shipped Sep-dated bin/workerpoh-cuda
  # without Blake3 device kernel / share_scan_max.
  if ! bash "${ROOT_DIR}/scripts/ops/build_cuda_worker.sh"; then
    echo "[release] WARN: workerpoh-cuda build failed — will use bin/ only if it passes marker checks" >&2
  fi
fi
if ship_linux_gpu_worker workerpoh-cuda; then
  chmod +x "${LINUX_DIR}/workerpoh-cuda"
  ln -sf workerpoh-cuda "${LINUX_DIR}/workerpoh-gpu" 2>/dev/null || true
  assert_bin_markers "${LINUX_DIR}/workerpoh-cuda" "poh_search_blake3" "share_scan_max" || exit 1
else
  echo "[release] ERROR: workerpoh-cuda missing — Linux NVIDIA miners need it in the bundle" >&2
  exit 1
fi
# Default + OpenCL workers must honor claim share_scan_max (soft pool payout).
assert_worker_fresh "${LINUX_DIR}/workerpoh" || exit 1
if [[ -x "${LINUX_DIR}/workerpoh-opencl" ]]; then
  assert_worker_fresh "${LINUX_DIR}/workerpoh-opencl" || exit 1
else
  echo "[release] ERROR: linux workerpoh-opencl missing after OpenCL build step" >&2
  exit 1
fi
if [[ -f "${WIN_DIR}/workerpoh-opencl.exe" ]]; then
  assert_bin_markers "${WIN_DIR}/workerpoh-opencl.exe" "-hackme-wasm-validate" "share_scan_max" || exit 1
fi
assert_bin_markers "${WIN_DIR}/workerpoh.exe" "-hackme-wasm-validate" "share_scan_max" || exit 1
assert_bin_markers "${LINUX_DIR}/workerpoh" "share_mod" || exit 1

# Bundle NVRTC runtime next to workers (no system CUDA toolkit required on miner hosts).
echo "[release] bundling CUDA NVRTC libs into linux/lib/"
mkdir -p "${LINUX_DIR}/lib"
_nvrtc_src=""
for _nvrtc_src in \
  "${ROOT_DIR}/.deps/cuda-lib" \
  "${ROOT_DIR}/lib" \
  "${ROOT_DIR}/dist/linux-lib-staging" \
  "/tmp/hm-prefix/usr/lib/x86_64-linux-gnu"
do
  if [[ -e "${_nvrtc_src}/libnvrtc.so.12" || -e "${_nvrtc_src}/libnvrtc.so" ]]; then
    cp -a "${_nvrtc_src}"/libnvrtc.so* "${LINUX_DIR}/lib/" 2>/dev/null || true
    cp -a "${_nvrtc_src}"/libnvrtc-builtins.so* "${LINUX_DIR}/lib/" 2>/dev/null || true
    break
  fi
done
if [[ -e "${LINUX_DIR}/lib/libnvrtc.so.12" || -e "${LINUX_DIR}/lib/libnvrtc.so" ]]; then
  echo "[release] NVRTC libs: $(ls "${LINUX_DIR}/lib"/libnvrtc*.so* 2>/dev/null | wc -l) files"
else
  echo "[release] WARN: libnvrtc.so.12 not bundled — workerpoh-cuda may fail without system NVRTC" >&2
fi
unset _nvrtc_src

for doc in docs/GPU_MINING_BACKENDS.md docs/CUDA_PRODUCTION.md; do
  [[ -f "${ROOT_DIR}/${doc}" ]] && cp "${ROOT_DIR}/${doc}" "${LINUX_DIR}/"
done
for op in build_gpu_workers.sh build_cuda_worker.sh detect_gpu_backend.sh desktop_worker_reset.sh worker_autostart.sh worker_loop.sh purge_stale_pool_workers.sh stop_pool_workers.sh resume_pool_mining.sh cuda_env.sh setup_cuda_desktop.sh; do
  [[ -f "${ROOT_DIR}/scripts/ops/${op}" ]] && cp "${ROOT_DIR}/scripts/ops/${op}" "${LINUX_DIR}/"
done
chmod +x "${LINUX_DIR}/worker_autostart.sh" "${LINUX_DIR}/purge_stale_pool_workers.sh" 2>/dev/null || true
echo "[release] linux miner layout (bin/ + scripts/ops/)"
mkdir -p "${LINUX_DIR}/bin" "${LINUX_DIR}/scripts/ops"
for op in detect_gpu_backend.sh desktop_worker_reset.sh worker_autostart.sh worker_loop.sh purge_stale_pool_workers.sh stop_pool_workers.sh resume_pool_mining.sh; do
  if [[ -f "${ROOT_DIR}/scripts/ops/${op}" ]]; then
    install -m 0755 "${ROOT_DIR}/scripts/ops/${op}" "${LINUX_DIR}/scripts/ops/${op}"
  fi
done
# Hardlink into bin/ when possible so tar.gz does not double-store worker blobs.
for wp in workerpoh workerpoh-opencl workerpoh-cuda; do
  if [[ -f "${LINUX_DIR}/${wp}" ]]; then
    ln -f "${LINUX_DIR}/${wp}" "${LINUX_DIR}/bin/${wp}" 2>/dev/null || \
      install -m 0755 "${LINUX_DIR}/${wp}" "${LINUX_DIR}/bin/${wp}"
  fi
done
[[ -f "${LINUX_DIR}/fleetplan" ]] && { ln -f "${LINUX_DIR}/fleetplan" "${LINUX_DIR}/bin/fleetplan" 2>/dev/null || install -m 0755 "${LINUX_DIR}/fleetplan" "${LINUX_DIR}/bin/fleetplan"; }
[[ -f "${LINUX_DIR}/minersign" ]] && { ln -f "${LINUX_DIR}/minersign" "${LINUX_DIR}/bin/minersign" 2>/dev/null || install -m 0755 "${LINUX_DIR}/minersign" "${LINUX_DIR}/bin/minersign"; }
cp "${ROOT_DIR}/scripts/release/linux/fix_miner_layout.sh" "${LINUX_DIR}/fix_miner_layout.sh"
chmod +x "${LINUX_DIR}/fix_miner_layout.sh"
echo "[release] building hackme-wasm-check (no-cgo Dig validate child; required next to workerpoh-cuda)"
# MUST be CGO_ENABLED=0 — CUDA-linked self-reexec dies in cuInit under RLIMIT_AS.
GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED=0 \
  go build -trimpath -buildvcs=true -ldflags "-s -w" -o "${LINUX_DIR}/hackme-wasm-check" ./cmd/hackme-wasm-check
GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED=0 \
  go build -trimpath -buildvcs=true -ldflags "-s -w" -o "${WIN_DIR}/hackme-wasm-check.exe" ./cmd/hackme-wasm-check
ln -f "${LINUX_DIR}/hackme-wasm-check" "${LINUX_DIR}/bin/hackme-wasm-check" 2>/dev/null || \
  install -m 0755 "${LINUX_DIR}/hackme-wasm-check" "${LINUX_DIR}/bin/hackme-wasm-check"
assert_bin_markers "${LINUX_DIR}/hackme-wasm-check" "-hackme-wasm-validate" "hackme-wasm-validate-child" || exit 1

echo "[release] building workerfuzz (pool dig; hybrid process mode + dedicated diggers)"
WF_LDFLAGS="-s -w -X hackme/internal/workerfuzzloop.Version=${VERSION}"
GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -ldflags "${WF_LDFLAGS}" -o "${LINUX_DIR}/workerfuzz" ./cmd/workerfuzz
GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -ldflags "${WF_LDFLAGS}" -o "${WIN_DIR}/workerfuzz.exe" ./cmd/workerfuzz
ln -f "${LINUX_DIR}/workerfuzz" "${LINUX_DIR}/bin/workerfuzz" 2>/dev/null || \
  install -m 0755 "${LINUX_DIR}/workerfuzz" "${LINUX_DIR}/bin/workerfuzz"
# Dig OOTB: Setup + ZIP must both ship workerfuzz (ISS no longer skipifsourcedoesntexist).
if [[ ! -f "${WIN_DIR}/workerfuzz.exe" ]]; then
  echo "[release] ERROR: windows workerfuzz.exe missing — Dig would break OOTB on Setup" >&2
  exit 1
fi
if [[ ! -f "${WIN_DIR}/hackme-wasm-check.exe" ]]; then
  echo "[release] ERROR: windows hackme-wasm-check.exe missing" >&2
  exit 1
fi
# CUDA Dig uses sibling checker; fail closed if cuda ships without it.
if [[ -x "${LINUX_DIR}/workerpoh-cuda" && ! -x "${LINUX_DIR}/hackme-wasm-check" ]]; then
  echo "[release] ERROR: workerpoh-cuda present but hackme-wasm-check missing" >&2
  exit 1
fi

echo "[release] building fleetplan (GPU fleet JSON)"
GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED=0 \
  go build -trimpath -buildvcs=true -ldflags "${LDFLAGS}" -o "${LINUX_DIR}/fleetplan" ./cmd/fleetplan
GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED=0 \
  go build -trimpath -buildvcs=true -ldflags "${LDFLAGS}" -o "${WIN_DIR}/fleetplan.exe" ./cmd/fleetplan
[[ -f "${LINUX_DIR}/fleetplan" ]] && { ln -f "${LINUX_DIR}/fleetplan" "${LINUX_DIR}/bin/fleetplan" 2>/dev/null || install -m 0755 "${LINUX_DIR}/fleetplan" "${LINUX_DIR}/bin/fleetplan"; }

cp "${ROOT_DIR}/README.md" "${WIN_DIR}/README.md"
cp "${ROOT_DIR}/README.md" "${LINUX_DIR}/README.md"
cp "${ROOT_DIR}/scripts/release/windows/hackme.iss" "${WIN_DIR}/hackme.iss"
cp "${ROOT_DIR}/scripts/release/windows/sign_windows.ps1" "${WIN_DIR}/sign_windows.ps1"
cp "${ROOT_DIR}/scripts/release/windows/build_windows_resources.ps1" "${WIN_DIR}/build_windows_resources.ps1"
cp "${ROOT_DIR}/scripts/release/windows/build_windows_resources.sh" "${WIN_DIR}/build_windows_resources.sh"
cp "${ROOT_DIR}/scripts/release/windows/versioninfo.json.template" "${WIN_DIR}/versioninfo.json.template"
cp "${ROOT_DIR}/scripts/release/windows/app.manifest" "${WIN_DIR}/app.manifest"
cp "${ROOT_DIR}/scripts/release/windows/README_WINDOWS_BRANDING.md" "${WIN_DIR}/README_WINDOWS_BRANDING.md"
cp "${ROOT_DIR}/scripts/release/windows/start_hackme_dashboard.bat" "${WIN_DIR}/start_hackme_dashboard.bat"
cp "${ROOT_DIR}/scripts/release/windows/start_hackme_desktop_mode.bat" "${WIN_DIR}/start_hackme_desktop_mode.bat"
cp "${ROOT_DIR}/scripts/release/RELEASE_QUICKSTART.md" "${WIN_DIR}/RELEASE_QUICKSTART.md"
cp "${ROOT_DIR}/scripts/release/RELEASE_QUICKSTART.md" "${LINUX_DIR}/RELEASE_QUICKSTART.md"
cp "${ROOT_DIR}/scripts/release/windows/install_from_code_toolchains.ps1" "${WIN_DIR}/install_from_code_toolchains.ps1"
cp "${ROOT_DIR}/scripts/release/windows/install_from_code_toolchains.bat" "${WIN_DIR}/install_from_code_toolchains.bat"
cp "${ROOT_DIR}/scripts/release/windows/status_hackme_desktop_mode.bat" "${WIN_DIR}/status_hackme_desktop_mode.bat"
cp "${ROOT_DIR}/scripts/release/windows/stop_hackme_desktop_mode.bat" "${WIN_DIR}/stop_hackme_desktop_mode.bat"
cp "${ROOT_DIR}/scripts/release/windows/hackme_autostart_boot.bat" "${WIN_DIR}/hackme_autostart_boot.bat"
cp "${ROOT_DIR}/scripts/release/windows/env.public_pool.example" "${WIN_DIR}/env.public_pool.example"
cp "${ROOT_DIR}/scripts/release/windows/start_hackme_public_pool.bat" "${WIN_DIR}/start_hackme_public_pool.bat"
cp "${ROOT_DIR}/scripts/release/windows/setup_hackme_miner.bat" "${WIN_DIR}/setup_hackme_miner.bat"
cp "${ROOT_DIR}/scripts/release/windows/start_hackme_miner.bat" "${WIN_DIR}/start_hackme_miner.bat"
cp "${ROOT_DIR}/scripts/release/windows/start_hackme_orders.bat" "${WIN_DIR}/start_hackme_orders.bat"
# Catalog Hunt needs upstream/oss_cve_targets.json next to the binary (HACKME_REPO_ROOT).
mkdir -p "${WIN_DIR}/upstream"
if [[ -f "${ROOT_DIR}/upstream/oss_cve_targets.json" ]]; then
  cp -f "${ROOT_DIR}/upstream/oss_cve_targets.json" "${WIN_DIR}/upstream/oss_cve_targets.json"
fi
if [[ -f "${ROOT_DIR}/upstream/pins.json" ]]; then
  cp -f "${ROOT_DIR}/upstream/pins.json" "${WIN_DIR}/upstream/pins.json"
fi
cp "${ROOT_DIR}/scripts/release/windows/autostart_pool_worker.bat" "${WIN_DIR}/autostart_pool_worker.bat"
cp "${ROOT_DIR}/scripts/release/windows/watchdog_pool_worker.bat" "${WIN_DIR}/watchdog_pool_worker.bat"
cp "${ROOT_DIR}/scripts/release/windows/run_watchdog_hidden.vbs" "${WIN_DIR}/run_watchdog_hidden.vbs"
cp "${ROOT_DIR}/scripts/release/windows/detect_gpu.ps1" "${WIN_DIR}/detect_gpu.ps1"
cp "${ROOT_DIR}/scripts/release/windows/set_windows_power_perf.ps1" "${WIN_DIR}/set_windows_power_perf.ps1"
cp "${ROOT_DIR}/scripts/release/windows/repair_hackme_env.bat" "${WIN_DIR}/repair_hackme_env.bat"
cp "${ROOT_DIR}/scripts/release/windows/repair_fair_env.bat" "${WIN_DIR}/repair_fair_env.bat"
cp "${ROOT_DIR}/scripts/release/windows/write_hackme_env.ps1" "${WIN_DIR}/write_hackme_env.ps1"
cp "${ROOT_DIR}/scripts/release/windows/lib_hackme_dir.bat" "${WIN_DIR}/lib_hackme_dir.bat" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/build_workerpoh_opencl.ps1" "${WIN_DIR}/build_workerpoh_opencl.ps1" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/patch_opencl_env.bat" "${WIN_DIR}/patch_opencl_env.bat" 2>/dev/null || true
cp "${ROOT_DIR}/docs/MINER_WINDOWS_ONE_CLICK.md" "${WIN_DIR}/MINER_WINDOWS_ONE_CLICK.md" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/INSTALLER_WELCOME.txt" "${WIN_DIR}/INSTALLER_WELCOME.txt" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/INSTALLER_AFTER.txt" "${WIN_DIR}/INSTALLER_AFTER.txt" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/wizard-side.bmp" "${WIN_DIR}/wizard-side.bmp" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/wizard-small.bmp" "${WIN_DIR}/wizard-small.bmp" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/LICENSE.txt" "${WIN_DIR}/LICENSE.txt" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/release/windows/hackme.ico" "${WIN_DIR}/hackme.ico"
cp "${ROOT_DIR}/scripts/release/windows/hackme.png" "${WIN_DIR}/hackme.png"
cp "${ROOT_DIR}/scripts/release/windows/Install-HackMe.ps1" "${WIN_DIR}/Install-HackMe.ps1"
cp "${ROOT_DIR}/scripts/release/windows/HackMe-Install.cmd" "${WIN_DIR}/HackMe-Install.cmd"
cp "${ROOT_DIR}/scripts/ops/update_hackme_miner.ps1" "${WIN_DIR}/update_hackme_miner.ps1"
cp "${ROOT_DIR}/scripts/ops/update_hackme_miner.bat" "${WIN_DIR}/update_hackme_miner.bat"
cp "${ROOT_DIR}/scripts/release/linux/install_hackme.sh" "${LINUX_DIR}/install_hackme.sh"
cp "${ROOT_DIR}/scripts/release/linux/setup_hackme_miner.sh" "${LINUX_DIR}/setup_hackme_miner.sh"
cp "${ROOT_DIR}/scripts/release/linux/start_hackme_miner.sh" "${LINUX_DIR}/start_hackme_miner.sh"
cp "${ROOT_DIR}/scripts/release/linux/stop_hackme_miner.sh" "${LINUX_DIR}/stop_hackme_miner.sh"
cp "${ROOT_DIR}/scripts/release/linux/start_hackme_orders.sh" "${LINUX_DIR}/start_hackme_orders.sh"
cp "${ROOT_DIR}/scripts/release/linux/hackme.desktop.template" "${LINUX_DIR}/hackme.desktop.template"
cp "${ROOT_DIR}/scripts/release/linux/hackme-dashboard.desktop.template" "${LINUX_DIR}/hackme-dashboard.desktop.template"
cp "${ROOT_DIR}/scripts/release/linux/hackme-orders.desktop.template" "${LINUX_DIR}/hackme-orders.desktop.template"
cp "${ROOT_DIR}/scripts/release/linux/install_menu_entry.sh" "${LINUX_DIR}/install_menu_entry.sh"
mkdir -p "${LINUX_DIR}/icons"
cp -a "${ROOT_DIR}/scripts/release/linux/icons/." "${LINUX_DIR}/icons/" 2>/dev/null || {
  # Fallback: windows brand assets
  [[ -f "${ROOT_DIR}/scripts/release/windows/hackme.png" ]] && \
    cp "${ROOT_DIR}/scripts/release/windows/hackme.png" "${LINUX_DIR}/icons/hackme.png"
}
cp "${ROOT_DIR}/scripts/release/linux/hackme-node.service.template" "${LINUX_DIR}/hackme-node.service.template"
cp "${ROOT_DIR}/scripts/ops/desktop_mode_up.sh" "${LINUX_DIR}/desktop_mode_up.sh"
cp "${ROOT_DIR}/scripts/ops/desktop_mode_status.sh" "${LINUX_DIR}/desktop_mode_status.sh"
cp "${ROOT_DIR}/scripts/ops/desktop_mode_stop.sh" "${LINUX_DIR}/desktop_mode_stop.sh"
cp "${ROOT_DIR}/scripts/release/linux/hackme-desktop.service.template" "${LINUX_DIR}/hackme-desktop.service.template" 2>/dev/null || true
cp "${ROOT_DIR}/scripts/ops/install_linux_desktop_launcher.sh" "${LINUX_DIR}/install_linux_desktop_launcher.sh"
cp "${ROOT_DIR}/scripts/ops/install_from_code_toolchains.sh" "${LINUX_DIR}/install_from_code_toolchains.sh"
cp "${ROOT_DIR}/scripts/ops/install_hunt_engines.sh" "${LINUX_DIR}/install_hunt_engines.sh"
cp "${ROOT_DIR}/scripts/ops/prune_hunt_miner_caches.sh" "${LINUX_DIR}/prune_hunt_miner_caches.sh"
cp "${ROOT_DIR}/scripts/ops/update_hackme_miner.sh" "${LINUX_DIR}/update_hackme_miner.sh"
cp "${ROOT_DIR}/scripts/ops/update_hackme_os_binaries.sh" "${LINUX_DIR}/update_hackme_os_binaries.sh"

chmod +x "${LINUX_DIR}/install_hackme.sh"
chmod +x "${LINUX_DIR}/setup_hackme_miner.sh"
chmod +x "${LINUX_DIR}/start_hackme_miner.sh"
chmod +x "${LINUX_DIR}/stop_hackme_miner.sh"
chmod +x "${LINUX_DIR}/start_hackme_orders.sh"
chmod +x "${LINUX_DIR}/install_menu_entry.sh"
chmod +x "${LINUX_DIR}/desktop_mode_up.sh"
chmod +x "${LINUX_DIR}/desktop_mode_status.sh"
chmod +x "${LINUX_DIR}/desktop_mode_stop.sh"
chmod +x "${LINUX_DIR}/install_linux_desktop_launcher.sh"
chmod +x "${LINUX_DIR}/install_from_code_toolchains.sh"
chmod +x "${LINUX_DIR}/install_hunt_engines.sh"
chmod +x "${LINUX_DIR}/prune_hunt_miner_caches.sh"
chmod +x "${LINUX_DIR}/update_hackme_miner.sh"
chmod +x "${LINUX_DIR}/update_hackme_os_binaries.sh"
chmod +x "${LINUX_DIR}/workerpoh"

POOL_MINER_TOKEN="${HACKME_RELEASE_POOL_MINER_TOKEN:-}"
if [[ -z "${POOL_MINER_TOKEN}" && -f "${ROOT_DIR}/.secrets/hackme_coordinator_worker_token" ]]; then
  POOL_MINER_TOKEN="$(tr -d '\r\n' <"${ROOT_DIR}/.secrets/hackme_coordinator_worker_token")"
fi
if [[ -z "${POOL_MINER_TOKEN}" ]]; then
  echo "[release] WARN: no pool miner token — run: bash scripts/ops/gen_coordinator_worker_token.sh" >&2
  echo "[release]       then: HACKME_COORDINATOR_WORKER_TOKEN on VPS + rebuild release" >&2
  POOL_MINER_TOKEN="REPLACE_WITH_POOL_TOKEN"
else
  printf '%s' "${POOL_MINER_TOKEN}" >"${WIN_DIR}/pool.miner.token"
  printf '%s' "${POOL_MINER_TOKEN}" >"${LINUX_DIR}/pool.miner.token"
  echo "[release] wrote pool.miner.token (${#POOL_MINER_TOKEN} chars) for Windows + Linux miners"
fi

# B2B Orders publish token (scoped): may ship when explicitly provided for rc18.1+ Orders.
# NEVER fall back to coordinator admin token (rc17 leak). Must differ from miner token.
for _orders_tok in "${WIN_DIR}/pool.orders.token" "${LINUX_DIR}/pool.orders.token"; do
  rm -f "${_orders_tok}"
done
printf '%s\n' "REPLACE_WITH_ORDERS_PUBLISH_TOKEN" >"${WIN_DIR}/pool.orders.token.example"
printf '%s\n' "REPLACE_WITH_ORDERS_PUBLISH_TOKEN" >"${LINUX_DIR}/pool.orders.token.example"
ORDERS_PUBLISH_TOKEN="${HACKME_RELEASE_ORDERS_PUBLISH_TOKEN:-}"
if [[ -z "${ORDERS_PUBLISH_TOKEN}" && -f "${ROOT_DIR}/.secrets/hackme_coordinator_orders_publish_token" ]]; then
  ORDERS_PUBLISH_TOKEN="$(tr -d '\r\n' <"${ROOT_DIR}/.secrets/hackme_coordinator_orders_publish_token")"
fi
if [[ -n "${ORDERS_PUBLISH_TOKEN}" ]]; then
  if [[ "${ORDERS_PUBLISH_TOKEN}" == "REPLACE_WITH_ORDERS_PUBLISH_TOKEN" ]]; then
    echo "[release] REFUSING placeholder ORDERS_PUBLISH_TOKEN" >&2
    exit 1
  fi
  if [[ -n "${POOL_MINER_TOKEN}" && "${ORDERS_PUBLISH_TOKEN}" == "${POOL_MINER_TOKEN}" ]]; then
    echo "[release] REFUSING: orders publish token must differ from pool.miner.token" >&2
    exit 1
  fi
  _admin="${HACKME_COORDINATOR_ADMIN_TOKEN:-}"
  if [[ -z "${_admin}" && -f "${ROOT_DIR}/.secrets/hackme_coordinator_admin_token" ]]; then
    _admin="$(tr -d '\r\n' <"${ROOT_DIR}/.secrets/hackme_coordinator_admin_token")"
  fi
  if [[ -n "${_admin}" && "${ORDERS_PUBLISH_TOKEN}" == "${_admin}" ]]; then
    echo "[release] REFUSING: orders publish token must differ from coordinator admin token" >&2
    exit 1
  fi
  printf '%s' "${ORDERS_PUBLISH_TOKEN}" >"${WIN_DIR}/pool.orders.token"
  printf '%s' "${ORDERS_PUBLISH_TOKEN}" >"${LINUX_DIR}/pool.orders.token"
  echo "[release] wrote scoped pool.orders.token (${#ORDERS_PUBLISH_TOKEN} chars) for Orders profile"
else
  echo "[release] NOTE: no orders publish token — Orders local Dig/Hunt OK; public-pool register needs pool.orders.token"
fi

# Prepaid hub signing secret — DISTINCT from pool.orders.token (fleet publish credential).
# Prefer hub-only key + admin mint (HACKME_ORDERS_PREPAID_LOCAL_SIGN=0) when not shipping this.
for _prepaid in "${WIN_DIR}/pool.orders.prepaid_hub.secret" "${LINUX_DIR}/pool.orders.prepaid_hub.secret"; do
  rm -f "${_prepaid}"
done
printf '%s\n' "REPLACE_WITH_ORDERS_PREPAID_HUB_SECRET" >"${WIN_DIR}/pool.orders.prepaid_hub.secret.example"
printf '%s\n' "REPLACE_WITH_ORDERS_PREPAID_HUB_SECRET" >"${LINUX_DIR}/pool.orders.prepaid_hub.secret.example"
ORDERS_PREPAID_HUB_SECRET="${HACKME_RELEASE_ORDERS_PREPAID_HUB_SECRET:-}"
if [[ -z "${ORDERS_PREPAID_HUB_SECRET}" && -f "${ROOT_DIR}/.secrets/hackme_orders_prepaid_hub_secret" ]]; then
  ORDERS_PREPAID_HUB_SECRET="$(tr -d '\r\n' <"${ROOT_DIR}/.secrets/hackme_orders_prepaid_hub_secret")"
fi
if [[ -n "${ORDERS_PREPAID_HUB_SECRET}" ]]; then
  if [[ "${ORDERS_PREPAID_HUB_SECRET}" == "REPLACE_WITH_ORDERS_PREPAID_HUB_SECRET" ]]; then
    echo "[release] REFUSING placeholder ORDERS_PREPAID_HUB_SECRET" >&2
    exit 1
  fi
  if [[ -n "${ORDERS_PUBLISH_TOKEN}" && "${ORDERS_PREPAID_HUB_SECRET}" == "${ORDERS_PUBLISH_TOKEN}" ]]; then
    echo "[release] REFUSING: prepaid hub secret must differ from pool.orders.token (fleet publish)" >&2
    exit 1
  fi
  if [[ -n "${POOL_MINER_TOKEN}" && "${ORDERS_PREPAID_HUB_SECRET}" == "${POOL_MINER_TOKEN}" ]]; then
    echo "[release] REFUSING: prepaid hub secret must differ from pool.miner.token" >&2
    exit 1
  fi
  _admin_pp="${HACKME_COORDINATOR_ADMIN_TOKEN:-}"
  if [[ -z "${_admin_pp}" && -f "${ROOT_DIR}/.secrets/hackme_coordinator_admin_token" ]]; then
    _admin_pp="$(tr -d '\r\n' <"${ROOT_DIR}/.secrets/hackme_coordinator_admin_token")"
  fi
  if [[ -n "${_admin_pp}" && "${ORDERS_PREPAID_HUB_SECRET}" == "${_admin_pp}" ]]; then
    echo "[release] REFUSING: prepaid hub secret must differ from coordinator admin token" >&2
    exit 1
  fi
  printf '%s' "${ORDERS_PREPAID_HUB_SECRET}" >"${WIN_DIR}/pool.orders.prepaid_hub.secret"
  printf '%s' "${ORDERS_PREPAID_HUB_SECRET}" >"${LINUX_DIR}/pool.orders.prepaid_hub.secret"
  chmod 600 "${WIN_DIR}/pool.orders.prepaid_hub.secret" "${LINUX_DIR}/pool.orders.prepaid_hub.secret" 2>/dev/null || true
  echo "[release] wrote pool.orders.prepaid_hub.secret (${#ORDERS_PREPAID_HUB_SECRET} chars) — distinct from publish token"
else
  echo "[release] NOTE: no prepaid hub secret — D1 minting hub needs hub-signed tickets (admin mint or set HACKME_RELEASE_ORDERS_PREPAID_HUB_SECRET)"
fi

cat > "${DIST_DIR}/BUILD_INFO.txt" <<EOF
app=${APP_NAME}
version=${VERSION}
commit=${COMMIT_SHA}
build_date_utc=${BUILD_DATE_UTC}
windows_arch=${WIN_ARCH}
linux_arch=${LINUX_ARCH}
cgo_enabled=${CGO_ENABLED}
EOF

# B2B CLI before zip/installer so Orders profile ships hackme-fuzzing*.exe in-folder.
if [[ -x "${ROOT_DIR}/scripts/release/build_fuzzing_cli.sh" ]]; then
  VERSION="${VERSION}" DIST_DIR="${DIST_DIR}" bash "${ROOT_DIR}/scripts/release/build_fuzzing_cli.sh" || \
    echo "[release] WARN: hackme-fuzzing CLI build failed" >&2
  if [[ -f "${DIST_DIR}/hackme-fuzzing-${VERSION}-windows-amd64.exe" ]]; then
    cp -f "${DIST_DIR}/hackme-fuzzing-${VERSION}-windows-amd64.exe" "${WIN_DIR}/hackme-fuzzing.exe"
  fi
  if [[ -f "${DIST_DIR}/hackme-fuzzing-build-${VERSION}-windows-amd64.exe" ]]; then
    cp -f "${DIST_DIR}/hackme-fuzzing-build-${VERSION}-windows-amd64.exe" "${WIN_DIR}/hackme-fuzzing-build.exe"
  fi
  if [[ -f "${DIST_DIR}/hackme-fuzzing-${VERSION}-linux-amd64" ]]; then
    cp -f "${DIST_DIR}/hackme-fuzzing-${VERSION}-linux-amd64" "${LINUX_DIR}/hackme-fuzzing"
    chmod +x "${LINUX_DIR}/hackme-fuzzing"
  fi
  if [[ -f "${DIST_DIR}/hackme-fuzzing-build-${VERSION}-linux-amd64" ]]; then
    cp -f "${DIST_DIR}/hackme-fuzzing-build-${VERSION}-linux-amd64" "${LINUX_DIR}/hackme-fuzzing-build"
    chmod +x "${LINUX_DIR}/hackme-fuzzing-build"
  fi
fi

(
  cd "${DIST_DIR}"
  zip -r "hackme_${VERSION}_windows.zip" "windows" >/dev/null
  # Flat zip: portable fallback (installer is primary on hackme.tech/downloads).
  (cd "windows" && zip -r "../hackme_${VERSION}_windows_setup.zip" . >/dev/null)
  tar -czf "hackme_${VERSION}_linux.tar.gz" "linux"
)

if [[ -f "${ROOT_DIR}/scripts/release/windows/build_installer.sh" ]]; then
  bash "${ROOT_DIR}/scripts/release/windows/build_installer.sh" "${VERSION}" || true
fi
for win_launcher in Install-HackMe.ps1 HackMe-Install.cmd; do
  if [[ -f "${ROOT_DIR}/scripts/release/windows/${win_launcher}" ]]; then
    cp -f "${ROOT_DIR}/scripts/release/windows/${win_launcher}" "${DIST_DIR}/${win_launcher}"
  fi
done

(
  cd "${DIST_DIR}"
  SUM_FILES=( "hackme_${VERSION}_windows.zip" "hackme_${VERSION}_windows_setup.zip" "hackme_${VERSION}_linux.tar.gz" )
  if [[ -f "HackMe-Setup-${VERSION}.exe" ]]; then
    SUM_FILES+=( "HackMe-Setup-${VERSION}.exe" )
    echo "[release] Windows installer: HackMe-Setup-${VERSION}.exe"
  fi
  [[ -f Install-HackMe.ps1 ]] && SUM_FILES+=( "Install-HackMe.ps1" )
  [[ -f HackMe-Install.cmd ]] && SUM_FILES+=( "HackMe-Install.cmd" )
  for f in \
    "hackme-fuzzing-${VERSION}-linux-amd64" \
    "hackme-fuzzing-${VERSION}-windows-amd64.exe" \
    "hackme-fuzzing-build-${VERSION}-linux-amd64" \
    "hackme-fuzzing-build-${VERSION}-windows-amd64.exe"; do
    [[ -f "$f" ]] && SUM_FILES+=( "$f" )
  done
  if [[ -f "HackMe-Setup-${VERSION}.exe" ]]; then
    : # already logged
  else
    echo "[release] WARN: HackMe-Setup-${VERSION}.exe not built (install iscc or use Docker for Inno Setup)" >&2
  fi
  sha256sum "${SUM_FILES[@]}" > "SHA256SUMS.txt"
)

# Optional: bootable HackMe OS ISO (built separately; symlink legacy Miner name → OS name).
ISO_OS="HackMe-OS-${VERSION}-amd64.iso"
ISO_LEGACY="HackMe-Miner-${VERSION}-amd64.iso"
if [[ -f "${DIST_DIR}/${ISO_LEGACY}" && ! -f "${DIST_DIR}/${ISO_OS}" ]]; then
  ln -sf "${ISO_LEGACY}" "${DIST_DIR}/${ISO_OS}"
  echo "[release] ISO: symlink ${ISO_OS} → ${ISO_LEGACY}"
fi
if [[ -f "${DIST_DIR}/${ISO_OS}" ]]; then
  # Drop stale root-owned checksum files from prior ISO builds so release can rewrite.
  rm -f "${DIST_DIR}/SHA256SUMS-iso.txt" 2>/dev/null || true
  (cd "${DIST_DIR}" && sha256sum "$(basename "${ISO_OS}")" > SHA256SUMS-iso.txt)
  echo "[release] HackMe OS ISO: ${ISO_OS} ($(du -h "${DIST_DIR}/${ISO_OS}" | awk '{print $1}'))"
elif [[ -f "${DIST_DIR}/${ISO_LEGACY}" ]]; then
  rm -f "${DIST_DIR}/SHA256SUMS-iso.txt" 2>/dev/null || true
  (cd "${DIST_DIR}" && sha256sum "$(basename "${ISO_LEGACY}")" > SHA256SUMS-iso.txt)
  echo "[release] HackMe OS ISO (legacy name): ${ISO_LEGACY}"
else
  echo "[release] ISO not in dist yet (run: VERSION=${VERSION} bash scripts/release/iso/build_hackme_miner_iso.sh)" >&2
fi

WIN_INSTALLER="${DIST_DIR}/HackMe-Setup-${VERSION}.exe"
WIN_ARCHIVE="${DIST_DIR}/hackme_${VERSION}_windows.zip"
LINUX_ARCHIVE="${DIST_DIR}/hackme_${VERSION}_linux.tar.gz"
if [[ -f "${WIN_INSTALLER}" ]]; then
  WIN_PRIMARY="${WIN_INSTALLER}"
  WIN_SHA="$(awk '/HackMe-Setup-.*\.exe$/ {print $1}' "${DIST_DIR}/SHA256SUMS.txt")"
else
  WIN_PRIMARY="${DIST_DIR}/hackme_${VERSION}_windows_setup.zip"
  WIN_SHA="$(awk '/windows_setup\.zip$/ {print $1}' "${DIST_DIR}/SHA256SUMS.txt")"
fi
LINUX_SHA="$(awk '/linux\.tar\.gz$/ {print $1}' "${DIST_DIR}/SHA256SUMS.txt")"
WIN_SIZE="$(stat -c%s "${WIN_PRIMARY}")"
LINUX_SIZE="$(stat -c%s "${LINUX_ARCHIVE}")"
ISO_FILE=""
ISO_SHA=""
ISO_SIZE=0
if [[ -f "${DIST_DIR}/${ISO_OS}" ]]; then
  ISO_FILE="${ISO_OS}"
elif [[ -f "${DIST_DIR}/${ISO_LEGACY}" ]]; then
  ISO_FILE="${ISO_LEGACY}"
fi
if [[ -n "$ISO_FILE" && -f "${DIST_DIR}/${ISO_FILE}" ]]; then
  ISO_SIZE="$(stat -c%s "${DIST_DIR}/${ISO_FILE}")"
  if [[ -f "${DIST_DIR}/SHA256SUMS-iso.txt" && -w "${DIST_DIR}/SHA256SUMS-iso.txt" ]]; then
    ISO_SHA="$(awk -v f="$ISO_FILE" '$2==f || $2 ~ f {print $1; exit}' "${DIST_DIR}/SHA256SUMS-iso.txt")"
  else
    ISO_SHA="$(sha256sum "${DIST_DIR}/${ISO_FILE}" | awk '{print $1}')"
  fi
fi
jq -nc \
  --arg app "$APP_NAME" \
  --arg version "$VERSION" \
  --arg commit "$COMMIT_SHA" \
  --arg build_date_utc "$BUILD_DATE_UTC" \
  --arg windows_file "$(basename "${WIN_PRIMARY}")" \
  --arg windows_sha256 "$WIN_SHA" \
  --argjson windows_size_bytes "$WIN_SIZE" \
  --arg linux_file "$(basename "${LINUX_ARCHIVE}")" \
  --arg linux_sha256 "$LINUX_SHA" \
  --argjson linux_size_bytes "$LINUX_SIZE" \
  --argjson has_installer "$( [[ -f "${WIN_INSTALLER}" ]] && echo true || echo false )" \
  --arg iso_file "$ISO_FILE" \
  --arg iso_sha256 "$ISO_SHA" \
  --argjson iso_size_bytes "$ISO_SIZE" \
  '{
    app:$app,
    version:$version,
    commit:$commit,
    build_date_utc:$build_date_utc,
    windows_installer:$has_installer,
    hackme_os_iso: ($iso_file != ""),
    artifacts: (
      [
        {platform:"windows",file:$windows_file,sha256:$windows_sha256,size_bytes:$windows_size_bytes,kind:(if $has_installer then "installer" else "zip" end)},
        {platform:"linux",file:$linux_file,sha256:$linux_sha256,size_bytes:$linux_size_bytes}
      ]
      + (if $iso_file != "" then [{platform:"hackme-os",file:$iso_file,sha256:$iso_sha256,size_bytes:$iso_size_bytes,kind:"iso",features:["zero_knowledge_start","live_usb"]}] else [] end)
    )
  }' > "${DIST_DIR}/RELEASE_MANIFEST.json"

bash "${ROOT_DIR}/scripts/release/verify_artifacts.sh" "${DIST_DIR}"
bash "${ROOT_DIR}/scripts/release/smoke_artifacts.sh" "${DIST_DIR}"

# L1 update channel: latest.json (site CDN + GitHub release asset)
VERSION="${VERSION}" bash "${ROOT_DIR}/scripts/release/generate_latest_json.sh" "${DIST_DIR}" || \
  echo "[release] WARN: latest.json generation failed" >&2
if [[ "${BUILD_DEB:-0}" == "1" ]]; then
  VERSION="${VERSION}" bash "${ROOT_DIR}/scripts/release/linux/build_deb_from_dist.sh" "${DIST_DIR}" || \
    echo "[release] WARN: deb build failed" >&2
fi

echo "[release] done"
echo "[release] artifacts:"
echo "  ${DIST_DIR}/hackme_${VERSION}_windows.zip"
echo "  ${DIST_DIR}/hackme_${VERSION}_windows_setup.zip  (flat zip — advanced)"
if [[ -f "${DIST_DIR}/HackMe-Setup-${VERSION}.exe" ]]; then
  echo "  ${DIST_DIR}/HackMe-Setup-${VERSION}.exe  (installer — Miner | Orders profiles)"
fi
echo "  ${DIST_DIR}/hackme_${VERSION}_linux.tar.gz"
echo "  ${DIST_DIR}/SHA256SUMS.txt"
echo "  ${DIST_DIR}/RELEASE_MANIFEST.json"
echo "  ${DIST_DIR}/latest.json"
echo "  ${DIST_DIR}/SMOKE_REPORT.txt"
