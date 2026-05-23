# Launch HackMe Setup — unblocks Mark-of-the-Web and shows a message if start fails.
param(
    [string]$SetupExe = ""
)

$ErrorActionPreference = "Stop"

function Show-Msg([string]$Text, [int]$Icon = 16) {
    try {
        $ws = New-Object -ComObject WScript.Shell
        $null = $ws.Popup($Text, 0, "HackMe Install", $Icon)
    } catch {
        Write-Host $Text
    }
}

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $SetupExe) {
    $f = Get-ChildItem -Path $here -Filter "HackMe-Setup-*.exe" -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if ($f) { $SetupExe = $f.FullName }
}
if (-not $SetupExe -or -not (Test-Path -LiteralPath $SetupExe)) {
    Show-Msg "HackMe-Setup-*.exe not found.`nDownload: https://hackme.tech/downloads.html"
    exit 1
}

try { Unblock-File -LiteralPath $SetupExe -ErrorAction SilentlyContinue } catch { }

try {
    $p = Start-Process -FilePath $SetupExe -PassThru
    if (-not $p) {
        Show-Msg "Could not start installer.`nRight-click HackMe-Setup.exe -> Run as administrator`nor use portable ZIP from downloads."
        exit 1
    }
    exit 0
} catch {
    Show-Msg "Installer error: $($_.Exception.Message)`nTry portable ZIP from https://hackme.tech/downloads.html"
    exit 1
}
