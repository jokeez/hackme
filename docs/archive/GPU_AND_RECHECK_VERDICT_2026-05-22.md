# GPU auto-detect, tuning, pool fairness — recheck verdict (2026-05-22)

Operator recheck after RC `0.1.0-rc11g` on **kapa-pc** (RTX 5060 Ti) + public pool `https://hackme.tech/pool`.

## Executive summary

| Area | Verdict | Notes |
|------|---------|-------|
| GPU auto-detect (`detect_gpu_backend`) | **PASS** | 3/3 runs → `cuda` |
| GPU rig suite (14 checks) | **PASS** | RTX 5060 Ti, ~69–130 GH/s probe, gputune hints Blackwell |
| GPU power / AC presets (`gpu_power_smoke`) | **PASS (degraded)** | GET `/api/hardware/tune` OK; `nvidia-smi -pl` denied without root (expected desktop) |
| Pool mining + retarget | **PASS** | `worker-kapa-pc` ~116–121 GH/s; `target_mod` rises with fleet (~87M→103M) |
| Pool fairness audit (3 samples) | **PASS** | Payout model tracks attempts; hash % ≠ payout % (by design) |
| Worker token in `ps` | **FIXED** | Worker uses `.secrets/hackme_coordinator_worker_token`, not admin |
| `go test ./...` | **PASS** | Including `internal/gputune` RTX5060 profile |
| Adversarial API matrix | **PASS** (after fix) | `tx-malformed` accepts 401 when admin auth enabled |
| Coordinator mega stress (quick) | **PASS** | Race/malformed probes OK |
| Difficulty health + redteam surface | **PASS** | Local node gates OK |
| Canonical proxy smoke | **FAIL** | Local SQLite tip stale; P2P peer `:18080` not reachable from LAN |
| VPS `:18080` direct | **FAIL (network)** | Timeout without SSH tunnel; OK via `ssh root@132.243.112.100` |

Mining and coordinator economics are healthy. The only open **product** gap is **local chain DB sync** (follower height 0 / wrong `tip_hash`); pool work and canonical overlay via HTTPS are unaffected.

---

## GPU auto-detect and tuning

### Backend detection (3×)

```text
run1: cuda
run2: cuda
run3: cuda
```

Script: `scripts/ops/detect_gpu_backend.sh` with `HACKME_GPU_BACKEND=auto`.

### Rig suite

Report: `reports/tests/20260522T135226Z/gpu_rig_suite/`

- **14/14 PASS** — CUDA probe, fleet count=1, `workerpoh_gpu_device_0` ~69 GH/s, gputune live hints, NVIDIA telemetry CSV.
- OpenCL path also probed (1 device) for AMD/Intel fallback matrix.

### Power / AC (`gpu_power_smoke`)

Report: `reports/tests/gpu_power_smoke_20260522T135216Z/summary.json`

- Telemetry: RTX 5060 Ti, presets `eco_w` / `daily_w` from `internal/gputune`.
- POST apply: `insufficient permissions` (exit 4) — **degraded PASS** on desktop without `CAP_SYS_ADMIN`.
- Operator action for real PL changes: `sudo nvidia-smi -pl <W>` or run tune API as root.

### Unit tests

```text
go test ./internal/gputune/...  → PASS (TestDetectRigProfileRTX5060)
go test ./...                   → PASS
```

`scripts/tests/gpu_hints_matrix.sh` → **PASS** (4 cases).

---

## Pool fairness and difficulty

### Live fleet (sample 3, ~123 GH/s pool)

| Worker | GH/s | target_mod trend |
|--------|------|------------------|
| worker-kapa-pc | ~121 | dominant hash share |
| worker-desktop-1rgp4ge | ~2 | |
| worker-vps-* | &lt;1 | |

`pool_fairness_audit.sh`: **3 samples**, `M` increased 86887532 → 103484441 as fleet GH/s grew; reward/M fell accordingly (expected retarget).

**Dashboard clarification:** **Hash %** = instantaneous fleet share; **Payout %** = cumulative `accepted_attempts` + hits — a fast rig joining late can show high GH/s but lower payout % until it catches up.

### Difficulty health

`difficulty_health.sh` → **PASS** (local coordinator path).

---

## Security / API fixes applied this session

1. **`adversarial_api_matrix.sh`** — treat HTTP **401** on malformed `/api/tx/send` as pass when `HACKME_ADMIN_TOKEN` is set (fail-closed before JSON parse).
2. **`gpu_power_smoke.sh`** — degraded pass when telemetry OK but `nvidia-smi -pl` permission denied.
3. **`desktop_worker_reset.sh`**, **`worker_loop.sh`**, **`restart_linux_desktop_worker.sh`** — prefer `.secrets/hackme_coordinator_worker_token` over admin token (admin no longer in worker argv).

Worker after reset:

```text
workerpoh-cuda ... -token <worker_token_hash> ...  ~126–130 GH/s calibrated
```

---

## Tests run (this recheck)

| Test | Result |
|------|--------|
| `detect_gpu_backend` ×3 | PASS |
| `gpu_rig_suite.sh` | PASS 14/14 |
| `gpu_power_smoke.sh` | PASS (degraded) |
| `gpu_hints_matrix.sh` | PASS |
| `go test ./...` | PASS |
| `adversarial_api_matrix.sh` | PASS 7/7 |
| `difficulty_health.sh` | PASS |
| `redteam_surface_smoke.sh` | PASS |
| `coordinator_mega_stress.sh` (STRESS_QUICK) | PASS |
| `pool_fairness_audit.sh` | PASS |
| `run_pool_health_check.sh` | PASS (earlier) |
| `canonical_proxy_smoke.sh` | **FAIL** — tip mismatch / :18080 timeout |

### Not run / expected in strict prod

| Test | Result | Reason |
|------|--------|--------|
| `coordinator_matrix.sh` (prod strict) | EXPECTED FAIL | `signature_required` under hybrid signer policy |
| `canonical_proxy` without tunnel | FAIL | Firewall on VPS `:18080` from desktop LAN |
| Full 10 min mega stress | skipped | quick mode sufficient for RC gate |

---

## Known issue: local chain sync (canonical proxy)

**Symptom:** `curl http://127.0.0.1:8080/api/status` tip ≠ `https://hackme.tech/api/status`; P2P `sync_run` → `no_lag_or_no_healthy_peer`.

**Cause:** `HACKME_P2P_PEERS=http://132.243.112.100:18080` — port **18080 not reachable** from desktop (connection timeout). P2P handshake never completes; local SQLite stays at height 0.

**Workarounds (operator):**

1. **SSH tunnel** (for tests):  
   `ssh -L 127.0.0.1:18080:127.0.0.1:18080 root@132.243.112.100`  
   then `VPS_BASE=http://127.0.0.1:18080 bash scripts/tests/canonical_proxy_smoke.sh`
2. **One-shot mirror:** `VPS_SSH=root@132.243.112.100 LEADER_URL=http://132.243.112.100:18080 ... bash scripts/ops/follower_bootstrap_from_vps.sh` (stops desktop node briefly).
3. **VPS firewall:** allow TCP 18080 from trusted miner IPs (see `docs/DUAL_VPS_ROLLOUT.md`).

**Not blocking:** pool claim/submit via `https://hackme.tech/pool/coordinator`, dashboard canonical metrics over HTTP, GPU mining.

---

## Uncommitted script fixes (ready to commit)

- `scripts/tests/adversarial_api_matrix.sh`
- `scripts/tests/gpu_power_smoke.sh`
- `scripts/ops/desktop_worker_reset.sh`
- `scripts/ops/restart_linux_desktop_worker.sh`
- `scripts/ops/worker_loop.sh`

---

## Operator actions (optional)

| Priority | Action |
|----------|--------|
| P1 | Commit + push script fixes above; redeploy site if dashboard copy changed |
| P2 | Open VPS :18080 to home IP **or** run `follower_bootstrap_from_vps.sh` for local explorer/wallet parity |
| P3 | For AC power limits: `sudo` wrapper or polkit rule for `nvidia-smi -pl` on desktop |

---

*Generated: 2026-05-22 UTC — kapa-pc recheck session.*
