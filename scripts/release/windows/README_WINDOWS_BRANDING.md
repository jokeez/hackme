# Windows Branding Notes

Branding assets used by release pipeline:

- `scripts/release/windows/hackme.ico`
- `scripts/release/windows/hackme.png`

Automated path (recommended):

```bash
VERSION=1.0.0 bash scripts/release/make_release_bundle.sh
```

It calls:

```bash
VERSION=1.0.0 bash scripts/release/windows/build_windows_resources.sh
```

Manual PowerShell path (Windows host):

```powershell
powershell -ExecutionPolicy Bypass -File scripts\release\windows\build_windows_resources.ps1 -Version 1.0.0 -IconPath scripts\release\windows\hackme.ico
```

This generates:

- `resource_windows_amd64.syso`

End-user convenience (copied into `dist/release_<ver>/windows/` by `make_release_bundle.sh`):

- `start_hackme_public_pool.bat` — recommended: public pool at `https://hackme.tech`, opens dashboard → Mining.
- `start_hackme_desktop_mode.bat` — generates `.env.desktop.windows` (worker profile + public authority).
- `start_hackme_dashboard.bat` — node only + browser (set env / tokens in UI).

The `.syso` is picked by Go build automatically and embeds:

- file version metadata
- icon
- app manifest (`requireAdministrator`)
