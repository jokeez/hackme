@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

set "ADMIN_TOKEN="
set "POOL_TOKEN="
if exist "hackme.env" (
  for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_ADMIN_TOKEN=" hackme.env 2^>nul') do set "ADMIN_TOKEN=%%B"
  for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_POOL_COORDINATOR_TOKEN=" hackme.env 2^>nul') do set "POOL_TOKEN=%%B"
)
if "!POOL_TOKEN!"=="" if exist "pool.miner.token" (
  for /f "usebackq delims=" %%T in ("pool.miner.token") do set "POOL_TOKEN=%%T"
)
if "!ADMIN_TOKEN!"=="" exit /b 0
if "!POOL_TOKEN!"=="" exit /b 0
if /I "!POOL_TOKEN!"=="REPLACE_WITH_POOL_TOKEN" exit /b 0

for /f "usebackq delims=" %%H in (`powershell -NoProfile -Command "(hostname).ToLower() -replace '[^a-z0-9-]','-' "`) do set "HOST=%%H"
set "WORKER_ID=worker-!HOST!"
set "GPU_BACKEND=auto"
set "WORKER_BATCH=4194304"
if exist "workerpoh-opencl.exe" set "GPU_BACKEND=opencl"
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_GPU_BACKEND=" hackme.env 2^>nul') do set "GPU_BACKEND=%%B"
if /I "!GPU_BACKEND!"=="auto" if exist "workerpoh-opencl.exe" set "GPU_BACKEND=opencl"
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_WORKER_BATCH_SIZE=" hackme.env 2^>nul') do set "WORKER_BATCH=%%B"

set /a N=0
:waitloop
curl -fsS -o nul -H "X-Hackme-Admin-Token: !ADMIN_TOKEN!" http://127.0.0.1:8080/api/status 2>nul
if %ERRORLEVEL%==0 goto startworker
timeout /t 2 /nobreak >nul
set /a N+=1
if !N! LSS 45 goto waitloop
exit /b 0

:startworker
curl -fsS -X POST -H "Content-Type: application/json" -H "X-Hackme-Admin-Token: !ADMIN_TOKEN!" ^
  -d "{\"coord_url\":\"https://hackme.tech/pool/coordinator\",\"worker_id\":\"!WORKER_ID!\",\"batch_size\":!WORKER_BATCH!,\"gpu_backend\":\"!GPU_BACKEND!\"}" ^
  http://127.0.0.1:8080/api/worker/start >nul 2>&1
exit /b 0
