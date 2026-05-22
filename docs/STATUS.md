# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11g` · **Site:** https://hackme.tech · **Branch:** `cursor/iso-audit-build-02a1`

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — hybrid Ed25519 strict on prod |
| ISO / downloads | **Published** — verify SHA256 on downloads page |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Site smoke | **PASS** — `bash scripts/tests/public_site_smoke.sh` |
| Dashboard UI (local) | **PASS** — Playwright `tests/e2e/specs/solopool-dashboard.spec.ts` |

## Open operator items (non-blocking for miners)

1. Set `TG_ADMIN_CHAT_ID` in VPS `/opt/hackme/.env.newsbot` for pool heartbeat ([TELEGRAM_NEWS_BOT_RUNBOOK.md](TELEGRAM_NEWS_BOT_RUNBOOK.md)).
2. Do not run 1000-packet `hybrid_crypto_matrix.sh` against prod in a loop (rate limits).
3. HMS / HMAI vectors in dashboard are **preview** — only **HMC** pool is mineable today.

## Morning check (your rig)

```bash
bash scripts/ops/mining_night_snapshot.sh
```

Historical dated verdicts moved to [archive/](archive/README.md).
