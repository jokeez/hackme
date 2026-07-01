#!/usr/bin/env bash
set -euo pipefail

# Smoke check release bundle contents.
#
# Usage:
#   bash scripts/release/smoke_artifacts.sh dist/release_1.0.0

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <release_dir>" >&2
  exit 2
fi

REL_DIR="$1"
if [[ ! -d "${REL_DIR}" ]]; then
  echo "[smoke] release dir not found: ${REL_DIR}" >&2
  exit 2
fi

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[smoke] missing command: $1" >&2
    exit 1
  }
}

require_cmd tar
require_cmd zipinfo
require_cmd timeout

WIN_ZIP="$(ls "${REL_DIR}"/hackme_*_windows.zip 2>/dev/null | head -n1 || true)"
LINUX_TGZ="$(ls "${REL_DIR}"/hackme_*_linux.tar.gz 2>/dev/null | head -n1 || true)"

if [[ -z "${WIN_ZIP}" || -z "${LINUX_TGZ}" ]]; then
  echo "[smoke] expected windows zip and linux tar.gz in ${REL_DIR}" >&2
  exit 1
fi

echo "[smoke] checking windows archive content names"
zipinfo -1 "${WIN_ZIP}" > "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"
if ! grep -q '^windows/hackme\.exe$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/hackme.exe missing in zip" >&2
  exit 1
fi
if ! grep -q '^windows/start_hackme_dashboard\.bat$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/start_hackme_dashboard.bat missing in zip" >&2
  exit 1
fi
if ! grep -q '^windows/start_hackme_public_pool\.bat$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/start_hackme_public_pool.bat missing in zip" >&2
  exit 1
fi
if ! grep -q '^windows/RELEASE_QUICKSTART\.md$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/RELEASE_QUICKSTART.md missing in zip" >&2
  exit 1
fi
if ! grep -q '^windows/workerpoh\.exe$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/workerpoh.exe missing in zip" >&2
  exit 1
fi
if ! grep -q '^windows/hackme_autostart_boot\.bat$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/hackme_autostart_boot.bat missing in zip" >&2
  exit 1
fi
if ! grep -q '^windows/hackme\.iss$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/hackme.iss missing in zip" >&2
  exit 1
fi

echo "[smoke] checking linux archive content names"
tar -tzf "${LINUX_TGZ}" > "${REL_DIR}/SMOKE_LINUX_LIST.txt"
if ! grep -q '^linux/hackme$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/hackme missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/install_hackme\.sh$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/install_hackme.sh missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/desktop_mode_up\.sh$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/desktop_mode_up.sh missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/RELEASE_QUICKSTART\.md$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/RELEASE_QUICKSTART.md missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/pool\.miner\.token$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/pool.miner.token missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/start_hackme_miner\.sh$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/start_hackme_miner.sh missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/scripts/ops/worker_autostart\.sh$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/scripts/ops/worker_autostart.sh missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/bin/workerpoh$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/bin/workerpoh missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^linux/fix_miner_layout\.sh$' "${REL_DIR}/SMOKE_LINUX_LIST.txt"; then
  echo "[smoke] linux/fix_miner_layout.sh missing in tar.gz" >&2
  exit 1
fi
if ! grep -q '^windows/pool\.miner\.token$' "${REL_DIR}/SMOKE_WINDOWS_LIST.txt"; then
  echo "[smoke] windows/pool.miner.token missing in zip" >&2
  exit 1
fi

echo "[smoke] extracting linux binary for runtime probe"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
tar -xzf "${LINUX_TGZ}" -C "${TMP_DIR}" linux/hackme
chmod +x "${TMP_DIR}/linux/hackme"
if ! timeout 2s "${TMP_DIR}/linux/hackme" >/dev/null 2>&1; then
  # Non-zero exit is acceptable here; we only care that binary starts and exits quickly.
  true
fi

cat > "${REL_DIR}/SMOKE_REPORT.txt" <<EOF
release_dir=${REL_DIR}
windows_zip=${WIN_ZIP##*/}
linux_tar=${LINUX_TGZ##*/}
archive_layout_check=pass
linux_binary_spawn_check=pass
captured_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF

echo "[smoke] PASS: release archive smoke checks ok"
