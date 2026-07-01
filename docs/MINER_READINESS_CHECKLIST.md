# Miner readiness checklist (controlled launch)

**Release:** `0.1.0-rc11r` (Win/Linux + ISO **rc11r**, single channel) · **Pool:** `https://hackme.tech/pool/coordinator`  
**Support:** report bugs in Telegram — we fix and ship updates (no SLA yet).

Use this before announcing “open for miners” and when onboarding a new rig.

---

## Operator (you) — one-time

| Step | Command / action | Pass |
|------|------------------|------|
| Go tests | `go test ./... -count=1` | ☐ |
| Chaos + ledger | `bash scripts/tests/nightly_chaos_guard.sh` | ☐ |
| Mega stress (quick) | `STRESS_QUICK=1 bash scripts/tests/coordinator_mega_stress.sh` | ☐ |
| Prod difficulty | `POOL_BASE=https://hackme.tech/pool bash scripts/tests/difficulty_health.sh` | ☐ |
| Redteam surface | `bash scripts/tests/redteam_surface_smoke.sh` | ☐ |
| ISO on CDN | SHA256 from `SHA256SUMS-iso.txt` on rc11r dist + GRUB “HackMe OS” (see below) | ☐ |
| Site downloads | https://hackme.tech/downloads.html — ISO link HTTP 200 | ☐ |

Automated bundle:

```bash
bash scripts/ops/run_miner_launch_gate.sh
```

Deep audits (on demand):

```bash
LEAK_SPEC_QUICK=1 bash scripts/tests/coordinator_memory_leak_spec.sh   # 3 min
bash scripts/tests/coordinator_memory_leak_spec.sh                     # 2 h / 500 workers
PACKETS=200 COORD_URL=https://hackme.tech/pool/coordinator REQUIRE_STRICT=1 \
  COORD_TOKEN=... bash scripts/tests/hybrid_crypto_matrix.sh
go test ./internal/worksubmit/... -run Matrix -count=1
```

---

## Published ISO verify (any machine)

```bash
# After download:
sha256sum HackMe-OS-0.1.0-rc11r-amd64.iso
# Must match SHA256SUMS-iso.txt on https://hackme.tech/dist/release_0.1.0-rc11r/

bash scripts/tests/verify_hackme_iso.sh /path/to/HackMe-OS-0.1.0-rc11r-amd64.iso
```

**Wrong boot:** Alpine `localhost login:` → wrong USB or wrong file.  
**Correct boot:** GRUB **HackMe OS (live · max performance)** → status / recovery phrase on screen.

---

## Miner — before first hash

1. Pick path: **HackMe OS ISO** (rig) · **Windows Setup** · **Linux bundle** + `desktop_worker_reset.sh`.
2. Verify download SHA256 (`SHA256SUMS-iso.txt` on site).
3. **Save recovery phrase** (ISO Zero-Knowledge) or backup `hackme.ini` / env seed.
4. Note `WORKER_ID` (`hackme-os-status` or dashboard).
5. Register **payout wallet** on coordinator for that worker (fleet docs / operator).
6. Join Telegram for help: [@hackme_ru](https://t.me/hackme_ru) · [@hackme_en](https://t.me/hackme_en) · [@hackme_tech](https://t.me/hackme_tech).

---

## Miner — healthy rig (15 min)

| Check | Expected |
|-------|----------|
| `hackme-os-status` or dashboard Mining | Pool URL reachable, GH/s > 0 on GPU rig |
| Coordinator stats | Worker row present, `accepted` increasing |
| No crash loop | `journalctl -u hackme-miner-worker -f` or worker log stable |
| Thermals | GPU temp sane (Hardware tab / `nvidia-smi`) |

---

## What we tell miners (honest)

- Early **release candidate** — updates without long notice possible.
- **HMC** payouts accrue per pool rules; not instant fiat.
- **Live USB** without disk install = new wallet each reboot unless you run `hackme-os-install`.
- **NVIDIA on ISO:** may need driver + `HACKME_GPU_BACKEND=cuda` after SSD install.
- Bugs → **Telegram** with: OS, GPU model, worker id, screenshot / last 20 log lines.

---

## Later (not blocking launch)

- VPS scale-up (RAM / CPU / disk).
- Merge PR to `main`, drop `rc` tag when stable.
- `coordinator_matrix` harness update for `signature_required` under strict hybrid.
- Full P2P follower mirror optional for miners.

---

## Quick prod pool snapshot

```bash
curl -sS https://hackme.tech/pool/coordinator/api/work/stats | jq '{workers:(.workers|keys),accepted:.accepted_attempts,found:.found_total,target_mod:.target_mod}'
```

---

See also: [HACKME_OS.md](HACKME_OS.md) · [MINER_ISO.md](MINER_ISO.md) · [STATUS.md](STATUS.md) · [SETUP.md](SETUP.md)
