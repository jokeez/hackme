# HackMe 0.1.0-rc11k — launch candidate (production miner)

**Status:** Public download channel · not a demo build  
**Pool:** https://hackme.tech/pool/coordinator  
**Commit:** see `BUILD_INFO.txt` in `dist/release_0.1.0-rc11k/`

## Why rc11k (after rc11j)

| Area | What changed |
|------|----------------|
| **Public API** | Fixed `/api/status` hangs when the node fetched canonical tip from itself (P2P / public authority loop). Lite polls stay sub-second; full status responds in a few seconds. |
| **Onboarding gates** | `new_miner_journey`, `economics_confidence`, `maximum_resilience` — PASS on production. |
| **Dashboard truth** | Worker GH/s and session come from the coordinator (`telemetry_source=coordinator`), not stale local estimates. |
| **Wallet UX** | Local PoH mining keeps earnings on `local_db` until round ends (no mid-round canonical proxy flicker). |
| **Golden path** | Linux `start_pool_miner.sh`, Windows **HackMe Miner**, HackMe OS USB — one entry per platform (`docs/SETUP.md`). |

rc11j remains valid for GPU matrix / CUDA fallback; **rc11k** is the recommended channel for new miners and reinstalls.

## Artifacts

| Artifact | Path |
|----------|------|
| Windows installer | `HackMe-Setup-0.1.0-rc11k.exe` |
| Linux tarball | `hackme_0.1.0-rc11k_linux.tar.gz` |
| HackMe OS ISO | `HackMe-OS-0.1.0-rc11k-amd64.iso` |
| Checksums | `SHA256SUMS.txt`, `SHA256SUMS-iso.txt` |

Downloads: https://hackme.tech/downloads.html

## Verify after install

```bash
curl -fsS 'https://hackme.tech/api/status?lite=1' | jq '{version,commit,network_mode_active}'
bash scripts/ops/new_miner_journey_gate.sh   # WORKER_SMOKE=0 from dev machine
```

## Operator notes

- Rebuild **required** for bundled `hackme.exe` / `hackme` (node). `workerpoh` unchanged unless you ship a new worker build from the same bundle script.
- VPS hub: deploy `hackme-node` before publishing `dist/` so pool API matches installer commit.
