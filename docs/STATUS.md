# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11g` · **Site:** https://hackme.tech · **Branch:** `cursor/iso-audit-build-02a1`

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — hybrid Ed25519 strict on prod |
| ISO / downloads | **Published** — verify SHA256 on downloads page |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Site smoke | **PASS** — `bash scripts/tests/public_site_smoke.sh` |
| Dashboard UI (local) | **PASS** — Playwright `tests/e2e/specs/solopool-dashboard.spec.ts` |
| Multi-GPU / hybrid fleet | **GO** — `fleetplan`, `HACKME_GPU_HYBRID=auto`, `worker_autostart.sh` (CUDA+OpenCL) |
| HackMe OS visual overhaul (source) | **GO** — GRUB/Plymouth/TTY shipped on downloads ISO |
| Published ISO on hackme.tech | **LIVE (visual overhaul)** — `3290445848…b228e` · **1 038 151 680** B (2026-05-23) |

## Open operator items (non-blocking for miners)

1. ~~Set `TG_ADMIN_CHAT_ID`~~ — **done on VPS** (pool heartbeat every 4h; see [TELEGRAM_NEWS_BOT_RUNBOOK.md](TELEGRAM_NEWS_BOT_RUNBOOK.md)). Republish channel news when SSH is up: `FORCE_NEWS_ID=2026-05-25-fuzzing-b2b-cli-hardening bash scripts/ops/publish_news_to_telegram.sh`.
2. Do not run 1000-packet `hybrid_crypto_matrix.sh` against prod in a loop (rate limits).
3. HMS / HMAI vectors in dashboard are **preview** — only **HMC** pool is mineable today.
4. ~~Rebuild ISO with visual overhaul~~ — **done** (see `SHA256SUMS-iso.txt` on downloads).

## Morning check (2026-05-23 UTC)

| Check | Result |
|-------|--------|
| `worker-kapa-pc` hashrate | **~68.1 GH/s** (stable overnight) |
| Pool attempts / payout | **~2.63B** attempts · **~0.425 HMC** |
| Processes | `hackme-node-desktop` + `workerpoh-cuda` **running** |
| `public_site_smoke.sh` | **PASS** (pages 200 · ISO **1 002 092 544** B) |
| Published ISO visuals | **Shipped** — GRUB/Plymouth/ZK TTY (`build` 2026-05-23 UTC) |
| Prod `/pool/api/metrics` | **Intermittent slow** (chain/coordinator OK; mining unaffected) |

Snapshot: `reports/mining-night-20260523T043740Z/SNAPSHOT.md`

```bash
bash scripts/ops/mining_night_snapshot.sh
```

Historical dated verdicts moved to [archive/](archive/README.md).
