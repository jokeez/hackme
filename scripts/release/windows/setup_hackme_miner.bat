@echo off
setlocal EnableExtensions EnableDelayedExpansion
title HackMe — install miner (one-time)
cd /d "%~dp0"

set "INSTALL_DIR=C:\HackMe"
set "SRC_DIR=%~dp0"

echo.
echo === HackMe miner setup ===
echo Source: %SRC_DIR%
echo Install: %INSTALL_DIR%
echo.

if not exist "%SRC_DIR%hackme.exe" (
  echo ERROR: hackme.exe not found next to this script.
  echo Run setup from the extracted HackMe folder ^(or re-download hackme_*_windows_setup.zip^).
  pause
  exit /b 1
)

if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"

echo Copying files...
xcopy /E /I /Y /Q "%SRC_DIR%*" "%INSTALL_DIR%\" >nul
if errorlevel 1 (
  echo ERROR: copy failed. Close HackMe if it is running and retry.
  pause
  exit /b 1
)

cd /d "%INSTALL_DIR%"

rem --- pool token: always prefer pool.miner.token from release zip ---
set "POOL_TOKEN="
if exist "pool.miner.token" (
  for /f "usebackq delims=" %%T in ("pool.miner.token") do set "POOL_TOKEN=%%T"
)
if "!POOL_TOKEN!"=="" (
  if exist "hackme.env" (
    for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_POOL_COORDINATOR_TOKEN=" hackme.env 2^>nul') do set "POOL_TOKEN=%%B"
  )
)
if "!POOL_TOKEN!"=="" (
  echo WARN: pool.miner.token missing — mining will fail until operator sends pool token.
  set "POOL_TOKEN=REPLACE_WITH_POOL_TOKEN"
)
if /I "!POOL_TOKEN!"=="ВАШ_РЕАЛЬНЫЙ_ТОКЕН_ПУЛА" set "POOL_TOKEN=REPLACE_WITH_POOL_TOKEN"
if /I "!POOL_TOKEN!"=="REPLACE_WITH_POOL_TOKEN" (
  if exist "pool.miner.token" for /f "usebackq delims=" %%T in ("pool.miner.token") do set "POOL_TOKEN=%%T"
)

rem --- local dashboard admin token (auto, unique per PC) ---
set "ADMIN_TOKEN="
if exist "hackme.env" (
  for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_ADMIN_TOKEN=" hackme.env 2^>nul') do (
    set "ADMIN_TOKEN=%%B"
  )
)
if "!ADMIN_TOKEN!"=="" (
  for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "$b=New-Object byte[] 24; [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b); ($b|ForEach-Object ToString x2) -join ''"`) do set "ADMIN_TOKEN=%%i"
)

set "RIG_PROFILE="
set "GPU_BACKEND=auto"
set "WORKER_BATCH=4194304"
set "GPU_CHUNK=4194304"
set "SEARCH_MS=2500"
set "CLAIM_MS=0"
set "TEMP_PAUSE=83"
set "TEMP_RESUME=76"
set "CALIB_GHS="
wmic path win32_VideoController get name 2>nul | findstr /I "580" >nul | findstr /I "2048" >nul && (
  set "RIG_PROFILE=amd_rx580_2048sp"
  set "GPU_BACKEND=auto"
  set "WORKER_BATCH=1048576"
  set "GPU_CHUNK=524288"
  set "SEARCH_MS=4500"
  set "CLAIM_MS=150"
  set "TEMP_PAUSE=78"
  set "TEMP_RESUME=72"
  set "CALIB_GHS=0.12"
)
if "!RIG_PROFILE!"=="" (
  wmic path win32_VideoController get name 2>nul | findstr /I "RX 580" >nul && (
    set "RIG_PROFILE=amd_rx580_generic"
    set "GPU_BACKEND=auto"
    set "WORKER_BATCH=2097152"
    set "GPU_CHUNK=1048576"
    set "SEARCH_MS=4000"
    set "CLAIM_MS=100"
    set "TEMP_PAUSE=80"
    set "TEMP_RESUME=74"
    set "CALIB_GHS=0.2"
  )
)
(
  echo HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
  echo HACKME_ADMIN_TOKEN=!ADMIN_TOKEN!
  echo HACKME_POOL_COORDINATOR_TOKEN=!POOL_TOKEN!
  echo HACKME_REQUIRE_ADMIN_TOKEN=1
  echo HACKME_DESKTOP_MODE=1
  echo HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1
  echo HACKME_RIG_PROFILE_AUTO=1
  if not "!RIG_PROFILE!"=="" echo HACKME_RIG_PROFILE=!RIG_PROFILE!
  echo HACKME_GPU_BACKEND=!GPU_BACKEND!
  echo HACKME_WORKER_BATCH_SIZE=!WORKER_BATCH!
  echo GPU_CHUNK=!GPU_CHUNK!
  echo SEARCH_TIMEOUT_MS=!SEARCH_MS!
  echo HACKME_WORKER_CLAIM_COOLDOWN_MS=!CLAIM_MS!
  echo HACKME_GPU_TEMP_PAUSE_C=!TEMP_PAUSE!
  echo HACKME_GPU_TEMP_RESUME_C=!TEMP_RESUME!
  echo HACKME_DESKTOP_GPU_POOL=1
  if not "!CALIB_GHS!"=="" echo HACKME_CUDA_CALIBRATE_GHS=!CALIB_GHS!
) > "hackme.env"

echo.
echo OK. Installed to %INSTALL_DIR%
echo Local admin token for dashboard: !ADMIN_TOKEN!
echo.
echo Next: double-click "Start HackMe Miner.bat" on Desktop or in %INSTALL_DIR%
echo.

if not exist "%USERPROFILE%\Desktop\Start HackMe Miner.bat" (
  > "%USERPROFILE%\Desktop\Start HackMe Miner.bat" (
    echo @echo off
    echo cd /d "%INSTALL_DIR%"
    echo call start_hackme_miner.bat
  )
  echo Desktop shortcut created.
)

if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
exit /b 0
