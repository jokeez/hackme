@echo off
setlocal EnableExtensions
cd /d "%~dp0"
if not exist "workerpoh-opencl.exe" (
  echo missing workerpoh-opencl.exe
  exit /b 1
)
if not exist "data\node_ed25519.seed" (
  echo missing data\node_ed25519.seed
  exit /b 1
)
set "POOL_TOKEN="
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_POOL_COORDINATOR_TOKEN=" hackme.env') do set "POOL_TOKEN=%%B"
if "%POOL_TOKEN%"=="" exit /b 1
for /f "usebackq delims=" %%S in ("data\node_ed25519.seed") do set "HACKME_MINER_ED25519_SEED_HEX=%%S"
set "HACKME_WORKER_CLAIM_COOLDOWN_MS=28000"
set "HACKME_WORKER_SIGN_SUBMITS=1"
set "WORKER_ID=worker-desktop-1rgp4ge"
for /f "usebackq delims=" %%H in (`powershell -NoProfile -Command "(hostname).ToLower()"`) do set "WORKER_ID=worker-%%H"
start "HackMe OpenCL" /MIN workerpoh-opencl.exe ^
  -coord https://hackme.tech/pool/coordinator ^
  -token %POOL_TOKEN% ^
  -worker %WORKER_ID% ^
  -batch 1048576 ^
  -gpu-chunk 524288 ^
  -search-timeout-ms 4500 ^
  -gpu-backend opencl >> logs\worker-opencl-live.log 2>&1
echo started %WORKER_ID% >> logs\worker-opencl-live.log
exit /b 0
