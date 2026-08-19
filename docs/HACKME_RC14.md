# HackMe 0.1.0-rc14x — desktop/pool channel + release bundle

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux/fuzz bundle on a single **rc14x** channel (HackMe OS ISO not shipped in this bundle; publishing soon).

## Release (2026-08-19, commit `b4a8c0b06c0d`)

Linux tarball SHA256:

`16ea5bcf40c0a0449b19b807fd15690c4eaedad7f0da666539243ab53ba4f2c9`

## Published artifacts

| Artifact | File |
|----------|------|
| Windows portable | `hackme_0.1.0-rc14x_windows.zip` |
| Windows setup ZIP | `hackme_0.1.0-rc14x_windows_setup.zip` |
| Windows installer | `HackMe-Setup-0.1.0-rc14x.exe` |
| Linux bundle | `hackme_0.1.0-rc14x_linux.tar.gz` |
| Fuzz CLI + build helper | `hackme-fuzzing*-0.1.0-rc14x-*` |
| HackMe OS ISO | *(not shipped in rc14x — ISO publishing soon)* |

## Verify

Download artifacts and their checksum files from:

- `https://hackme.tech/dist/release_0.1.0-rc14x/`
- `https://github.com/jokeez/hackme/releases/tag/0.1.0-rc14x`

Verify ordinary artifacts with `SHA256SUMS.txt`.

HackMe OS ISO is not included in this `rc14x` bundle, so `SHA256SUMS-iso.txt` is not published for this release line.

```text
f374688925849d219767fedff5660066b6387cffa40103a1bfe8db546f35ead3  hackme_0.1.0-rc14x_windows.zip
20da968ef7b5679e4b40e4f5e05a6156017e0c4beedad4757a63e9548d115ad3  hackme_0.1.0-rc14x_windows_setup.zip
16ea5bcf40c0a0449b19b807fd15690c4eaedad7f0da666539243ab53ba4f2c9  hackme_0.1.0-rc14x_linux.tar.gz
f0ac96be3c4f9c3da28cb7f4ea9476df92c1d4789ec1c67655e3a0a045c19dbf  HackMe-Setup-0.1.0-rc14x.exe
99cab528dbbc1ac8e30913f60893af5c55bc999f9ad1007fa611fd0e199498a7  Install-HackMe.ps1
0ac66574d6c2eb2bc605f3d254d912f08bb7f46befb8b66db9edc61618c8d243  HackMe-Install.cmd
087c44f3a4b44754459cd5bce92b6d41ffd188b38a0a73a273c48f04c1dabceb  hackme-fuzzing-0.1.0-rc14x-linux-amd64
654aeebc5d6ed45474c787fe50620d209f16d3c4e3493155e0dadced197adaac  hackme-fuzzing-0.1.0-rc14x-windows-amd64.exe
fd8e39969959d1f2a6e92b2e624fb287ba187ad27849d443dea1795c5b3c5c47  hackme-fuzzing-build-0.1.0-rc14x-linux-amd64
869b2e619a3be2f0893cfe548095ea0e469b640be9388a1d8f2989bf59b942ad  hackme-fuzzing-build-0.1.0-rc14x-windows-amd64.exe
```

## Operator rebuild

```bash
# Publisher script filename is historical (rc12w); VERSION= selects the channel.
VERSION=0.1.0-rc14x bash scripts/ops/release_rc12w_publish.sh
NODE_SSH=hackme-vps SYNC_DIST=1 bash scripts/ops/deploy_hackme_site.sh
```

# HackMe 0.1.0-rc14x — dashboard/pool UX + faster mining control

## Changelog

- Pool fuzz: customer-first claim priority; claim p50 in ~milliseconds (was ~6s); added/updated indexes.
- Hybrid dig: enabled by default with parameters `2/50/2000/10%`.
- Bootstrap resync: include `owner_ref`.
- Dashboard: fix fuzz marketplace rate + ETA display (runs/min, delta-based rate, correct "warming up" rules).
- Fuzz report plane (pre-exchange honesty): clarified `evidence_window` semantics (fetched-window vs full-history) + explicit raw vs grouped counters in JSON/HTML/CSV.

## E2E proof (customer campaign)

- Customer 256/256 completion: ~26 minutes.
- Observed speedup: ~4x faster than the bootstrap-tier baseline.

## Operator rebuild

```bash
VERSION=0.1.0-rc14x bash scripts/release/make_release_bundle.sh
VERSION=0.1.0-rc14x bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true
```

If you also need the ISO publish + site publishing:

```bash
# Publisher script filename is historical (rc12w); VERSION= selects the channel.
SKIP_ISO=0 SKIP_INSTALLER=0 SKIP_GATES=0 VERSION=0.1.0-rc14x bash scripts/ops/release_rc12w_publish.sh
```

