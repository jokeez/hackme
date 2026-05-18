# Release Packaging (Windows + Linux)

This guide prepares polished distributables:

- Windows ZIP payload with installer template (`Inno Setup`)
- Linux tarball with installer script (`systemd` + desktop entry)
- checksums for release integrity
- optional Windows Authenticode signing
- nightly repeatable release pipeline

## 1) Build release bundles

```bash
cd /home/kapa/Desktop/HackMe
VERSION=1.0.0 bash scripts/release/make_release_bundle.sh
```

Artifacts:

- `dist/release_1.0.0/hackme_1.0.0_windows.zip`
- `dist/release_1.0.0/hackme_1.0.0_linux.tar.gz`
- `dist/release_1.0.0/SHA256SUMS.txt`

## 2) Windows installer (admin install)

The release includes `hackme.iss` template in the windows payload.

Build installer on Windows:

```powershell
iscc /DMyAppVersion=1.0.0 /DMyAppPublisher="HackMe Labs" dist\release_1.0.0\windows\hackme.iss
```

`PrivilegesRequired=admin` is enabled in installer setup.

### Optional: embed EXE icon/version/manifest

Before final Windows build, generate resource file (`.syso`) on Windows:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\release\windows\build_windows_resources.ps1 -Version 1.0.0 -IconPath scripts\release\windows\hackme.ico
```

This uses:

- `scripts/release/windows/versioninfo.json.template`
- `scripts/release/windows/app.manifest` (`requireAdministrator`)

Then rebuild release bundle to include embedded resources.

## 3) Windows code signing

Use script:

```powershell
powershell -ExecutionPolicy Bypass -File .\dist\release_1.0.0\windows\sign_windows.ps1 `
  -FilePath .\Output\HackMe-Setup-1.0.0.exe `
  -PfxPath .\certs\codesign.pfx `
  -PfxPassword "<password>"
```

Notes:

- Real signing requires your own trusted code-signing certificate.
- Script uses `signtool.exe` from Windows SDK.

## 4) Linux install (polished mode)

On Linux target host:

```bash
sudo bash install_hackme.sh --archive ./hackme_1.0.0_linux.tar.gz
```

What it does:

- installs binary to `/opt/hackme`
- creates `.env` if missing
- creates `hackme` service user
- installs and starts `hackme-node.service`
- installs desktop launcher (`hackme.desktop`)

## 5) Version metadata in API

Release metadata is injected with linker flags and exposed via:

- `GET /api/status` → `version`, `commit`, `build_date`

This helps support and release diagnostics.

## 6) Nightly pipeline

```bash
VERSION=1.0.0-rc2 bash scripts/release/release_nightly.sh
```

It runs:

1) `go test ./...`
2) release bundle build
3) artifact checksum verification
