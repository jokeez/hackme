# GPU mining test closure — 2026-05-21

Practical audit after Bitcointalk economics post: unit tests, rig suite, live pool, Windows RX 580, Linux desktop.

## Summary

| Platform | GPU | Backend | Test result | Pool GH/s (live) |
|----------|-----|---------|-------------|------------------|
| **Windows** `DESKTOP-1RGP4GE` | AMD RX 580 2048SP | **OpenCL** | **PASS** — deployed `workerpoh-opencl.exe`, profile `amd_rx580_2048sp` | **~3.53** (`worker-desktop-1rgp4ge`) |
| **Linux desktop** | NVIDIA (card1 `0x10de`) | blocked | **FAIL** — NVML driver/library mismatch + CUDA `CompatNotSupportedOnDevice` | **~0.01** (`worker-kapa-pc`, CPU fallback) |
| **VPS fleet** | CPU | cpu/opencl low | **PASS** (online) | ~0.01–0.35 per worker |
| **Repo (CI-safe)** | — | unit | **PASS** — `gputune`, `gpupoh`, `gpu_hints_matrix` | — |

**Pool total (admin `/api/work/stats`):** ~**3.9 GH/s** after Windows OpenCL fix (was ~0.4 GH/s).

---

## Tests run

```bash
# Always green (no GPU required)
go test ./internal/gputune ./internal/gpupoh -count=1
bash scripts/tests/gpu_hints_matrix.sh

# Full local rig suite
RUN_ID=gpu_audit_YYYYMMDD bash scripts/tests/gpu_rig_suite.sh
# Report: reports/tests/<RUN_ID>/gpu_rig_suite/summary.json
```

Latest local suite: **12 PASS, 2 FAIL** — failures only on CUDA probe/list (NVML mismatch on this host).

---

## Windows RX 580 — fixed today

**Hardware:** `AMD Radeon RX 580 2048SP` (driver 31.0.12027.9001)

**Actions performed:**

1. Built / synced `workerpoh-opencl.exe` → `C:\HackMe\`
2. `write_hackme_env.ps1` with `-RigProfile amd_rx580_2048sp` (batch 1M, cooldown 28s, thermal guard)
3. `windows_fix_env_and_restart.ps1` — node + OpenCL worker, hybrid seed from `data\node_ed25519.seed`

**Verify:**

```powershell
Get-Process hackme,workerpoh-opencl
# Pool (operator): active_rigs worker-desktop-1rgp4ge ~3+ GH/s
```

**Operator repeat deploy:**

```bash
scp dist/windows/workerpoh-opencl.exe hackme-windows:'C:/HackMe/'
ssh hackme-windows "powershell -File C:/HackMe/windows_fix_env_and_restart.ps1 C:/HackMe"
```

---

## Linux desktop NVIDIA — action required (you)

**Symptoms:**

- `nvidia-smi`: `Failed to initialize NVML: Driver/library version mismatch`
- `detect_gpu_backend.sh` → `cpu` (intentional when NVML unhealthy)
- `gpuprobe-cuda`: `CompatNotSupportedOnDevice` (driver/kernel not aligned with CUDA 12.8 userland)

**Fix (pick one path):**

1. **Reboot** after last driver/kernel update (fastest for NVML mismatch).
2. Or reinstall matching driver stack, then:
   ```bash
   bash scripts/ops/setup_cuda_desktop.sh
   bash scripts/ops/apply_desktop_gpu_pool.sh
   bash scripts/ops/desktop_worker_reset.sh
   ```
3. Confirm: `nvidia-smi -L` OK → `bash scripts/tests/gpu_rig_suite.sh` → CUDA smokes PASS.

Until fixed, **do not expect CUDA GH/s** on `worker-kapa-pc`; pool row will stay ~0.01 GH/s.

---

## Rig profiles reference

| Profile | GPU | Backend | Key env |
|---------|-----|---------|---------|
| `amd_rx580_2048sp` | RX 580 2048SP | opencl | batch 1M, cooldown 28s, temp 78°C |
| `amd_rx580_generic` | RX 580 | opencl | batch 2M, cooldown 28s |
| `nvidia_rtx50_cuda` | RTX 50xx | cuda | batch 4M, calibrate |
| `intel_arc_daily` | Intel Arc | opencl | batch 2M |

API: `GET http://127.0.0.1:8080/api/hardware/rig-profiles/detect`

Docs: [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md), [RIG_PROFILES.md](RIG_PROFILES.md), [CUDA_PRODUCTION.md](CUDA_PRODUCTION.md)

---

## Critical security pack (2026-05-21)

```bash
bash scripts/tests/critical_security_pack.sh
```

| Suite | Covers |
|-------|--------|
| `internal/fuzz` | Malicious WASM: OOB trap, WASI import blocked, infinite-loop child kill, eval loop bounded |
| `internal/workercoord` | 500–800ms latency + 30% packet loss; retry/backoff cap 45s; submit dedup |
| `internal/worksubmit` | One-byte canonical JSON tamper breaks Ed25519 verify |
| `cmd/coordinator` | Hybrid signer: flip sig byte / change `attempts` after sign → `invalid_signature`, zero payout |
| `internal/sandbox` | Existing checkexport tests |

**Note:** wazero interpreter does not preempt tight infinite loops inside one process; production relies on `wasmCheckTimeout` + subprocess tests document wall-clock bounds. Consider a native watchdog if this becomes a production concern.

---

## What is “closed” vs open

| Item | Status |
|------|--------|
| Multi-vendor rig profile matrix (unit) | **Closed** |
| Windows AMD OpenCL production path | **Closed** (live ~3.5 GH/s) |
| Public pool accrual + hybrid sign | **Closed** (4–5 workers online) |
| Linux NVIDIA CUDA on desktop | **Open** — needs reboot/driver align |
| MiningPoolStats listing polish | **Open** — list CUDA rig GH/s when desktop fixed |

---

## Next commands (operator)

```bash
# Re-test after Linux reboot
bash scripts/tests/gpu_rig_suite.sh

# Pool monitor
ssh hackme-vps 'curl -fsS -H "X-Hackme-Admin-Token: $(cat /opt/hackme/.secrets/hackme_coordinator_admin_token)" http://127.0.0.1:18081/api/work/stats | jq ".pool_hashrate_gh_s, .active_rigs"'

# Windows health
ssh hackme-windows 'tasklist | findstr /i workerpoh hackme'
```
