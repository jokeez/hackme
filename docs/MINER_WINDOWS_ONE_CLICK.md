# Windows: one-click mining (public pool hackme.tech)

## For miner (recommended - installer)

1. Download **`HackMe-Setup-<version>.exe`** from https://hackme.tech/downloads.html  
   (not the `HackMe` folder from GitHub sources).
2. Run the installer → **Next** → default install is **`%LOCALAPPDATA%\HackMe`** (no admin required; `PrivilegesRequired=lowest`).  
   Prefer this path. Do not look for `C:\Program Files\HackMe` unless you deliberately installed there with an older build.
3. The installer:
   - copies `hackme.exe`, `workerpoh` / `workerpoh-opencl`, `workerfuzz`, pool token;
   - creates `hackme.env` and a **HackMe Miner** shortcut on the desktop;
   - writes uninstall/registry entries under HKCU (per-user).
4. At the end, mark **Start HackMe Miner** — the browser opens on the **Mining** tab.
5. Keep the **HackMe Miner** window open (node is spinning in it). The worker starts on its own in ~10 s.
6. **Watchdog:** if the GPU/worker fails — `watchdog_pool_worker.bat` and node will restart mining automatically (log: `logs\watchdog_worker.log`).

**What Windows runs OOTB:** PoH mining + **Dig** (`workerfuzz.exe`). Hunt coverage engines (libFuzzer / AFL++ / bubblewrap) are **Linux-only** — the Windows bundle does not install them and does not promise Hunt engine pay; Hunt leases soft-skip while Dig/PoH continue. Publish Hunt from Orders if you want Linux pool miners to run coverage.

**The pool token is not needed manually** — it is in `pool.miner.token` inside the installer.

**If hashrate stays 0 / claims 403 `payout_address_locked`:** the hostname `WORKER_ID` was reused with a new miner seed. Set a new `WORKER_ID=` in `hackme.env` (or re-run `write_hackme_env.ps1`, which mints a unique suffix on fresh installs) and restart the miner. Keep the same seed if you only meant to upgrade in place.

## Alternative: ZIP (advanced)

1. Download **`hackme_*_windows_setup.zip`** (flat archive).
2. Extract to any folder → run **`setup_hackme_miner.bat`** once.  
   Automation: `set HACKME_SETUP_NONINTERACTIVE=1` before the bat (skips `pause`).
3. **`Start HackMe Miner.bat`** — as in the installer.

## Delete

**Windows Settings → Applications → HackMe → Uninstall**,  
or Start menu → HackMe → Uninstall.

## If you ask for admin token in the dashboard

Copy the line `HACKME_ADMIN_TOKEN=...` from `hackme.env` (in the installation folder).

## For the pool operator (kapa)

```bash
bash scripts/ops/rollout_coordinator_worker_token.sh   # when rotating the token
VERSION=0.1.0-rc18.1 bash scripts/release/make_release_bundle.sh
```

The following are posted on the site:
- **`HackMe-Setup-<ver>.exe`** — main download for Windows;
- zip is a backup option.

Build `.exe` on Linux: Docker `amake/innosetup` (see `scripts/release/windows/build_installer.sh`).
