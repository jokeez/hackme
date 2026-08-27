# HackMe 0.1.0-rc11l — HackMe OS ISO channel

> **Historical archive.** Current channel: [HACKME_RC16.md](../../HACKME_RC16.md) (`0.1.0-rc16`).


**Status:** ISO live-boot fix shipped; **current ISO on downloads** until rc11m ISO is built. Win/Linux channel: [HACKME_RC11M.md](HACKME_RC11M.md).

## What changed vs rc11k

| Area | rc11k | rc11l |
|------|-------|-------|
| HackMe OS ISO | Overlay modules in `local-premount` (never runs on live USB) | `scripts/casper-premount/05-hackme-overlay-modules` |
| Windows / Linux | rc11k build | Same binaries, renamed on `dist/release_0.1.0-rc11l/` |
| ISO SHA256 | `ed5c9e7…` / older | `81a1f1f180f72d1d56c29990e3a872ff1a9ecd0e0f3d141c5b49ad6cad30b0db` |

## Downloads

- https://hackme.tech/downloads.html
- ISO sums: https://hackme.tech/dist/release_0.1.0-rc11l/SHA256SUMS-iso.txt

## Operator

```bash
bash scripts/tests/verify_hackme_iso.sh dist/release_0.1.0-rc11l/HackMe-OS-0.1.0-rc11l-amd64.iso
bash scripts/tests/site_release_consistency_gate.sh
```

Historical launch notes: [HACKME_RC11K_LAUNCH_CANDIDATE.md](HACKME_RC11K_LAUNCH_CANDIDATE.md)
