# HackMe release — public pool quick start

Local-only solo mining was removed. Use the **public pool** at `https://hackme.tech`.

## Windows

1. Download **`hackme_*_windows_setup.zip`** (flat layout — all files in zip root).
2. Run **`setup_hackme_miner.bat`** once (installs to `C:\HackMe`, generates local admin token, applies pool miner token from `pool.miner.token`).
3. Run **`start_hackme_miner.bat`** (or desktop shortcut **Start HackMe Miner**).
4. Browser opens → **Mining** tab; worker autostarts when the node is up.
5. Legacy: **`start_hackme_public_pool.bat`** forwards to `start_hackme_miner.bat`.

See `docs/MINER_WINDOWS_ONE_CLICK.md` in the repo.

Binaries: `hackme.exe` (node), `workerpoh.exe` (pool worker), `minersign.exe` (hybrid signer).

## Linux (one click — recommended)

1. Download **`hackme_*_linux.tar.gz`** from https://hackme.tech/downloads.html and verify `SHA256SUMS.txt`.
2. `tar -xzf hackme_*_linux.tar.gz && cd linux`
3. Run **`bash start_hackme_miner.sh`** — pool token is in `pool.miner.token`; setup runs once automatically.
4. Browser opens the local dashboard; mining on **hackme.tech** starts without editing env files.

**Advanced:** `sudo bash install_hackme.sh --archive ../hackme_*_linux.tar.gz` (systemd). Dev tree: `bash desktop_mode_up.sh` (needs Go).

**GPU:** Linux bundle includes ready binaries `bin/workerpoh-cuda` + `bin/workerpoh-opencl` (+ `lib/libnvrtc*` for CUDA). No Go toolchain needed. NVIDIA needs a working driver (`nvidia-smi`); AMD/Intel use OpenCL ICD. Setup writes `HACKME_GPU_BACKEND=auto|cuda|opencl|cpu` only when that backend is actually usable. See `GPU_MINING_BACKENDS.md`. Multi-GPU: `bash worker_autostart.sh` after `bin/fleetplan`.

## Downloads on the site

Checksums: `SHA256SUMS.txt` next to the archives on https://hackme.tech
