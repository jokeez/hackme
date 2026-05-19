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
  go build -trimpath -ldflags "-s -w" -o "${LINUX_DIR}/workerpoh" ./cmd/workerpoh
GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED="${CGO_ENABLED}" \
  go build -trimpath -ldflags "-s -w" -o "${WIN_DIR}/workerpoh.exe" ./cmd/workerpoh
if [[ "${CGO_ENABLED}" == "1" ]] && (pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]); then
  echo "[release] building workerpoh-opencl (AMD/Intel/NVIDIA via OpenCL ICD)"
  GOOS=linux GOARCH="${LINUX_ARCH}" CGO_ENABLED=1 \
    go build -tags opencl -trimpath -ldflags "-s -w" -o "${LINUX_DIR}/workerpoh-opencl" ./cmd/workerpoh
  if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
    echo "[release] building workerpoh-opencl.exe (mingw cross)"
    CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
      GOOS=windows GOARCH="${WIN_ARCH}" CGO_ENABLED=1 \
      go build -tags opencl -trimpath -ldflags "-s -w" -o "${WIN_DIR}/workerpoh-opencl.exe" ./cmd/workerpoh
  else
    echo "[release] WARN: skip workerpoh-opencl.exe (install mingw-w64 for Windows OpenCL build)" >&2
  fi
fi
if [[ -x "${ROOT_DIR}/scripts/ops/build_cuda_worker.sh" ]]; then
  echo "[release] building workerpoh-cuda (NVIDIA native; Linux only)"
  if bash "${ROOT_DIR}/scripts/ops/build_cuda_worker.sh" 2>/dev/null; then
    cp -f "${ROOT_DIR}/bin/workerpoh-cuda" "${LINUX_DIR}/workerpoh-cuda"
    chmod +x "${LINUX_DIR}/workerpoh-cuda"
    ln -sf workerpoh-cuda "${LINUX_DIR}/workerpoh-gpu" 2>/dev/null || true
  else
    echo "[release] WARN: workerpoh-cuda build skipped (no CUDA toolkit on build host)" >&2
  fi
fi
for doc in docs/GPU_MINING_BACKENDS.md docs/CUDA_PRODUCTION.md; do
  [[ -f "${ROOT_DIR}/${doc}" ]] && cp "${ROOT_DIR}/${doc}" "${LINUX_DIR}/"
done
for op in build_gpu_workers.sh build_cuda_worker.sh detect_gpu_backend.sh desktop_worker_reset.sh cuda_env.sh setup_cuda_desktop.sh; do
  [[ -f "${ROOT_DIR}/scripts/ops/${op}" ]] && cp "${ROOT_DIR}/scripts/ops/${op}" "${LINUX_DIR}/"
done

cp "${ROOT_DIR}/README.md" "${WIN_DIR}/README.md"
cp "${ROOT_DIR}/README.md" "${LINUX_DIR}/README.md"
cp "${ROOT_DIR}/docs/EXPLORER_SUBDOMAIN_RUNBOOK.md" "${WIN_DIR}/EXPLORER_SUBDOMAIN_RUNBOOK.md"
cp "${ROOT_DIR}/docs/EXPLORER_SUBDOMAIN_RUNBOOK.md" "${LINUX_DIR}/EXPLORER_SUBDOMAIN_RUNBOOK.md"
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
cp "${ROOT_DIR}/scripts/release/windows/hackme.ico" "${WIN_DIR}/hackme.ico"
cp "${ROOT_DIR}/scripts/release/windows/hackme.png" "${WIN_DIR}/hackme.png"
cp "${ROOT_DIR}/scripts/release/linux/install_hackme.sh" "${LINUX_DIR}/install_hackme.sh"
cp "${ROOT_DIR}/scripts/release/linux/hackme.desktop.template" "${LINUX_DIR}/hackme.desktop.template"
cp "${ROOT_DIR}/scripts/release/linux/hackme-node.service.template" "${LINUX_DIR}/hackme-node.service.template"
cp "${ROOT_DIR}/scripts/ops/desktop_mode_up.sh" "${LINUX_DIR}/desktop_mode_up.sh"
cp "${ROOT_DIR}/scripts/ops/desktop_mode_status.sh" "${LINUX_DIR}/desktop_mode_status.sh"
cp "${ROOT_DIR}/scripts/ops/desktop_mode_stop.sh" "${LINUX_DIR}/desktop_mode_stop.sh"
cp "${ROOT_DIR}/scripts/ops/install_linux_desktop_launcher.sh" "${LINUX_DIR}/install_linux_desktop_launcher.sh"
cp "${ROOT_DIR}/scripts/ops/install_from_code_toolchains.sh" "${LINUX_DIR}/install_from_code_toolchains.sh"

chmod +x "${LINUX_DIR}/install_hackme.sh"
chmod +x "${LINUX_DIR}/desktop_mode_up.sh"
chmod +x "${LINUX_DIR}/desktop_mode_status.sh"
chmod +x "${LINUX_DIR}/desktop_mode_stop.sh"
chmod +x "${LINUX_DIR}/install_linux_desktop_launcher.sh"
chmod +x "${LINUX_DIR}/install_from_code_toolchains.sh"
chmod +x "${LINUX_DIR}/workerpoh"

cat > "${DIST_DIR}/BUILD_INFO.txt" <<EOF
app=${APP_NAME}
version=${VERSION}
commit=${COMMIT_SHA}
build_date_utc=${BUILD_DATE_UTC}
windows_arch=${WIN_ARCH}
linux_arch=${LINUX_ARCH}
cgo_enabled=${CGO_ENABLED}
EOF

(
  cd "${DIST_DIR}"
  zip -r "hackme_${VERSION}_windows.zip" "windows" >/dev/null
  tar -czf "hackme_${VERSION}_linux.tar.gz" "linux"
  sha256sum "hackme_${VERSION}_windows.zip" "hackme_${VERSION}_linux.tar.gz" > "SHA256SUMS.txt"
)

WIN_ARCHIVE="${DIST_DIR}/hackme_${VERSION}_windows.zip"
LINUX_ARCHIVE="${DIST_DIR}/hackme_${VERSION}_linux.tar.gz"
WIN_SHA="$(awk '/windows\.zip$/ {print $1}' "${DIST_DIR}/SHA256SUMS.txt")"
LINUX_SHA="$(awk '/linux\.tar\.gz$/ {print $1}' "${DIST_DIR}/SHA256SUMS.txt")"
WIN_SIZE="$(stat -c%s "${WIN_ARCHIVE}")"
LINUX_SIZE="$(stat -c%s "${LINUX_ARCHIVE}")"
jq -nc \
  --arg app "$APP_NAME" \
  --arg version "$VERSION" \
  --arg commit "$COMMIT_SHA" \
  --arg build_date_utc "$BUILD_DATE_UTC" \
  --arg windows_file "$(basename "${WIN_ARCHIVE}")" \
  --arg windows_sha256 "$WIN_SHA" \
  --argjson windows_size_bytes "$WIN_SIZE" \
  --arg linux_file "$(basename "${LINUX_ARCHIVE}")" \
  --arg linux_sha256 "$LINUX_SHA" \
  --argjson linux_size_bytes "$LINUX_SIZE" \
  '{
    app:$app,
    version:$version,
    commit:$commit,
    build_date_utc:$build_date_utc,
    artifacts:[
      {platform:"windows",file:$windows_file,sha256:$windows_sha256,size_bytes:$windows_size_bytes},
      {platform:"linux",file:$linux_file,sha256:$linux_sha256,size_bytes:$linux_size_bytes}
    ]
  }' > "${DIST_DIR}/RELEASE_MANIFEST.json"

bash "${ROOT_DIR}/scripts/release/verify_artifacts.sh" "${DIST_DIR}"
bash "${ROOT_DIR}/scripts/release/smoke_artifacts.sh" "${DIST_DIR}"

echo "[release] done"
echo "[release] artifacts:"
echo "  ${DIST_DIR}/hackme_${VERSION}_windows.zip"
echo "  ${DIST_DIR}/hackme_${VERSION}_linux.tar.gz"
echo "  ${DIST_DIR}/SHA256SUMS.txt"
echo "  ${DIST_DIR}/RELEASE_MANIFEST.json"
echo "  ${DIST_DIR}/SMOKE_REPORT.txt"
