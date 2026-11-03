# HackMe release — public pool quick start

Local-only solo mining was removed. Use the **public pool** at `https://hackme.tech`.

## Windows

**Primary:** download **`HackMe-Setup-*.exe`** from https://hackme.tech/downloads.html (installs under `%LOCALAPPDATA%\HackMe`).

1. Run Setup → Start Menu **HackMe Miner** (`start_hackme_miner.bat`).
2. Browser opens → **Mining** tab; PoH + Dig (`workerfuzz.exe`) autostart on the public pool.
3. **Orders (B2B Dig):** Start Menu **HackMe Orders** (`start_hackme_orders.bat`) — local node + `#orders`, no mining worker. Paste a scoped publish token into `pool.orders.token` (copy from `pool.orders.token.example`). Settle-pull is forced ON for Orders so escrow can close.
4. **Update:** `update_hackme_miner.bat` / `.ps1` — prefers Setup.exe from `latest.json`, keeps `hackme.env`.

**Zip fallback:** `hackme_*_windows_setup.zip` → `setup_hackme_miner.bat` then `start_hackme_miner.bat`.

Windows OOTB = **Dig WASM + PoH only**. Hunt coverage engines (libFuzzer / AFL++ / bwrap) are **Linux-only** — soft-skip on Windows, Dig/PoH continue. See `docs/MINER_WINDOWS_ONE_CLICK.md`.

Binaries: `hackme.exe` (node), `workerpoh.exe` (pool worker), `workerfuzz.exe` (Dig), `minersign.exe` (hybrid signer).

## Linux (one click — recommended)

1. Download **`hackme_*_linux.tar.gz`** from https://hackme.tech/downloads.html and verify `SHA256SUMS.txt`.
2. `tar -xzf hackme_*_linux.tar.gz && cd linux`
3. Run **`bash start_hackme_miner.sh`** — pool token is in `pool.miner.token`; setup runs once automatically.
4. Browser opens the local dashboard; mining on **hackme.tech** starts without editing env files.
5. **Orders (B2B Dig/Hunt):** `bash start_hackme_orders.sh` — local node + `#orders` (no mining worker). CLI: `./hackme-fuzzing` in the same folder.

**Advanced:** `sudo bash install_hackme.sh --archive ../hackme_*_linux.tar.gz` (systemd). Dev tree: `bash desktop_mode_up.sh` (needs Go).

**Update (L1):** `bash update_hackme_miner.sh` — fetches `latest.json`, verifies SHA256, replaces binaries, keeps `.env` / `data` / `logs`. HackMe OS: `bash update_hackme_os_binaries.sh`.

**Menu icon:** system install (`install_hackme.sh` / `.deb`) adds **HackMe** + **HackMe Dashboard** entries with the branded logo.

**GPU:** Linux bundle includes ready binaries `bin/workerpoh-cuda` + `bin/workerpoh-opencl` (+ `lib/libnvrtc*` for CUDA). No Go toolchain needed. NVIDIA needs a working driver (`nvidia-smi`); AMD/Intel use OpenCL ICD. Setup writes `HACKME_GPU_BACKEND=auto|cuda|opencl|cpu` only when that backend is actually usable. See `GPU_MINING_BACKENDS.md`. Multi-GPU: `bash worker_autostart.sh` after `bin/fleetplan`.

## Downloads on the site

Checksums: `SHA256SUMS.txt` next to the archives on https://hackme.tech. Update feed: `https://hackme.tech/dist/latest.json` (after publish).
