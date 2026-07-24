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

```text
58c1f88957239a53c116db0c6ee980f8659940c0a25833d5473996fc7ecc3792  hackme_0.1.0-rc13_windows.zip
846812f37b8113915388501a39be53e5ad19fb6165ca7b6f64d38be94dc97e92  hackme_0.1.0-rc13_windows_setup.zip
ba6307040053891766232a055051a46b78f5e820822b8668ab2354f024d28ae1  HackMe-Setup-0.1.0-rc13.exe
c7d9fe2d0f901f1f973ced43d9af8828728cd9786b293b83b52f1a36ae7c8e90  hackme_0.1.0-rc13_linux.tar.gz
94e4d222d280878378edd65debbc600cce945e4fed016af16e7aaf0ef7ba0ace  hackme-fuzzing-0.1.0-rc13-linux-amd64
1c93f976b81d29c7e244fb977ccbbdb01338ee10e61960651235ed96cd468e89  hackme-fuzzing-0.1.0-rc13-windows-amd64.exe
69220338e6813205720ee4541552027eb442eecf89887e93a42140bddc001fc6  hackme-fuzzing-build-0.1.0-rc13-linux-amd64
3f73918dfe69088551d282cfecfb28c78c9aafe97bb7830bab5a7b2aa9da91e3  hackme-fuzzing-build-0.1.0-rc13-windows-amd64.exe
1adf63cc8252c25ae040fc9362d78ac3209f0a5bf8babb05cd394f156ad5e60f  HackMe-OS-0.1.0-rc13-amd64.iso
```

## Operator rebuild

```bash
VERSION=0.1.0-rc13 bash scripts/ops/release_rc12w_publish.sh
NODE_SSH=hackme-vps SYNC_DIST=1 bash scripts/ops/deploy_hackme_site.sh
```

Historical: [HACKME_RC12W.md](HACKME_RC12W.md)
