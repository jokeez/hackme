# Windows: one-click mining (public pool hackme.tech)

## For miner (recommended - installer)

1. Download **`HackMe-Setup-<version>.exe`** from https://hackme.tech/downloads.html  
   (not the `HackMe` folder from GitHub sources).
2. Run the installer → **Next** → installation in `C:\Program Files\HackMe` (default).
3. Master himself:
   - copies `hackme.exe`, `workerpoh`, pool token;
   - will create `hackme.env` and a **HackMe Miner** shortcut on the desktop;
   - writes the installation path to the registry (`HKLM\Software\HackMe Network\HackMe`).
4. At the end, mark **Start HackMe Miner** - the browser will open on the **Mining** tab.
5. Keep the **HackMe Miner** window open (node ​​is spinning in it). The worker starts on its own in ~10 s.
6. **Watchdog:** if the GPU/worker fails - `watchdog_pool_worker.bat` and node will restart mining automatically (log: `logs\watchdog_worker.log`).

**The pool token is not needed manually** - it is in `pool.miner.token` inside the installer.

## Alternative: ZIP (advanced)

1. Download **`hackme_*_windows_setup.zip`** (flat archive).
2. Extract to any folder → run **`setup_hackme_miner.bat`** once.
3. **`Start HackMe Miner.bat`** - as in the installer.

## Delete

**Windows Settings → Applications → HackMe → Uninstall**,  
or Start menu → HackMe → Uninstall.

## If you ask for admin token in the dashboard

Copy the line `HACKME_ADMIN_TOKEN=...` from `hackme.env` (in the installation folder).

## For the pool operator (kapa)

```bash
bash scripts/ops/rollout_coordinator_worker_token.sh   # when rotating the token
VERSION=0.1.0-rc15 bash scripts/release/make_release_bundle.sh
```

The following are posted on the site:
- **`HackMe-Setup-<ver>.exe`** — main download for Windows;
- zip is a backup option.

Build `.exe` on Linux: Docker `amake/innosetup` (see `scripts/release/windows/build_installer.sh`).
