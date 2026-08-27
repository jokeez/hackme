# HackMe miner self-update (W1) — Windows
# Reads latest.json, downloads zip or prefers installer note, verifies SHA256,
# replaces binaries, never overwrites hackme.env / data / logs.
#
# Usage (from install dir or repo):
#   powershell -ExecutionPolicy Bypass -File scripts\ops\update_hackme_miner.ps1
#   powershell -File update_hackme_miner.ps1 -InstallDir "C:\Program Files\HackMe" -DryRun
#
# Env: HACKME_LATEST_URL

param(
  [string]$InstallDir = "",
  [string]$LatestUrl = $env:HACKME_LATEST_URL,
  [switch]$DryRun,
  [switch]$Force,
  [switch]$PreferZip
)

$ErrorActionPreference = "Stop"
function Log([string]$m) { Write-Host "[hackme-update] $m" }

if (-not $InstallDir) {
  if ($PSScriptRoot -and (Test-Path (Join-Path $PSScriptRoot "hackme.exe"))) {
    $InstallDir = $PSScriptRoot
  } elseif (Test-Path "${env:ProgramFiles}\HackMe\hackme.exe") {
    $InstallDir = Join-Path $env:ProgramFiles "HackMe"
  } else {
    $InstallDir = $PSScriptRoot
  }
}
$InstallDir = (Resolve-Path -LiteralPath $InstallDir).Path
Log "install_dir=$InstallDir dry_run=$DryRun"

$urls = @()
if ($LatestUrl) { $urls += $LatestUrl }
$urls += @(
  "https://hackme.tech/dist/latest.json",
  "https://github.com/jokeez/hackme/releases/latest/download/latest.json"
)

$tmp = Join-Path $env:TEMP ("hackme-update-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $latestFile = Join-Path $tmp "latest.json"
  $ok = $false
  foreach ($u in $urls) {
    try {
      Log "fetch $u"
      if ($u -like "file://*") {
        Copy-Item ($u -replace "^file:///", "" -replace "^file://", "") $latestFile -Force
      } else {
        Invoke-WebRequest -Uri $u -OutFile $latestFile -UseBasicParsing -TimeoutSec 60
      }
      $ok = $true
      break
    } catch {
      Log "miss: $u"
    }
  }
  if (-not $ok) { throw "could not fetch latest.json" }

  $latest = Get-Content -Raw $latestFile | ConvertFrom-Json
  if ($latest.schema -ne "hackme.release.latest.v1") {
    throw "unsupported schema: $($latest.schema)"
  }
# Soft refuse if min_updater too high
if ($latest.min_updater -and [int]$latest.min_updater -gt 1) {
  # Protocol 1 ships with this script; bump when breaking latest.json changes land.
  $localUpdater = 1
  if ([int]$latest.min_updater -gt $localUpdater) {
    throw "updater protocol too old (local=$localUpdater need>=$($latest.min_updater)) — re-download update_hackme_miner.ps1"
  }
}
$remote = [string]$latest.version
  Log "remote=$remote"

  $plat = $null
  if (-not $PreferZip) {
    $plat = $latest.platforms | Where-Object { $_.id -eq "windows_installer" } | Select-Object -First 1
  }
  if (-not $plat) {
    $plat = $latest.platforms | Where-Object { $_.id -eq "windows_zip" } | Select-Object -First 1
  }
  if (-not $plat) { throw "no windows platform in latest.json" }

  if ($plat.kind -eq "installer") {
    Log "installer payload detected: $($plat.file)"
    Log "Windows Setup self-update: download + run installer (keeps env if you installed via Setup)."
    $arch = Join-Path $tmp $plat.file
    $dlOk = $false
    foreach ($u in @($plat.url, $plat.mirror_url)) {
      if (-not $u) { continue }
      try {
        Log "download $u"
        if ($u -like "file://*") {
          Copy-Item ($u -replace "^file:///", "" -replace "^file://", "") $arch -Force
        } else {
          Invoke-WebRequest -Uri $u -OutFile $arch -UseBasicParsing -TimeoutSec 600
        }
        $dlOk = $true
        break
      } catch { Log "download miss" }
    }
    if (-not $dlOk) { throw "download failed" }
    $hash = (Get-FileHash -Algorithm SHA256 -Path $arch).Hash.ToLowerInvariant()
    if ($hash -ne $plat.sha256.ToLowerInvariant()) {
      throw "SHA256 mismatch got=$hash want=$($plat.sha256)"
    }
    Log "sha256 ok"
    if ($DryRun) {
      Log "DRY-RUN would launch installer: $arch"
      exit 0
    }
    Log "starting installer (interactive / silent if supported)"
    Start-Process -FilePath $arch -Wait
    Log "OK installer finished — verify version in dashboard"
    exit 0
  }

  # ZIP path — replace binaries in InstallDir
  $arch = Join-Path $tmp $plat.file
  $dlOk = $false
  foreach ($u in @($plat.url, $plat.mirror_url)) {
    if (-not $u) { continue }
    try {
      Log "download $u"
      if ($u -like "file://*") {
        Copy-Item ($u -replace "^file:///", "" -replace "^file://", "") $arch -Force
      } else {
        Invoke-WebRequest -Uri $u -OutFile $arch -UseBasicParsing -TimeoutSec 600
      }
      $dlOk = $true
      break
    } catch { Log "download miss" }
  }
  if (-not $dlOk) { throw "download failed" }
  $hash = (Get-FileHash -Algorithm SHA256 -Path $arch).Hash.ToLowerInvariant()
  if ($hash -ne $plat.sha256.ToLowerInvariant()) {
    throw "SHA256 mismatch"
  }
  Log "sha256 ok"

  $extract = Join-Path $tmp "extract"
  New-Item -ItemType Directory -Path $extract | Out-Null
  Expand-Archive -Path $arch -DestinationPath $extract -Force
  $payload = Get-ChildItem -Path $extract -Recurse -Filter "hackme.exe" | Select-Object -First 1
  if (-not $payload) { throw "hackme.exe not found in zip" }
  $payloadDir = $payload.Directory.FullName

  if ($DryRun) {
    Log "DRY-RUN would copy from $payloadDir → $InstallDir"
    exit 0
  }

  $prev = Join-Path $InstallDir ("previous\" + (Get-Date -Format "yyyyMMddTHHmmssZ"))
  New-Item -ItemType Directory -Path $prev -Force | Out-Null
  foreach ($name in @("hackme.exe", "workerpoh.exe", "workerfuzz.exe", "minersign.exe", "fleetplan.exe")) {
    $src = Join-Path $InstallDir $name
    if (Test-Path $src) { Copy-Item $src $prev -Force }
  }

  Get-Process -Name "hackme","workerpoh","workerfuzz" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

  foreach ($name in @("hackme.exe", "workerpoh.exe", "workerfuzz.exe", "minersign.exe", "fleetplan.exe", "workerpoh-opencl.exe")) {
    $src = Join-Path $payloadDir $name
    if (Test-Path $src) {
      Copy-Item $src (Join-Path $InstallDir $name) -Force
      Log "installed $name"
    }
  }

  foreach ($keep in @("hackme.env", ".env", "data", "logs", "pool.miner.token")) {
    if (Test-Path (Join-Path $InstallDir $keep)) { Log "preserved: $keep" }
  }

  @"
version=$remote
updated_utc=$((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"))
"@ | Set-Content -Path (Join-Path $InstallDir "BUILD_INFO.txt") -Encoding ASCII

  Log "OK updated → $remote (backup $prev)"
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
