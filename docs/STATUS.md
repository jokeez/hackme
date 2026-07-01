# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11r` · **Site:** https://hackme.tech · **Branch:** `main`

| Highlight (2026-07-02) | |
|------------------------|--|
| **rc11r live** | Win/Linux/ISO single channel on CDN |
| **Linux miners** | Tarball ships `scripts/ops/` + `bin/workerpoh-opencl` — fixes `worker_script_missing` |
| **Fuzz B2B** | Pull-mode settle outbox, escrow cleanup, nginx routes |
| **Mining ops** | VPS settlement timer re-enabled; pool accrual → on-chain sweep |
| **Downloads** | https://hackme.tech/downloads.html |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — hybrid Ed25519 strict on prod |
| SUP (accrual + on-chain) | **Live** — settlement timer active |
| Win/Linux/ISO (rc11r) | **Published** — verify SHA256 on downloads page |
| Security audit (prod) | **16/16 PASS** |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Site smoke | **PASS** — `bash scripts/tests/public_site_smoke.sh` |
| Version consistency | **PASS** — `bash scripts/tests/version_consistency_gate.sh` |
| Fuzzing B2B | **Live** — wizard + pool + CI gate |

## Out of scope (preview / later)

1. **HMS** — prelaunch; not for public miners.
2. HMAI / Alpha vectors in dashboard are **preview** — only **HMC** pool is mineable today.

## Version source of truth

| File | Role |
|------|------|
| `scripts/release/CURRENT_VERSION` | Win/Linux release channel (`0.1.0-rc11r`) |
| `scripts/release/CURRENT_ISO_VERSION` | HackMe OS ISO channel (`0.1.0-rc11r`) |
| `web/site/assets/app.js` → `RELEASE_VER` / `ISO_CHANNEL` | Site + dashboard download URLs |
| `main.go` → `Version` | Node binary embed (rebuild/deploy to match) |

Detail: [HACKME_RC11R.md](HACKME_RC11R.md)
