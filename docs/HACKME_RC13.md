# HackMe 0.1.0-rc13 — 48-hour pool soak PASS

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) after the successful `pool_48h_soak_20260722T162444Z` mining soak.

## Soak evidence

- Window: approximately 48 hours; no gaps longer than 10 minutes.
- Pool hashrate: approximately 70–197 GH/s, average approximately 137 GH/s.
- Chain tip: approximately 199444 → 203990.
- Thirteen workers observed; hub mining remained enabled.

## Published artifacts

| Artifact | File |
|----------|------|
| Windows portable | `hackme_0.1.0-rc13_windows.zip` |
| Windows setup ZIP | `hackme_0.1.0-rc13_windows_setup.zip` |
| Windows installer | `HackMe-Setup-0.1.0-rc13.exe` |
| Linux bundle | `hackme_0.1.0-rc13_linux.tar.gz` |
| Fuzz CLI + build helper | `hackme-fuzzing*-0.1.0-rc13-*` |
| HackMe OS ISO | `HackMe-OS-0.1.0-rc13-amd64.iso` |

## Verify

Download artifacts and their checksum files from:

- `https://hackme.tech/dist/release_0.1.0-rc13/`
- `https://github.com/jokeez/hackme/releases/tag/0.1.0-rc13`

Verify ordinary artifacts with `SHA256SUMS.txt` and the bootable image with
`SHA256SUMS-iso.txt`.

## Operator rebuild

```bash
VERSION=0.1.0-rc13 bash scripts/ops/release_rc12w_publish.sh
NODE_SSH=hackme-vps SYNC_DIST=1 bash scripts/ops/deploy_hackme_site.sh
```

Historical: [HACKME_RC12W.md](HACKME_RC12W.md)
