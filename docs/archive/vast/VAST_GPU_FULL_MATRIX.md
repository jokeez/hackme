# Vast GPU + regions — full test matrix (operator)

Agent runs SSH sessions; secrets in `.secrets/vast/` only.  
Pack: `dist/vast-gpu-matrix-*.tar.gz`

---

## A. GPU models (architecture)

| Phase | GPUs | worker_id prefix |
|-------|------|------------------|
| A | 2× 5090 | `vast-5090-` |
| B | 4090, 4080, 4060 | `vast-40-` |
| C | 3090, 3080 | `vast-30-` |
| D | 2080 Ti, T4 | `vast-20-` |
| E | 6× fleet one box | `vast-fleet-6g` (+ `-gpu0`…) |

Per run: `00_inventory` → worker/fleet 30–60m → `regional_latency_probe` → `03_ui_snapshot` → `02_collect_report`.

---

## B. Regions (latency / shares)

Rent **same GPU tier** (e.g. RTX 4090 or T4) in **different Vast regions**:

- US-East, US-West  
- EU (Netherlands / Germany — compare to your NL hub)  
- Asia (Singapore / Japan) if cheap enough  

Record in `GPU_MATRIX_SHEET.csv`: `region`, `ping_ms`, `curl_ttfb_ms`, `ghs`, `accepts_per_min`, `429_count`.

**What we validate**

| Signal | Good | Bad |
|--------|------|-----|
| Ping hackme.tech | &lt;150ms from EU to NL hub | &gt;300ms sustained |
| claim/submit errors | rare timeouts | flood of 429/timeout |
| GH/s vs desktop | same order of magnitude | 10× lower with no reason |
| Shares in log | `submit` / accepted | only claim, no submit |
| Pool stats | worker row updates | stale &gt;5 min |

Hub is **NL** — expect best numbers from **EU** Vast; US/Asia document degradation, not necessarily fail.

---

## C. UI (mandatory every run)

### Local (`node :8080` must run)

| Tab | URL | Check |
|-----|-----|--------|
| Mining | `/#mining` | All `WORKER_ID` rows; fleet shows **each GPU** |
| Hardware | `/#hardware` | GPU cards on **local PC** (Vast: log only) |
| Chain | `/#chain` | tip, blocks, difficulty fields |
| Hive fleet | mining panel | GH/s overlay per worker |

```bash
bash scripts/vast/local_dashboard_probe.sh <worker_id>
```

### Prod

- https://hackme.tech/dashboard.html#mining  
- https://miningpoolstats.app/coins/HMC  

---

## D. Pool / chain / economics

| Check | API / UI |
|-------|----------|
| Difficulty `target_mod` | `/api/status`, mining KPI |
| Pool GH/s | coordinator pool/stats, MPS |
| Work distribution | `/api/work/stats` workers list |
| Blocks / lead miner | `/api/network/stats`, #chain |
| Settlement path | accrual moves (longer runs) |
| 429 / abuse | worker log, no ban loop |

---

## E. Adaptivity & sync

- CUDA auto-calibration (no manual `HACKME_CUDA_CALIBRATE_GHS`)  
- Batch 4M remote stable 15+ min  
- Coordinator `worker_found_in_stats` == dashboard row  
- 6-GPU: N rows in UI (`01_run_fleet.sh`)  

---

## F. Scripts per session

```bash
# On Vast (in pack):
bash scripts/00_inventory.sh
bash scripts/regional_latency_probe.sh EU-Amsterdam
bash scripts/01_run_pool_worker.sh   # or 01_run_fleet.sh
bash scripts/03_ui_snapshot.sh
bash scripts/02_collect_report.sh

# On operator PC:
bash scripts/vast/local_dashboard_probe.sh <worker_id>
bash scripts/vast/ssh_check_ui.sh <worker_id>
```

SSH wrapper: `bash scripts/vast/ssh_run_session.sh <name>`

---

## G. CSV columns

`region, gpu_name, worker_id, ping_avg_ms, api_ttfb_s, ghs_peak, submits_ok, ui_mining_pass, ui_fleet_count, ui_chain_ok, mps_ok, notes`

---

## H. Budget discipline

- Destroy instance after each region/GPU test  
- T4 20–30 min for region sweep; 5090 45–60 min  
- Log SKIP if offer &gt;$2/h on $10 day  

---

## I. What agent delivers after day

1. `reports/vast-remote/*` logs + snapshots  
2. Filled CSV pass/fail per GPU × region  
3. Regressions list (if any) with repro worker_id  
4. UI verdict: local + prod aligned yes/no  
