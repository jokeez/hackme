# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11n` · **Site:** https://hackme.tech · **Branch:** `main`

| Highlight (2026-06-09) | |
|------------------------|--|
| **Node watchdog** | VPS HTTP health timer restarts hung `hackme-node` |
| **Payments E2E** | Canonical nonce in `transfer_demo.sh`; `payments_e2e_max.sh` pack |
| **Channel** | `0.1.0-rc11n` — Windows installer + Linux tarball on downloads |
| **HackMe OS ISO** | Still `0.1.0-rc11l` (live-boot fix) until next ISO rebuild |
| **Downloads** | https://hackme.tech/downloads.html |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — hybrid Ed25519 strict on prod |
| Win/Linux downloads (rc11n) | **Published** — verify SHA256 on downloads page |
| HackMe OS ISO (rc11l) | **Published** — [SHA256SUMS-iso.txt](https://hackme.tech/dist/release_0.1.0-rc11l/SHA256SUMS-iso.txt) |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Site smoke | **PASS** — `bash scripts/tests/public_site_smoke.sh` |
| Version consistency | **PASS** — `bash scripts/tests/version_consistency_gate.sh` |
| Dashboard wallet (local) | **PASS** — canonical peer cache 418+ HMC on prod wallet |

## Open operator items (non-blocking for miners)

1. **rc11m ISO** — optional rebuild when USB image should match Win/Linux channel.
2. **Acer / physical USB** — confirm boot with rc11l ISO SHA before bumping ISO channel.
3. HMS / HMAI vectors in dashboard are **preview** — only **HMC** pool is mineable today.

## Version source of truth

| File | Role |
|------|------|
| `scripts/release/CURRENT_VERSION` | Win/Linux release channel (`0.1.0-rc11n`) |
| `scripts/release/CURRENT_ISO_VERSION` | HackMe OS ISO channel (`0.1.0-rc11l`) |
| `web/site/assets/app.js` → `RELEASE_VER` | Site + dashboard download URLs |
| `main.go` → `Version` | Node binary embed (rebuild/deploy to match) |

Detail: [HACKME_RC11N.md](HACKME_RC11N.md)
