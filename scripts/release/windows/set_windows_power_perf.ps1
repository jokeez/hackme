# Best-effort: High performance power plan for fair GPU mining on Windows laptops.
$ErrorActionPreference = 'SilentlyContinue'
$guid = '8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c'
$cur = (powercfg /getactivescheme 2>$null) -join ' '
if ($cur -match $guid) { exit 0 }
powercfg /setactive $guid 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) {
    powercfg -duplicatescheme $guid 2>$null | Out-Null
    powercfg /setactive $guid 2>$null | Out-Null
}
