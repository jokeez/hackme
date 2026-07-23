# Checklist: second PC (Windows) and public pool

Goal: **not a separate chain on each PC**, but **participation in a pool** (VPS = canon + coordinator). Local “beginner solo” does not fit into the network.

## 1. What to download

- The same **release channel** as on the VPS (for example `release_0.1.0-rc8`), so that the protocol and behavior are the same.
- In the folder with **`hackme.exe`** there should be **`workerpoh.exe`** (native worker for Windows; the dashboard runs it instead of a bash script), **`start_hackme_dashboard.bat`**, optionally **`start_hackme_public_pool.bat`** + **`env.public_pool.example`** (template `hackme.env` / `.env` with `HACKME_PUBLIC_AUTHORITY_BASE`). Scripts in the repository: `scripts/release/windows/`.

## 2. Launch on a second PC

1. Unpack the zip **into one folder** (next to `hackme.exe` and bat).
2. Launch **`start_hackme_dashboard.bat`** (or the **HackMe Miner** shortcut after installing via `HackMe-Setup.exe`).
3. In the browser `http://127.0.0.1:8080` → **header**: insert **admin token** (the same secret class that your node accepts at `HACKME_REQUIRE_ADMIN_TOKEN`, otherwise POST will not go through).
4. **Mining** → **Start worker** (or Desktop master in worker mode): specify **`COORD_URL`** public coordinator (as on a VPS, often HTTPS + path `/pool/coordinator` - see nginx snippets in the repo).
5. You need a **coordinator token**: from the variables on the VPS / what the pool operator issued (`HACKME_POOL_COORDINATOR_TOKEN` or admin token, if so configured) - otherwise the API will return `412 coordinator_token_required`.

## 3. Environment (preferably before the first launch)

Next to **`hackme.exe`** you can put the file **`.env`** or **`hackme.env`**: when starting, the node will pick up the variables from there, **without overwriting** those already set in the system. Convenient for `HACKME_PUBLIC_AUTHORITY_BASE` and `HACKME_POOL_COORDINATOR_TOKEN` without manually setting them in “Windows environment variables”.

On the second PC in **system variables** or `.env` next to the process (as is customary on your VPS):

- **`HACKME_PUBLIC_AUTHORITY_BASE`** = base URL **command node** with VPS (as in `README.md`) so that the wallet/height in the UI matches the non-P2P network.
- If necessary, explicitly: **`HACKME_POOL_COORDINATOR_URL`**, **`HACKME_CANONICAL_CHAIN_URL`** - see the main `README.md`, Worker-mode section.

## 4. Expectations

- **Local `tip_height` in SQLite** on the second PC may **lag** behind the network - this is normal without P2P; pool and "canon" in the API must be consistent, see `docs/NETWORK_MODEL.md` and `scripts/ops/verify_chain_sync_snapshot.sh`.
- The balance on the second PC in worker-mode **does not** have to grow as with a local PoH block - accruals go through **coordinator / settlement**; see tips on the Mining / Wallet tab in the UI.

## 5. Check after switching on

From a second PC (or VPS):

```bash
LOCAL_BASE=http://127.0.0.1:8080 bash scripts/ops/verify_chain_sync_snapshot.sh
```

(on Windows - via Git Bash / WSL, or transfer the logic manually: compare `pool_target_mod` and `global work.target_mod`.)

## 6. VPS and website (does operator with access)

- Upload the **same** build/zip that you are testing locally; restart the systemd services of the node and coordinator.
- Nginx/TLS: current snippets in the repository (`scripts/ops/nginx/`, `tmp/hackme-site-domain.conf`, etc.) - apply on the server and `nginx -t && reload`.
- On the Downloads page - a link and **checksum** to the artifact from the CI/release.

Bottom line: **yes, everything can be fine** if you don’t wait for a “separate chain” on the second PC, but configure **worker + tokens + authority/coordinator URL** as in the main README; exe must be **the same version** as the pool on the VPS.

## 7. Perfect run (repo → VPS → Windows)

The procedure to avoid running “outdated exe” and not catching protocol out of sync:

1. **On a machine with a repository (Linux/macOS/WSL):**
   `bash scripts/ops/repo_final_selfcheck.sh`  
Optionally deeper: `RUN_LOCAL_STACK_SMOKE=1 bash scripts/ops/repo_final_selfcheck.sh` (short stack coordinator+node+worker).
2. **Collect/take the same release zip** that will go to the VPS and Downloads; commit the /commit tag in the release notes.
3. **VPS:** roll out the artifact, restart services, `nginx -t && reload`, check public GET (`/api/status`, if necessary `GET /api/worker/settlement`).
4. **Windows (second PC):** unpack **same** zip → `start_hackme_dashboard.bat` → admin token → **Start pool worker** with coordinator URL and VPS token.
5. **Reconciliation:** compare the version/build in the UI or logs with the VPS; in case of doubt - again `verify_chain_sync_snapshot.sh` (see §5).
