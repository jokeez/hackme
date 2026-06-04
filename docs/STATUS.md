# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11l` · **Site:** https://hackme.tech · **Branch:** `main`

| Highlight (2026-06-04) | |
|------------------------|--|
| **HackMe OS ISO** | Live USB overlay fix — `05-hackme-overlay-modules` in `casper-premount` (not `local-premount`) |
| **Channel** | `0.1.0-rc11l` on downloads — Windows/Linux same binaries as rc11k; **new ISO only** until hardware verified |
| **Downloads** | https://hackme.tech/downloads.html |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — hybrid Ed25519 strict on prod |
| ISO / downloads | **Published** — verify SHA256 on downloads page |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Site smoke | **PASS** — `bash scripts/tests/public_site_smoke.sh` |
| Dashboard UI (local) | **PASS** — Playwright `tests/e2e/specs/solopool-dashboard.spec.ts` |
| Multi-GPU / hybrid fleet | **GO** — `fleetplan`, `HACKME_GPU_HYBRID=auto`, `worker_autostart.sh` (CUDA+OpenCL) |
| HackMe OS visual overhaul (source) | **GO** — GRUB/Plymouth/TTY shipped on downloads ISO |
| Published ISO on hackme.tech | **LIVE** — `81a1f1f1…cad30b0db` · **878 454 784** B (~838 MiB) · [SHA256SUMS-iso.txt](https://hackme.tech/dist/release_0.1.0-rc11l/SHA256SUMS-iso.txt) |

## Open operator items (non-blocking for miners)

1. **Acer / physical USB** — confirm boot with rc11l ISO SHA before any version bump past `rc11l`.
2. Do not run 1000-packet `hybrid_crypto_matrix.sh` against prod in a loop (rate limits).
3. HMS / HMAI vectors in dashboard are **preview** — only **HMC** pool is mineable today.

## Version source of truth

- `scripts/release/CURRENT_VERSION` — operator scripts default
- `web/site/assets/app.js` → `RELEASE_VER` — site + dashboard download URLs
- `main.go` → `Version` — node binary embed (rebuild/deploy to match)
