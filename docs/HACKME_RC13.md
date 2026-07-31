# HackMe 0.1.0-rc13 — desktop/pool channel + security hotfix

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux/fuzz/ISO on a single **rc13** channel.

## Hotfix (2026-07-31, commit `d15ec25`)

Same tag **0.1.0-rc13**, new SHA256 (no version bump). Patches private bounty items B1–B5:

- Desktop simpleSign requires loopback Host (DNS-rebind defense)
- Fleet `-gpuN` merge no longer attributes conflicting sibling accrual to attacker address
- Coordinator prefers nginx X-Real-IP/XFF; CF-Connecting-IP only from Cloudflare peers
- Fuzz escrow `miner_address` always requires hybrid PoP
- Bootstrap VPS template defaults `DESKTOP_MODE=0`

Linux tarball SHA256:

`bd3ff39ccd697729755e5ac0ed34076c04e38b1b348e46a656a3549884e36693  hackme_0.1.0-rc13_linux.tar.gz`

## Soak evidence (earlier rc13 window)

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
ab6ff12bbc74bd34b15d3b460e2d61d5f5b0643987b3c06535c717cec8a8d457  hackme_0.1.0-rc13_windows.zip
1d4205b7004b91d8278a5ed37a2c7140eefba21b1543a1ee6400e9687f56c135  hackme_0.1.0-rc13_windows_setup.zip
bd3ff39ccd697729755e5ac0ed34076c04e38b1b348e46a656a3549884e36693  hackme_0.1.0-rc13_linux.tar.gz
ba6307040053891766232a055051a46b78f5e820822b8668ab2354f024d28ae1  HackMe-Setup-0.1.0-rc13.exe
99cab528dbbc1ac8e30913f60893af5c55bc999f9ad1007fa611fd0e199498a7  Install-HackMe.ps1
0ac66574d6c2eb2bc605f3d254d912f08bb7f46befb8b66db9edc61618c8d243  HackMe-Install.cmd
4bf7bc0c9341c0f2d6edbfe48afc062e5006539dde5d03e0fb294ddb2581e5ae  hackme-fuzzing-0.1.0-rc13-linux-amd64
5fddd962476623fa646865bbe8df7481bd2f3fcc8bf070247666096f80961d34  hackme-fuzzing-0.1.0-rc13-windows-amd64.exe
df0a84a4db37b7b60164afd1ccd810ebe95a2aa3022cd0e7b033ec120d7d299e  hackme-fuzzing-build-0.1.0-rc13-linux-amd64
8bda0fab19302cf00b537982a06d64ac160eb1ae9bb881a4968e6bbc278a4716  hackme-fuzzing-build-0.1.0-rc13-windows-amd64.exe
1adf63cc8252c25ae040fc9362d78ac3209f0a5bf8babb05cd394f156ad5e60f  HackMe-OS-0.1.0-rc13-amd64.iso
```

## Operator rebuild

```bash
VERSION=0.1.0-rc13 bash scripts/ops/release_rc12w_publish.sh
NODE_SSH=hackme-vps SYNC_DIST=1 bash scripts/ops/deploy_hackme_site.sh
```

Historical: [HACKME_RC12W.md](HACKME_RC12W.md)
