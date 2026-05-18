# HackMe Release Pipeline

## Build release bundle

```bash
VERSION=1.0.0-rc3 bash scripts/release/make_release_bundle.sh
```

Output:

- Windows zip includes **`hackme.exe`**, **`workerpoh.exe`**, **`minersign.exe`**, **`start_hackme_public_pool.bat`** (recommended), **`start_hackme_desktop_mode.bat`**, **`start_hackme_dashboard.bat`**, and **`RELEASE_QUICKSTART.md`**. Solo/local-only mining launchers were removed.
- `dist/release_<VERSION>/hackme_<VERSION>_windows.zip`
- `dist/release_<VERSION>/hackme_<VERSION>_linux.tar.gz`
- `dist/release_<VERSION>/SHA256SUMS.txt`
- `dist/release_<VERSION>/BUILD_INFO.txt`
- `dist/release_<VERSION>/RELEASE_MANIFEST.json`
- `dist/release_<VERSION>/SMOKE_REPORT.txt`

## Verify existing bundle

```bash
bash scripts/release/verify_artifacts.sh "dist/release_<VERSION>"
```

## Run smoke checks only

```bash
bash scripts/release/smoke_artifacts.sh "dist/release_<VERSION>"
```

## Nightly-style pipeline

```bash
VERSION=nightly_$(date -u +%Y%m%dT%H%M%SZ) bash scripts/release/release_nightly.sh
```

## Windows installer (Inno Setup)

`iscc` runs only on **Windows** (or a Windows CI runner). After `make_release_bundle.sh`:

```powershell
pwsh -File scripts/release/windows/build_installer.ps1 -Version 0.1.0-rc8
```

Installer reads `scripts/release/windows/hackme.iss` and expects `dist/release_<VERSION>/windows/hackme.exe` and companion files. Upload the generated `HackMe-Setup-<VERSION>.exe` to `/dist/release_<VERSION>/` on the site if you ship it next to the zip (optional).

## Publish site + downloads on VPS

From a machine with SSH access:

```bash
NODE_SSH=hackme-vps NODE_DEPLOY_DIR=/opt/hackme bash scripts/ops/deploy_hackme_site.sh
```

Set `SKIP_DIST=1` for HTML-only. Default syncs `web/site/` and full `dist/`.

