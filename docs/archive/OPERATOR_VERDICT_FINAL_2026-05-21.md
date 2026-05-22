# Final operator verdict — full test sweep — 2026-05-21

Consolidated results after running **all applicable** automated suites (local, public API, VPS).  
Run id prefix: `verdict_*` / `coord_matrix2_*` under `reports/tests/`.

---

## Executive verdict

| Layer | Status |
|-------|--------|
| **Code / unit tests** | **PASS** — `go test ./...` green |
| **Security pack (new)** | **PASS** — WASM, network fault, hybrid tamper |
| **Public pool production** | **PASS** — coordinator, hybrid strict, redteam surface |
| **Windows RX 580 OpenCL** | **PASS** (after `windows_run_ideal_worker.ps1`) — ~**3.0 GH/s** |
| **Linux NVIDIA CUDA** | **BLOCKED** — NVML mismatch until reboot |
| **Full daily / E2E / soak** | **NOT RUN** — no local node stack on audit machine |
| **CI (GitHub)** | **PASS** — last 2 pushes green |

**Overall: READY for public pool operations and security posture; complete `MODE=full` gate requires a machine with local node + admin token.**

---

## Live pool (verified now)

| Metric | Value |
|--------|--------|
| `pool_hashrate_gh_s` | **~3.4 GH/s** |
| `target_mod` | ~2.5M |
| `reward_per_m` | ~0.004 HMC / 1M attempts |
| `hybrid_signer_strict` | **true** |
| `max_active_leases_per_worker` | **12** |
| `lease_sec` | **90** |

| Worker | GH/s | Note |
|--------|------|------|
| `worker-desktop-1rgp4ge` | **~3.0** | Windows RX 580 OpenCL |
| `worker-vps-62-01` | ~0.35 | VPS CPU |
| Others | ~0.01 | canary / desktop CPU |

**Total accrued payout (coordinator):** ~8.4 HMC across workers (settlement timer on VPS reported inactive in `miner_happiness_check` — enable for faster on-chain credits).

---

## Test matrix (what we ran)

### PASS

| Suite | Where | Notes |
|-------|-------|-------|
| `go test ./...` | Local | All packages incl. `internal/fuzz`, `workercoord`, `cmd/coordinator` |
| `critical_security_pack.sh` | Local | WASM sandbox, network fault, hybrid tamper, sandbox gate |
| `gpu_hints_matrix.sh` | Local | 4 GPU model profiles |
| `run_language_production_pack.sh` (STATIC_ONLY) | Local | WASM artifacts validated |
| `hybrid_signer_smoke.sh` | **Public pool** | `REQUIRE_HYBRID=1 REQUIRE_STRICT=1` |
| `coordinator_matrix.sh` | **Public pool** | 8/8 checks (`COORD=https://hackme.tech/pool/coordinator`) |
| `redteam_surface_smoke.sh` | **Public site** | Privileged APIs reject unauth (403/401) |
| `check_invariants.sh` | Local status | Economics invariants (local stub node) |
| **GitHub CI** | Remote | Runs `26219793710`, `26219719108` — **success** |

### FAIL / expected fail

| Suite | Reason |
|-------|--------|
| `gpu_rig_suite.sh` | **NVML driver/library mismatch** on Linux desktop — CUDA/OpenCL probes fail (host fix: reboot) |
| `run_daily.sh` MODE=quick `transfers_matrix` | Public `https://hackme.tech` rejects/invalid for unsigned tx matrix (4/4) — run on **local node with token** |
| `coordinator_matrix` on VPS | Test worker `qa-worker-01` hit **`too_many_worker_leases`** (429) — pool full; not a prod bug |
| `mock_miners_load.sh` | `work/stats` **403** without correct admin path on public URL |

### NOT RUN (needs local stack)

| Suite | Requirement |
|-------|-------------|
| `run_daily.sh` MODE=**full** / **pre_release** | Local node `:8080` + `ADMIN_TOKEN` |
| `fuzz_runtime_gate.sh` | Running node + admin |
| `orders_matrix.sh` | Admin + local node |
| `e2e_stack.sh` / `run_ui_e2e.sh` | Playwright + local stack |
| `soak_capture.sh` | Long run + node |
| `coordinator_mega_stress.sh` | 10 min isolated stress |
| `public_release_mega_pack.sh` | Full ultimate profile |
| `canonical_proxy_smoke.sh` | Local + VPS node pair |

---

## Security (closed)

```bash
bash scripts/tests/critical_security_pack.sh   # PASS
```

- Malicious WASM: OOB trap, WASI blocked, infinite-loop bounded (subprocess wall clock)
- Worker client: 500–800 ms latency + 30% packet loss; backoff ≤45 s
- Hybrid signer: tampered sig / payload → `invalid_signature`, **zero payout**
- Live pool: hybrid **strict** enforced (`signature_required` on unsigned submit)

---

## Windows RX 580 — operations

**Stable start (repeat after reboot):**

```bash
scp scripts/ops/windows_run_ideal_worker.ps1 hackme-windows:'C:/HackMe/'
ssh hackme-windows 'powershell -NoProfile -ExecutionPolicy Bypass -File C:\HackMe\windows_run_ideal_worker.ps1'
```

**Root causes addressed in repo:**

- `write_hackme_env.ps1`: `RIG_PROFILE_AUTO=0`, RX580 → opencl + 28s cooldown
- `windows_run_ideal_worker.ps1`: correct seed + token + OpenCL args
- `start_rx580_pool_worker.bat`, `autostart_pool_worker.bat`: prefer OpenCL binary

**Remaining:** refresh `hackme.exe` on Windows to current **rc11g** so auto-profile matches repo (old binary rewrote env with `auto` / 150 ms).

---

## Linux desktop — one action

```text
NVML: Driver/library version mismatch
```

1. Reboot  
2. `nvidia-smi -L` OK  
3. `bash scripts/ops/setup_cuda_desktop.sh && bash scripts/ops/apply_desktop_gpu_pool.sh`  
4. `bash scripts/tests/gpu_rig_suite.sh` → expect CUDA **PASS**

---

## VPS operator actions (optional polish)

```bash
# Fair pool + settlement (miner_happiness warned timer inactive)
ssh hackme-vps 'cd /opt/hackme && bash scripts/ops/apply_miner_fair_pool.sh'

# Enable settlement timer if not active
# systemctl status hackme-settlement-timer.service
```

---

## Marketing / docs (done)

| Item | Status |
|------|--------|
| Bitcointalk economics BBCode | Ready — `docs/BITCOINTALK_ECONOMICS_BBCode.txt` |
| Telegram bot | Configured on VPS |
| README / stress / GPU docs | In repo |

---

## Final checklist

| # | Item | Status |
|---|------|--------|
| 1 | `go test ./...` | ✅ |
| 2 | Security pack | ✅ |
| 3 | Public pool + hybrid strict | ✅ |
| 4 | Coordinator matrix (public) | ✅ |
| 5 | Redteam API surface | ✅ |
| 6 | Windows ~3 GH/s | ✅ (restart script) |
| 7 | Linux CUDA | ⏳ reboot |
| 8 | `MODE=full` daily gate | ⏳ needs local/VPS node session |
| 9 | Settlement timer | 🔶 enable on VPS |
| 10 | Rotate Telegram bot token | 🔶 if exposed in chat |

---

## One-line verdict

**Production-ready for public HTTP pool mining (AMD Windows + VPS CPU), security tests green, CI green — full pre-release soak and Linux CUDA need a hosted test node and one desktop reboot respectively.**
