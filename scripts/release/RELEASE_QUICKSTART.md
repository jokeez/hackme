# HackMe release — public pool quick start

Local-only solo mining was removed. Use the **public pool** at `https://hackme.tech`.

## Windows

1. Unzip `hackme_*_windows.zip` to a folder (e.g. `C:\HackMe`).
2. **Recommended:** double-click **`start_hackme_public_pool.bat`** — sets `HACKME_PUBLIC_AUTHORITY_BASE`, opens dashboard → Mining tab.
3. Or **`start_hackme_desktop_mode.bat`** — creates `.env.desktop.windows` with a generated admin token (worker profile + public authority).
4. Paste **admin token** in the dashboard (first-run wizard) and **coordinator token** from the pool operator (`hackme.env` or UI).
5. Mining tab → **Start worker**.

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
