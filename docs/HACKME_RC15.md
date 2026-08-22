# HackMe 0.1.0-rc15 — B2B fuzz Phase 2 + pool anticheat

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux/fuzz **rc15** · hub node **0.1.0-rc15** (deploy 2026-08-22).

## Highlights

| Area | rc15 |
|------|------|
| **Mining dashboard** | New `#mining` fuzz banner — libheif OSS series archived CLEAN; customer B2B campaigns prioritized |
| **Fuzz product** | Wizard packs: `secrets`, `script_bounds`, `filter_utf8`, `parser_expat` · Scan/Audit/Deep tiers |
| **Pool anticheat** | Dynamic worker lease, exec cap 64 on hub, segment replay fail-closed, coverage bitmap @ 8192 |
| **What's new** | Dashboard notice rc15; stale libheif Day 1 messaging removed |

## Version alignment

- `main.go` · `scripts/release/CURRENT_VERSION` · `web/site/assets/app.js` · `dashboard.html` `DASHBOARD_UI_VER`

## Build

```bash
VERSION=0.1.0-rc15 bash scripts/release/make_release_bundle.sh
```

## SHA256 (Win/Linux/fuzz)

```
c4e3cbc6bc51815278ae24f3c5ca84b9d77b16d81ff921a6333f8454b916e13d  hackme_0.1.0-rc15_windows.zip
8ba1b5bbd1cd98adbc1418e6e196c75ce50536fe6ff30dc1dfb26ccde7e31085  hackme_0.1.0-rc15_windows_setup.zip
30f2bf98e10ec8ba6c942739bad17fc4e65e0ea3212f5c846bc8521659773a54  hackme_0.1.0-rc15_linux.tar.gz
fc9e6fe4a5065e87156330491173cdb79c340fa55ab796c37fa1d77b02601832  HackMe-Setup-0.1.0-rc15.exe
666ead77c433eb6c6517cabcbcceabd9079c8aed57a1731ba91f36a3de85fd02  hackme-fuzzing-0.1.0-rc15-linux-amd64
5a6989eb132e42c53726655b3af835ddb2eedf8cfc88e076af09abae877ae93e  hackme-fuzzing-0.1.0-rc15-windows-amd64.exe
```

Full list: `dist/release_0.1.0-rc15/SHA256SUMS.txt` · GitHub tag `0.1.0-rc15`

## GitHub release

```bash
gh auth login -h github.com
gh release create 0.1.0-rc15 dist/release_0.1.0-rc15/*.{zip,tar.gz,exe,txt,json,md} \
  dist/release_0.1.0-rc15/hackme-fuzzing-* \
  dist/release_0.1.0-rc15/hackme-fuzzing-build-* \
  --title "HackMe 0.1.0-rc15 — B2B fuzz Phase 2" \
  --notes-file dist/release_0.1.0-rc15/RELEASE_NOTES.md
```

ISO: add `HackMe-OS-0.1.0-rc15-amd64.iso` + `SHA256SUMS-iso.txt` when ISO build completes.
