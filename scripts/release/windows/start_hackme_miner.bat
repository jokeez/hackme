@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"
title HackMe Miner — hackme.tech

if not exist "hackme.exe" (
  echo Running one-time setup...
  call "%~dp0setup_hackme_miner.bat"
  cd /d "%~dp0"
)

if not exist "hackme.env" (
  call "%~dp0setup_hackme_miner.bat"
  cd /d "%~dp0"
)

set "HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech"

for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_ADMIN_TOKEN=" hackme.env 2^>nul') do set "HACKME_ADMIN_TOKEN=%%B"
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_POOL_COORDINATOR_TOKEN=" hackme.env 2^>nul') do set "HACKME_POOL_COORDINATOR_TOKEN=%%B"

if "!HACKME_POOL_COORDINATOR_TOKEN!"=="" (
  echo ERROR: HACKME_POOL_COORDINATOR_TOKEN missing in hackme.env
  echo Re-run setup_hackme_miner.bat or ask pool operator for pool.miner.token
  pause
  exit /b 1
)
if /I "!HACKME_POOL_COORDINATOR_TOKEN!"=="REPLACE_WITH_POOL_TOKEN" (
  echo ERROR: pool token placeholder — download a fresh release zip from hackme.tech
  pause
  exit /b 1
)

echo.
echo HackMe public pool miner
echo Dashboard: http://127.0.0.1:8080/#mining
echo Admin token is in hackme.env ^(paste same value in dashboard header if asked^).
echo Pool token: preconfigured for claim/submit.
echo.
echo After browser opens: Mining -^> Start pool worker ^(or wait for autostart^).
echo Keep this window open.
echo.

start "hackme-browser" cmd /c "timeout /t 3 /nobreak >nul && start http://127.0.0.1:8080/#mining"

start "hackme-autoworker" /min cmd /c call "%~dp0autostart_pool_worker.bat"

hackme.exe
set EC=%ERRORLEVEL%
if not %EC%==0 (
  echo Node exited with code %EC%.
  pause
)
exit /b %EC%
