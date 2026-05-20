@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"
title HackMe Miner — hackme.tech

set "HACKME_DIR=%~dp0"
if "%HACKME_DIR:~-1%"=="\" set "HACKME_DIR=%HACKME_DIR:~0,-1%"

if not exist "%HACKME_DIR%\hackme.exe" (
  echo ERROR: run from the HackMe install folder ^(e.g. C:\Program Files\HackMe^).
  pause
  exit /b 1
)

if not exist "%HACKME_DIR%\pool.miner.token" (
  echo ERROR: pool.miner.token missing — reinstall HackMe from https://hackme.tech/downloads.html
  pause
  exit /b 1
)

if not exist "%HACKME_DIR%\hackme.env" (
  echo Creating hackme.env...
  call "%HACKME_DIR%\setup_hackme_miner.bat"
  cd /d "%HACKME_DIR%"
)

set "POOL_OK=0"
for /f "usebackq delims=" %%T in ("%HACKME_DIR%\pool.miner.token") do if not "%%T"=="" set "POOL_OK=1"
if "!POOL_OK!"=="1" (
  for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_POOL_COORDINATOR_TOKEN=" "%HACKME_DIR%\hackme.env" 2^>nul') do (
    if not "%%B"=="" if /I not "%%B"=="REPLACE_WITH_POOL_TOKEN" set "HAS_POOL=1"
  )
)
if not "!HAS_POOL!"=="1" (
  echo Repairing hackme.env from pool.miner.token...
  powershell -NoProfile -ExecutionPolicy Bypass -File "%HACKME_DIR%\write_hackme_env.ps1" -InstallDir "%HACKME_DIR%" -RepairOnly -NonInteractive
  if errorlevel 1 (
    echo ERROR: could not write hackme.env
    pause
    exit /b 1
  )
)

set "HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech"
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_ADMIN_TOKEN=" "%HACKME_DIR%\hackme.env" 2^>nul') do set "HACKME_ADMIN_TOKEN=%%B"
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_POOL_COORDINATOR_TOKEN=" "%HACKME_DIR%\hackme.env" 2^>nul') do set "HACKME_POOL_COORDINATOR_TOKEN=%%B"
if "!HACKME_POOL_COORDINATOR_TOKEN!"=="" (
  for /f "usebackq delims=" %%T in ("%HACKME_DIR%\pool.miner.token") do set "HACKME_POOL_COORDINATOR_TOKEN=%%T"
)
if "!HACKME_POOL_COORDINATOR_TOKEN!"=="" (
  echo ERROR: pool token missing. Run setup_hackme_miner.bat or reinstall.
  pause
  exit /b 1
)

echo.
echo HackMe public pool miner
echo Install: %HACKME_DIR%
echo Dashboard: http://127.0.0.1:8080/#mining
echo.
echo Keep this window open while mining.
echo.

start "hackme-browser" cmd /c "timeout /t 3 /nobreak >nul && start http://127.0.0.1:8080/#mining"
start "hackme-autoworker" /min cmd /c call "%HACKME_DIR%\autostart_pool_worker.bat"

cd /d "%HACKME_DIR%"
hackme.exe
set EC=%ERRORLEVEL%
if not %EC%==0 (
  echo Node exited with code %EC%.
  pause
)
exit /b %EC%
