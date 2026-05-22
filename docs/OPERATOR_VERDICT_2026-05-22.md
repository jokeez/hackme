# Operator verdict — HackMe OS ISO · full sweep — 2026-05-22

**Release:** `0.1.0-rc11g` · **Commit:** `0751f3f` (ISO pipeline) + `12f0ce0` (site/downloads)  
**Run ids:** `20260522T120550Z` (chaos guard) · agent build log `/tmp/hackme-os-build3.log`

---

## Executive verdict

| Layer | Status | Notes |
|-------|--------|-------|
| **Go tests** (`go test ./...`) | **PASS** | All packages green (~12s) |
| **Nightly chaos guard** | **PASS** | 5000 payouts, crypto chaos, init-worker, security pack |
| **HackMe OS init / ZK** | **PASS** | `init_worker_test.sh` incl. `zk_empty_ini` |
| **ISO build** | **PASS** | `HackMe-OS-0.1.0-rc11g-amd64.iso` 956 MB, squashfs ~826 MB |
| **ISO contents** | **PASS** | `pool.token`, `minersign`, `workerpoh` in image |
| **Pool token → claim** | **PASS** | HTTP **200** canary claim (prod coordinator) |
| **Public pool live** | **PASS** | ~**2.43 GH/s**, **5 workers**, economics fair mode |
| **Hybrid signer** | **ENFORCED** | Unsigned submits → **403** `signature_required` (expected) |
| **Rate limits** | **ACTIVE** | `claim_rate_limited` / `worker_temporarily_banned` under abuse |
| **VPS deploy (this agent)** | **BLOCKED** | No SSH key to `root@82.146.53.7` — operator must run deploy script |

**Overall: READY for public ISO test-drive and pool mining.** Publish ISO to VPS `dist/` then reload nginx.

---

## Live pool snapshot (2026-05-22)

| Metric | Value |
|--------|--------|
| Pool hashrate | **~2.43 GH/s** |
| `target_mod` | **~4,972,912** |
| `reward_per_m` | **~0.00201 HMC** / 1M attempts |
| `base_reward_hmc` | 0.01 |
| `issued_ranges` | 3140+ |
| `found_hits` | 533+ |
| `hybrid_signer_strict` | **true** |
| Active workers | **5** |

| Worker | GH/s (approx) |
|--------|----------------|
| `worker-desktop-1rgp4ge` | **~2.06** (main GPU rig) |
| `worker-vps-62-01` | ~0.35 |
| `worker-kapa-pc` | ~0.08 |
| `worker-vps-msk-01` | ~0.02 |
| `vps-canary-01` | ~0.01 |

`drop_reason_count` includes **claim_rate_limited** and **worker_temporarily_banned** — coordinator backpressure works; do not flood claim without valid hybrid signing.

---

## HackMe OS ISO (built & verified)

| Field | Value |
|-------|--------|
| File | `dist/release_0.1.0-rc11g/HackMe-OS-0.1.0-rc11g-amd64.iso` |
| Size | **956 MB** |
| SHA256 | `1b7bd70e381bb0d5aee82135fe01963d27d2af43ebfba95e02dec22aabe17658` |
| Pool URL (embedded) | `https://hackme.tech/pool/coordinator` |
| Zero-Knowledge | Empty `hackme.ini` → `minersign` + 24-word phrase on TTY1 |

**Pipeline fixes (PR #1):** unmount `proc/sys/dev` before squashfs; `mtools` for `grub-mkrescue`; `swapoff` + GRUB `toram`/`isolcpus`; `clean_iso_work.sh`.

---

## Mining smoke (agent host)

- **CPU worker** without seed: submits rejected **`signature_required`** — correct for strict hybrid pool.
- **CPU worker** with `HACKME_WORKER_SIGN_SUBMITS=1` + seed: claims OK until **429 banned** after earlier unsigned abuse — rate limiter OK.
- **Production rigs** must use ISO ZK / `minersign` / Windows env with signing enabled (ISO sets `HACKME_WORKER_SIGN_SUBMITS=1`).

---

## Operator deploy checklist (VPS)

From machine with SSH to canonical hub (`NODE_SSH=hackme-vps` or `root@82.146.53.7`):

```bash
cd /path/to/hackme
export NODE_SSH=hackme-vps   # or root@<vps-ip>
export NODE_DEPLOY_DIR=/opt/hackme
bash scripts/ops/deploy_release_rc11g_iso.sh
```

This rsyncs `dist/release_0.1.0-rc11g/` (incl. ISO + `SHA256SUMS-iso.txt`), refreshes `web/site`, rebuilds `news-feed.json`, reloads nginx, curls smoke URLs.

**After deploy:** confirm https://hackme.tech/downloads.html#hackme-os shows ISO link (HEAD 200).

---

## Communications

| Channel | Asset |
|---------|--------|
| Bitcointalk | `docs/BITCOINTALK_UPDATE_HACKME_OS_BBCode.txt` |
| Telegram | `docs/TELEGRAM_POST_HACKME_OS.md` |
| Site news | `web/site/assets/news.json` (ISO SHA256 published) |

---

## Known gaps

1. **Windows installer** not rebuilt on agent (no Inno/Docker overlay) — existing `HackMe-Setup` on site unchanged.
2. **ISO not on hackme.tech yet** until operator runs deploy (956 MB rsync).
3. **Live USB** without `hackme-os-install` regenerates wallet each reboot — document for miners.

---

*Experimental RC — verify SHA256 and explorer payouts before farm scale.*
