# Archived ops scripts

One-shot marathon and wave-specific helpers kept for audit/repro. **Prefer canonical entry points** in [../README.md](../README.md).

| Pattern | Replacement |
|---------|-------------|
| `run_oss_cve_wave4.sh` … `wave30.sh` | `run_oss_cve_wave.sh` + `upstream/oss_cve_high_yield.json` |
| `run_oss_cve_watch*.sh`, `away_libheif_*` | Series closed — see `docs/OSS_CVE_HUNT.md` |
| `run_bounty_hunt_wave13.sh`, `run_bounty_max_push.sh`, … | `run_bounty_autopilot.sh` phases |
| `release_rc11*_publish.sh`, `deploy_release_rc11g_iso.sh` | rc16 release pipeline in `scripts/release/` |

Frozen copies may remain on operator disks under this folder without being tracked in git.
