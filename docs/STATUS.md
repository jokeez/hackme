# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11n` · **Site:** https://hackme.tech · **Branch:** `main`

| Highlight (2026-06-16) | |
|------------------------|--|
| **Production final** | SUP on-chain live + public `/api/sup/economics`; ISO aligned to rc11n |
| **Node watchdog** | VPS HTTP health timer restarts hung `hackme-node` |
| **Channel** | `0.1.0-rc11n` — Windows installer + Linux tarball + HackMe OS ISO |
| **Downloads** | https://hackme.tech/downloads.html |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — hybrid Ed25519 strict on prod |
| SUP (accrual + on-chain) | **Live** — genesis mint enabled on VPS; settlement timer |
| Win/Linux downloads (rc11n) | **Published** — verify SHA256 on downloads page |
| HackMe OS ISO (rc11n) | **Published** — `dist/release_0.1.0-rc11n/SHA256SUMS-iso.txt` |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Site smoke | **PASS** — `bash scripts/tests/public_site_smoke.sh` |
| Version consistency | **PASS** — `bash scripts/tests/version_consistency_gate.sh` |
| Fuzzing B2B | **Live** — security-audit + Bitcoin30 Week 1 public report |

## Out of scope (preview / later)

1. **HMS** — prelaunch; not for public miners.
2. HMAI / Alpha vectors in dashboard are **preview** — only **HMC** pool is mineable today.

## Version source of truth

| File | Role |
|------|------|
| `scripts/release/CURRENT_VERSION` | Win/Linux release channel (`0.1.0-rc11n`) |
| `scripts/release/CURRENT_ISO_VERSION` | HackMe OS ISO channel (`0.1.0-rc11n`) |
| `web/site/assets/app.js` → `RELEASE_VER` / `ISO_CHANNEL` | Site + dashboard download URLs |
| `main.go` → `Version` | Node binary embed (rebuild/deploy to match) |

Detail: [HACKME_RC11N.md](HACKME_RC11N.md)
