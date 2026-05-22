# Operator verdict — HackMe pool & security — 2026-05-21

## Executive summary

| Area | Verdict | Notes |
|------|---------|--------|
| **Public pool (VPS)** | **READY** | Coordinator healthy; fair payout mode; hybrid signer strict |
| **Windows RX 580 (OpenCL)** | **READY** (after `windows_run_ideal_worker.ps1`) | ~**3.6 GH/s** live in `active_rigs` |
| **Linux desktop NVIDIA** | **BLOCKED** | NVML driver/library mismatch — **reboot** required |
| **Security test pack** | **PASS** | WASM sandbox, network fault client, hybrid tamper |
| **Bitcointalk / Telegram / economics doc** | **DONE** | Content in repo; operator posted |

---

## Live pool snapshot (verified)

| Metric | Value |
|--------|--------|
| `pool_hashrate_gh_s` | ~**4.0 GH/s** (with Windows rig) |
| `target_mod` | ~2.0–2.4M (auto retarget under load) |
| `reward_per_m` | ~**0.0042** HMC / 1M attempts (tracks base 0.01 / M) |
| `worker-desktop-1rgp4ge` | ~**3.61 GH/s** |
| VPS CPU workers | ~0.01–0.35 GH/s each |

**Earnings intuition (RX 580):**  
`payout ≈ (attempts/1e6) × reward_per_m` per accepted submit.  
At ~3.6 GH/s, batch 1M, cooldown 28s: order of **~0.0001–0.0004 HMC/submit** before settlement batching — confirm on explorer after `settle_worker_payouts`.

---

## Windows — root cause & fix

**Problem:** Old `hackme.exe` (May 20) re-merged rig profile into `hackme.env` with `GPU_BACKEND=auto`, `CLAIM_COOLDOWN_MS=150`, and worker often started without valid `HACKME_MINER_ED25519_SEED_HEX`.

**Fix (repeatable):**

```bash
scp scripts/ops/windows_run_ideal_worker.ps1 hackme-windows:'C:/HackMe/'
ssh hackme-windows 'powershell -NoProfile -ExecutionPolicy Bypass -File C:\HackMe\windows_run_ideal_worker.ps1'
```

Also shipped:

- `scripts/release/windows/write_hackme_env.ps1` — `RIG_PROFILE_AUTO=0`, hard-lock RX580 → opencl + 28s cooldown  
- `scripts/release/windows/start_rx580_pool_worker.bat`  
- `scripts/ops/windows_rx580_ideal.sh` — one-shot from Linux  

**Ideal:** Replace `C:\HackMe\hackme.exe` with current rc11g installer build so auto-profile matches repo (`28000` ms cooldown).

---

## Linux desktop — action

```text
NVML: Driver/library version mismatch (580.159)
```

1. **Reboot** the machine.  
2. `nvidia-smi -L` must succeed.  
3. `bash scripts/ops/setup_cuda_desktop.sh`  
4. `bash scripts/ops/apply_desktop_gpu_pool.sh` && `bash scripts/ops/desktop_worker_reset.sh`  
5. `bash scripts/tests/gpu_rig_suite.sh` → expect CUDA smokes **PASS**.

Until then `worker-kapa-pc` stays ~0.01 GH/s (CPU).

---

## Security & QA (automated)

```bash
bash scripts/tests/critical_security_pack.sh
```

| Suite | Result |
|-------|--------|
| `internal/fuzz` | Malicious WASM: OOB trap, WASI blocked, infinite-loop child timeout |
| `internal/workercoord` | Latency 500–800ms + 30% loss; backoff ≤45s; submit retry bounded |
| `internal/worksubmit` + `cmd/coordinator` | Hybrid tamper → `invalid_signature`, **zero payout** |
| `internal/sandbox` | Existing gate tests |

**Known limit:** wazero interpreter does not preempt tight infinite loops in-process; production uses `wasmCheckTimeout` + validation path; subprocess tests enforce wall-clock bounds.

---

## Marketing / docs

| Deliverable | Status |
|-------------|--------|
| `docs/BITCOINTALK_ECONOMICS_BBCode.txt` | Ready to paste |
| Telegram news bot | Configured on VPS (`@hackme_news_bot`) |
| `docs/GPU_TEST_CLOSURE_2026-05-21.md` | GPU audit trail |

---

## Final operator checklist

- [x] Pool coordinator + settlement running on VPS  
- [x] Windows RX580 ≥3 GH/s on official pool  
- [ ] Linux NVIDIA CUDA after reboot  
- [x] Critical security tests green  
- [ ] Optional: refresh Windows `hackme.exe` to latest rc11g installer  
- [ ] Optional: rotate Telegram bot token (was exposed in chat)  

**Overall verdict: PRODUCTION-READY for public pool + AMD Windows miners; Linux CUDA desktop pending one host reboot.**
