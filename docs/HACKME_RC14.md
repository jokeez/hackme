# HackMe 0.1.0-rc14 — desktop/pool channel + release bundle

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux/fuzz/ISO on a single **rc14** channel.

## Release (2026-08-11, commit `ac48b36`)

Linux tarball SHA256:

`0cb89a2dd6cac598add2340db70f336f81f982f3638adbbd3d922eb73c41f4d0  hackme_0.1.0-rc14_linux.tar.gz`

## Published artifacts

| Artifact | File |
|----------|------|
| Windows portable | `hackme_0.1.0-rc14_windows.zip` |
| Windows setup ZIP | `hackme_0.1.0-rc14_windows_setup.zip` |
| Windows installer | `HackMe-Setup-0.1.0-rc14.exe` |
| Linux bundle | `hackme_0.1.0-rc14_linux.tar.gz` |
| Fuzz CLI + build helper | `hackme-fuzzing*-0.1.0-rc14-*` |
| HackMe OS ISO | `HackMe-OS-0.1.0-rc14-amd64.iso` |

## Verify

Download artifacts and their checksum files from:

- `https://hackme.tech/dist/release_0.1.0-rc14/`
- `https://github.com/jokeez/hackme/releases/tag/0.1.0-rc14`

Verify ordinary artifacts with `SHA256SUMS.txt` and the bootable image with:
`SHA256SUMS-iso.txt`.

```text
139de21d4aff6d77144d70bddd66e0cadc24a15c1c110a66772b9343757a1f08  hackme_0.1.0-rc14_windows.zip
b552c7ddc80f33642320fa0695c5332452903b1fb705449380ca26bac69ad9b9  hackme_0.1.0-rc14_windows_setup.zip
0cb89a2dd6cac598add2340db70f336f81f982f3638adbbd3d922eb73c41f4d0  hackme_0.1.0-rc14_linux.tar.gz
013bbed4859287e051df0f32b39d978db9f60cac14eb4ff3f82a453f9db58287  HackMe-Setup-0.1.0-rc14.exe
99cab528dbbc1ac8e30913f60893af5c55bc999f9ad1007fa611fd0e199498a7  Install-HackMe.ps1
0ac66574d6c2eb2bc605f3d254d912f08bb7f46befb8b66db9edc61618c8d243  HackMe-Install.cmd
b31c875f691af0ab79511ef41dbfacaf1d859c3e9abe1afe5dfc30294e679b16  hackme-fuzzing-0.1.0-rc14-linux-amd64
daa7a46c43290a4fd041de71cf6f82762c6f3521b1c99b03d84d6308ee457347  hackme-fuzzing-0.1.0-rc14-windows-amd64.exe
1150c05901b2a2b955d4732da5fd49f7e61957287cc87112e5bc2954b66e5a46  hackme-fuzzing-build-0.1.0-rc14-linux-amd64
f8423e3ffae684f37ebd7ae51ff5786ed6cf22039fc87b428b7cccb52456448a  hackme-fuzzing-build-0.1.0-rc14-windows-amd64.exe
1adf63cc8252c25ae040fc9362d78ac3209f0a5bf8babb05cd394f156ad5e60f  HackMe-OS-0.1.0-rc14-amd64.iso
```

## Operator rebuild

```bash
VERSION=0.1.0-rc14 bash scripts/ops/release_rc12w_publish.sh
NODE_SSH=hackme-vps SYNC_DIST=1 bash scripts/ops/deploy_hackme_site.sh
```

# HackMe 0.1.0-rc14 — dashboard/pool UX + faster mining control

## Changelog

- Pool fuzz: customer-first claim priority; claim p50 in ~milliseconds (was ~6s); added/updated indexes.
- Hybrid dig: enabled by default with parameters `2/50/2000/10%`.
- Bootstrap resync: include `owner_ref`.
- Dashboard: fix fuzz marketplace rate + ETA display (runs/min, delta-based rate, correct "warming up" rules).

## E2E proof (customer campaign)

- Customer 256/256 completion: ~26 minutes.
- Observed speedup: ~4x faster than the bootstrap-tier baseline.

## Operator rebuild

```bash
VERSION=0.1.0-rc14 bash scripts/release/make_release_bundle.sh
VERSION=0.1.0-rc14 bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true
```

If you also need the published ISO + site publishing:

```bash
SKIP_ISO=0 SKIP_INSTALLER=0 SKIP_GATES=0 VERSION=0.1.0-rc14 bash scripts/ops/release_rc12w_publish.sh
```

