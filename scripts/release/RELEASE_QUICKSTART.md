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

## Linux

1. Extract `hackme_*_linux.tar.gz`.
2. `bash install_hackme.sh` (optional systemd) or `bash desktop_mode_up.sh` from the `linux/` folder.
3. Default `.env.desktop` uses **worker** profile and `https://hackme.tech` as public authority.
4. **NVIDIA GPU:** install CUDA toolkit 12.x, then `bash build_cuda_worker.sh` and `export HACKME_GPU_BACKEND=cuda` (see `CUDA_PRODUCTION.md` in bundle).
5. **AMD / Intel:** `export HACKME_GPU_BACKEND=opencl` and use `workerpoh-opencl` (see `GPU_MINING_BACKENDS.md`).
6. Dashboard → Mining → Start worker, or `bash desktop_worker_reset.sh` on desktop rigs.

## Downloads on the site

Checksums: `SHA256SUMS.txt` next to the archives on https://hackme.tech
