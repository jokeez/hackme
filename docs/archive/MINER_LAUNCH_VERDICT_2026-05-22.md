# Miner launch verdict — controlled start — 2026-05-22

**Decision:** **GO** for early miners (RC `0.1.0-rc11g`).  
**Model:** bugs and setup issues → Telegram → fix in next deploy. VPS scale (RAM/CPU/disk) later.

---

## Automated gate (this run)

```bash
bash scripts/ops/run_miner_launch_gate.sh
```

Report: `reports/miner-launch-gate-20260522T171236Z/VERDICT.md`

| Step | Result |
|------|--------|
| `go test ./...` | PASS |
| `nightly_chaos_guard.sh` | PASS |
| `init_worker_test.sh` | PASS |
| `coordinator_mega_stress` (quick) | PASS |
| `difficulty_health` (prod pool) | PASS |
| `redteam_surface_smoke` | PASS |
| Pool `work/stats` | PASS |
| UI e2e (8 tests) | 7 PASS, 1 flaky (`mining stop` — non-blocking) |
| VPS ISO GRUB + SHA256 | PASS (`1b7bd70e…`, HackMe OS menu) |

---

## What miners get

- Pool: https://hackme.tech/pool/coordinator  
- Downloads: https://hackme.tech/downloads.html  
- ISO SHA256: `1b7bd70e381bb0d5aee82135fe01963d27d2af43ebfba95e02dec22aabe17658`  
- Support: [@hackme_ru](https://t.me/hackme_ru) · [@hackme_en](https://t.me/hackme_en) · [@hackme_tech](https://t.me/hackme_tech)

---

## Operator checklist

See **[MINER_READINESS_CHECKLIST.md](MINER_READINESS_CHECKLIST.md)** before each announce wave.

---

## Known limits (tell miners upfront)

1. Release candidate — updates without long SLA.  
2. Alpine boot screen = wrong USB/file (not HackMe OS).  
3. Live USB → save phrase or `hackme-os-install`.  
4. NVIDIA on ISO may need post-install driver + `cuda` backend.  
5. P2P full mirror optional; pool coordinator is source of truth for work/payout.

---

## Next when traffic grows

- VPS: more RAM/cores/disk.  
- Merge `cursor/iso-audit-build-02a1` → stable tag.  
- Re-run launch gate weekly: `bash scripts/ops/run_miner_launch_gate.sh`
