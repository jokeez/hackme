# Setup guide — miners and operators

One-page paths for **mining on the public pool** and **running your own desktop node**.  
Official coordinator: `https://hackme.tech/pool/coordinator`

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

## Path A — Linux desktop (recommended)

```bash
# Edit .env.desktop: WORKER_PAYOUT_MAP=worker-$(hostname -s)=HMC-your16hex
bash scripts/ops/desktop_mode_up.sh
```

- Dashboard: **http://127.0.0.1:8080**
- **Workers** tab → **Start pool worker** (or `bash scripts/ops/restart_linux_desktop_worker.sh` for CUDA/OpenCL)
- Stop: `bash scripts/ops/desktop_mode_stop.sh`

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
2. Verify: `bash scripts/tests/verify_hackme_iso.sh /path/to/HackMe-OS-0.1.0-rc11j-amd64.iso`  
3. Flash USB (Etcher / `dd`), boot **HackMe OS** GRUB entry — not Alpine `localhost login`  
4. On-screen: generated `HMC-…` wallet + recovery phrase; mining starts against hackme.tech  

Details: [HACKME_OS.md](HACKME_OS.md)

---

## Path D — CLI worker (servers)

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

SSH host alias `hackme-vps` in `~/.ssh/config` (see your internal runbook — **do not commit** host keys or passwords).

```bash
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_node.sh
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_site.sh
```

Coordinator admin token **only** on the server and in local `.secrets/` — never in GitHub.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `401` / `signature_required` | Enable hybrid seed: `HACKME_WORKER_SIGN_SUBMITS=1` + `HACKME_MINER_ED25519_SEED_HEX` |
| `429` / `worker_temporarily_banned` | Back off 1–5 min; reduce burst tests against prod |
| Stale dashboard UI | Rebuild node: `go build -o logs/desktop/hackme-node-desktop .` then restart |
| Local chain height 0 | Normal for pool follower; use `canonical_tip_height` or P2P sync scripts |

More: [OPEN_POOL_MINERS.md](OPEN_POOL_MINERS.md) · [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md) · [API.md](API.md)
