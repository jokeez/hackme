# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11j` · **Site:** https://hackme.tech · **Branch:** `main`

| Highlight (2026-05-26) | |
|------------------------|--|
| **Fuzz Engine v2** | Seed corpus · bit-flip mutation · coverage buckets v2 · reproducible artifacts · `fuzz_report_v2` |
| **Wallet / settlement** | Desktop canonical wallet, settlement timer fix, `/dev/null` VPS sanity |
| **Downloads** | Rebuild `0.1.0-rc11j` Windows `.exe` + Linux tarball on https://hackme.tech/downloads.html |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — hybrid Ed25519 strict on prod |
| ISO / downloads | **Published** — verify SHA256 on downloads page |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Site smoke | **PASS** — `bash scripts/tests/public_site_smoke.sh` |
| Dashboard UI (local) | **PASS** — Playwright `tests/e2e/specs/solopool-dashboard.spec.ts` |
| Multi-GPU / hybrid fleet | **GO** — `fleetplan`, `HACKME_GPU_HYBRID=auto`, `worker_autostart.sh` (CUDA+OpenCL) |
| HackMe OS visual overhaul (source) | **GO** — GRUB/Plymouth/TTY shipped on downloads ISO |
| Published ISO on hackme.tech | **LIVE** — `43abb592…67d6125` · **874 442 752** B (~834 MiB) · [SHA256SUMS-iso.txt](https://hackme.tech/dist/release_0.1.0-rc11j/SHA256SUMS-iso.txt) |

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
| `public_site_smoke.sh` | **PASS** (pages 200 · ISO **874 442 752** B) |
| Published ISO visuals | **Shipped** — GRUB/Plymouth/ZK TTY |
| Prod `GET /api/global/metrics` | OK — use this for aggregate stats (not legacy `/pool/api/metrics`) |

Snapshot: `reports/mining-night-20260523T043740Z/SNAPSHOT.md`

```bash
bash scripts/ops/mining_night_snapshot.sh
```

Historical dated verdicts moved to [archive/](archive/README.md).
