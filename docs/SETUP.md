# Setup guide — miners and operators

One-page paths for **mining on the public pool** and **running your own desktop node**.  
Official coordinator: `https://hackme.tech/pool/coordinator`

---

## Golden path (recommended — ~2 minutes)

**One entry point per platform.** Detect GPU → start node → start worker → pool row on coordinator.

| Platform | Command |
|----------|---------|
| **Linux (dev checkout)** | `bash scripts/ops/start_pool_miner.sh` |
| **Linux (release tarball)** | `bash start_hackme_miner.sh` |
| **Windows** | Start menu → **HackMe Miner** (`start_hackme_miner.bat`) |
| **HackMe OS (USB)** | Boot — auto wallet + mining (no config) |

After start, open **http://127.0.0.1:8080/#ecosystem** — **Workers** tab shows your row with **GH/s from the coordinator** (same source as the public pool UI).

Verify locally:

```bash
bash scripts/ops/start_pool_miner.sh          # full path + wait for pool row
curl -s http://127.0.0.1:8080/api/worker/status | jq '{running,worker_id,hashrate_gh_s,coordinator_hashrate_gh_s,telemetry_source}'
```

Do **not** mix `restart_linux_desktop_worker.sh`, manual `workerpoh` CLI, and `desktop_worker_reset.sh` unless debugging — use the golden path above.

---

## Before you start

1. Clone: `git clone https://github.com/jokeez/hackme.git && cd hackme`
2. Copy env templates — **never commit** filled-in files ([SECURITY_REPO.md](SECURITY_REPO.md)):

| Role | Copy this | To |
|------|-----------|-----|
| Desktop + dashboard | [`.env.desktop.example`](../.env.desktop.example) | `.env.desktop` |
| CLI worker only | [`scripts/ops/worker.env.example`](../scripts/ops/worker.env.example) | `.env.worker` or export in shell |
| VPS coordinator | [`scripts/ops/public_pool_hardening.env.example`](../scripts/ops/public_pool_hardening.env.example) | server `.env.coord` |

3. Optional secrets dir (gitignored):

```bash
mkdir -p .secrets
# Coordinator admin (operators only) — one line, no newline noise:
# echo 'YOUR_ADMIN_TOKEN' > .secrets/hackme_coordinator_admin_token
# Worker token for rigs (from operator):
# echo 'YOUR_WORKER_TOKEN' > .secrets/hackme_coordinator_worker_token
```

4. Generate miner identity (per rig):

```bash
go build -o bin/minersign ./cmd/minersign
bin/minersign -gen-seed   # → HACKME_MINER_ED25519_SEED_HEX + HMC- address
```

---

## Path A — Linux desktop (dev)

```bash
# Edit .env.desktop: WORKER_PAYOUT_MAP=worker-$(hostname -s)=HMC-your16hex
bash scripts/ops/start_pool_miner.sh
```

- Dashboard: **http://127.0.0.1:8080**
- Stop node: `bash scripts/ops/desktop_mode_stop.sh`

GPU backend is auto-detected (`cuda` → `opencl` → `cpu`). Override with `HACKME_GPU_BACKEND=cuda` in `.env.desktop`.

---

## Path B — Windows miner

1. Download installer from [hackme.tech/downloads.html](https://hackme.tech/downloads.html)  
2. Verify **SHA256** on the same page  
3. Run **Start HackMe Miner** — pool URL is preconfigured  
4. Keep the window open; watchdog restarts the worker after GPU hiccups  

AMD **RX 580**: use build with `workerpoh-opencl.exe` (installer picks OpenCL when present).

---

## Path C — HackMe OS (live USB)

1. Download ISO + `SHA256SUMS-iso.txt` from the downloads page  
2. Verify: `bash scripts/tests/verify_hackme_iso.sh /path/to/HackMe-OS-0.1.0-rc11r-amd64.iso` (all channels **0.1.0-rc11r** — see `scripts/release/CURRENT_VERSION`)  
3. Flash USB (Etcher / `dd`), boot **HackMe OS** GRUB entry — not Alpine `localhost login`  
4. On-screen: generated `HMC-…` wallet + recovery phrase; mining starts against hackme.tech  

Details: [HACKME_OS.md](HACKME_OS.md)

---

## Path D — CLI worker (servers / advanced)

For operators who need raw `workerpoh` without the dashboard:

```bash
cp scripts/ops/worker.env.example .env.worker
# Set COORD_URL, COORD_ADMIN_TOKEN or worker token, WORKER_ID, HACKME_MINER_ED25519_SEED_HEX
set -a; source .env.worker; set +a
go build -tags cuda -o bin/workerpoh-cuda ./cmd/workerpoh   # or opencl / cpu
bin/workerpoh-cuda -coord "$COORD_URL" -token "$COORD_TOKEN" -worker "$WORKER_ID" -batch 4194304
```

Smoke: `bash scripts/ops/new_miner_journey_gate.sh`

---

## Payout address

- Submits must use a valid **`HMC-` + 16 hex** address (derived from your Ed25519 seed or assigned by operator).
- Accrual is **off-chain** on the coordinator until operator **settlement** maps `WORKER_ID` → wallet ([NETWORK_MODEL.md](NETWORK_MODEL.md)).

---

## Verify everything works

```bash
bash scripts/ops/start_pool_miner.sh                 # golden path + pool row wait
bash scripts/ops/miner_happiness_check.sh          # pool health
bash scripts/tests/public_site_smoke.sh            # hackme.tech pages + ISO size
bash scripts/ops/run_miner_launch_gate.sh          # full RC gate (operators)
```

Local UI tests (node on :8080):

```bash
E2E_BASE_URL=http://127.0.0.1:8080 \
E2E_ADMIN_TOKEN="$(grep ^HACKME_ADMIN_TOKEN= .env.desktop | cut -d= -f2-)" \
bash scripts/tests/run_ui_e2e.sh specs/solopool-dashboard.spec.ts --config=playwright.desktop.config.ts
```

Overnight mining check:

```bash
bash scripts/ops/mining_night_snapshot.sh
```

---

## Operators (VPS deploy)

See [OPERATOR_CHECKLIST.md](OPERATOR_CHECKLIST.md) and `scripts/ops/deploy_hackme_public_stack.sh`.

---

## Dashboard telemetry

- **Hashrate & session (pool mode):** dashboard reads **`GET /api/work/stats`** from the coordinator (via local proxy). Local log GH/s is diagnostic only.
- **`GET /api/metrics`:** host CPU/GPU telemetry; `pool_worker_hashrate_gh_s` mirrors coordinator when mining.
- **Public pool UI:** [hackme.tech/pool/coordinator](https://hackme.tech/pool/coordinator) — same worker rows.
