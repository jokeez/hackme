#!/usr/bin/env bash
# Install CUDA dev for HackMe native worker (Ubuntu 22.04/24.04).
# RTX 50 / Blackwell (CC 12.0) needs CUDA 12.8+ NVRTC — Ubuntu's 12.0 may be too old.
#
#   cd ~/Desktop/HackMe && sudo bash scripts/ops/install_cuda_dev_ubuntu.sh
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run with sudo: sudo bash $0" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

DISABLED=()
restore_apt_sources() {
  local f
  for f in "${DISABLED[@]}"; do
    [[ -f "${f}.hackme-disabled" ]] && mv -f "${f}.hackme-disabled" "$f"
  done
}
trap restore_apt_sources EXIT

for f in /etc/apt/sources.list.d/v2raya.list; do
  [[ -f "$f" ]] || continue
  echo "[cuda-install] temporarily disabling: $f"
  mv -f "$f" "${f}.hackme-disabled"
  DISABLED+=("$f")
done

apt_get_update() {
  apt-get update -qq || apt-get update -qq \
    -o Dir::Etc::sourcelist="sources.list" \
    -o Dir::Etc::sourceparts="-" 2>/dev/null || apt-get update
}

nvrtc_present() {
  [[ -f /usr/include/nvrtc.h ]] || \
    [[ -f /usr/local/cuda/include/nvrtc.h ]] || \
    [[ -f /usr/local/cuda-12.8/include/nvrtc.h ]] || \
    [[ -f /usr/lib/cuda/include/nvrtc.h ]]
}

install_cuda128_nvidia_repo() {
  echo "[cuda-install] NVIDIA CUDA 12.8 repo (recommended for RTX 5060 Ti / compute_12.0)..."
  local base="https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2404/x86_64"
  local keyring="cuda-keyring_1.1-1_all.deb"
  local codename
  codename="$(. /etc/os-release 2>/dev/null && echo "${VERSION_ID:-2404}" | tr -d .)"
  case "$codename" in
    2204) base="https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64" ;;
    2404|*) base="https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2404/x86_64" ;;
  esac
  if ! dpkg -l cuda-keyring 2>/dev/null | grep -q ^ii; then
    apt-get install -y wget ca-certificates
    wget -q "${base}/${keyring}" -O "/tmp/${keyring}"
    dpkg -i "/tmp/${keyring}"
  fi
  apt_get_update
  # Meta-package; includes NVRTC + headers for Blackwell
  apt-get install -y cuda-toolkit-12-8 || apt-get install -y cuda-toolkit-12-6
  if [[ -d /usr/local/cuda-12.8 ]]; then
    ln -sfn /usr/local/cuda-12.8 /usr/local/cuda
  fi
}

install_cuda_ubuntu_multiverse() {
  echo "[cuda-install] Ubuntu multiverse nvidia-cuda-dev (CUDA 12.0 — older NVRTC)..."
  apt_get_update
  apt-get install -y nvidia-cuda-dev build-essential libnvrtc12 || true
  if [[ -d /usr/lib/cuda && ! -e /usr/local/cuda ]]; then
    ln -sf /usr/lib/cuda /usr/local/cuda
  fi
}

apt_get_update

if nvrtc_present; then
  echo "[cuda-install] nvrtc.h already installed"
else
  # Prefer NVIDIA 12.8 for Blackwell; fall back to Ubuntu 12.0 dev package
  if ! install_cuda128_nvidia_repo 2>/dev/null; then
    echo "[cuda-install] NVIDIA 12.8 repo install failed — trying Ubuntu nvidia-cuda-dev..." >&2
    install_cuda_ubuntu_multiverse || true
  fi
fi

if ! nvrtc_present; then
  echo "[cuda-install] FAIL: nvrtc.h not found after install." >&2
  echo "  Manual (Noble 24.04):" >&2
  echo "    wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2404/x86_64/cuda-keyring_1.1-1_all.deb" >&2
  echo "    sudo dpkg -i cuda-keyring_1.1-1_all.deb && sudo apt-get update" >&2
  echo "    sudo apt-get install -y cuda-toolkit-12-8" >&2
  exit 1
fi

echo "[cuda-install] OK: $(ls -1 /usr/include/nvrtc.h /usr/local/cuda*/include/nvrtc.h 2>/dev/null | head -1)"
echo "[cuda-install] done. Next:"
echo "  bash $ROOT/scripts/ops/build_cuda_worker.sh --probe"
echo "  bash $ROOT/scripts/ops/apply_desktop_gpu_pool.sh"
