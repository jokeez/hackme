@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"
title HackMe Miner - hackme.tech

set "HACKME_DIR=%~dp0"
if "%HACKME_DIR:~-1%"=="\" set "HACKME_DIR=%HACKME_DIR:~0,-1%"

if not exist "%HACKME_DIR%\hackme.exe" (
  echo ERROR: run from the HackMe install folder ^(e.g. %%LOCALAPPDATA%%\HackMe^).
  pause
  exit /b 1
)

if not exist "%HACKME_DIR%\pool.miner.token" (
  echo ERROR: pool.miner.token missing - reinstall HackMe from https://hackme.tech/downloads.html
  pause
  exit /b 1
)

if not exist "%HACKME_DIR%\logs" mkdir "%HACKME_DIR%\logs" >nul 2>&1

if not exist "%HACKME_DIR%\hackme.env" (
  echo Creating hackme.env...
  rem Non-interactive: avoid mid-start "pause" from setup_hackme_miner.bat
  set "HACKME_SETUP_NONINTERACTIVE=1"
  call "%HACKME_DIR%\setup_hackme_miner.bat"
  set "EC_SETUP=!ERRORLEVEL!"
  set "HACKME_SETUP_NONINTERACTIVE="
  cd /d "%HACKME_DIR%"
  if not "!EC_SETUP!"=="0" (
    echo ERROR: setup_hackme_miner.bat failed ^(code !EC_SETUP!^).
    pause
    exit /b !EC_SETUP!
  )
)

set "POOL_OK=0"
for /f "usebackq delims=" %%T in ("%HACKME_DIR%\pool.miner.token") do if not "%%T"=="" set "POOL_OK=1"
set "HAS_POOL="
if "!POOL_OK!"=="1" (
  for /f "usebackq tokens=1,* delims==" %%A in ("%HACKME_DIR%\hackme.env") do (
    if /I "%%~A"=="HACKME_POOL_COORDINATOR_TOKEN" if not "%%~B"=="" if /I not "%%~B"=="REPLACE_WITH_POOL_TOKEN" set "HAS_POOL=1"
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
set "HACKME_ADMIN_TOKEN="
set "HACKME_POOL_COORDINATOR_TOKEN="
for /f "usebackq tokens=1,* delims==" %%A in ("%HACKME_DIR%\hackme.env") do (
  if /I "%%~A"=="HACKME_ADMIN_TOKEN" set "HACKME_ADMIN_TOKEN=%%~B"
  if /I "%%~A"=="HACKME_POOL_COORDINATOR_TOKEN" set "HACKME_POOL_COORDINATOR_TOKEN=%%~B"
)
set "HACKME_ADMIN_TOKEN=!HACKME_ADMIN_TOKEN:"=!"
set "HACKME_POOL_COORDINATOR_TOKEN=!HACKME_POOL_COORDINATOR_TOKEN:"=!"

if "!HACKME_ADMIN_TOKEN!"=="" (
  echo Admin token missing - regenerating hackme.env...
  powershell -NoProfile -ExecutionPolicy Bypass -File "%HACKME_DIR%\write_hackme_env.ps1" -InstallDir "%HACKME_DIR%" -RepairOnly -NonInteractive
  if errorlevel 1 (
    echo ERROR: could not create admin token in hackme.env
    pause
    exit /b 1
  )
  for /f "usebackq tokens=1,* delims==" %%A in ("%HACKME_DIR%\hackme.env") do (
    if /I "%%~A"=="HACKME_ADMIN_TOKEN" set "HACKME_ADMIN_TOKEN=%%~B"
    if /I "%%~A"=="HACKME_POOL_COORDINATOR_TOKEN" set "HACKME_POOL_COORDINATOR_TOKEN=%%~B"
  )
  set "HACKME_ADMIN_TOKEN=!HACKME_ADMIN_TOKEN:"=!"
  set "HACKME_POOL_COORDINATOR_TOKEN=!HACKME_POOL_COORDINATOR_TOKEN:"=!"
)

if "!HACKME_POOL_COORDINATOR_TOKEN!"=="" (
  for /f "usebackq delims=" %%T in ("%HACKME_DIR%\pool.miner.token") do set "HACKME_POOL_COORDINATOR_TOKEN=%%T"
)
if "!HACKME_ADMIN_TOKEN!"=="" (
  echo ERROR: HACKME_ADMIN_TOKEN still empty after repair. Delete hackme.env and re-run setup_hackme_miner.bat
  pause
  exit /b 1
)
if "!HACKME_POOL_COORDINATOR_TOKEN!"=="" (
  echo ERROR: pool token missing. Run setup_hackme_miner.bat or reinstall.
  pause
  exit /b 1
)

powershell -NoProfile -ExecutionPolicy Bypass -File "%HACKME_DIR%\set_windows_power_perf.ps1" >nul 2>&1

echo.
echo HackMe public pool miner
echo Install: %HACKME_DIR%
echo Dashboard: http://127.0.0.1:8080/#ecosystem
echo Watchdog log: %HACKME_DIR%\logs\watchdog_worker.log
echo.
echo Keep this window open while mining.
echo.

start "hackme-browser" cmd /c "timeout /t 3 /nobreak >nul && start http://127.0.0.1:8080/#ecosystem"

rem Hidden watchdog (no blank "hackme-watchdog" console). Falls back to minimized+log redirect.
rem Use start /D for Program Files / LocalAppData paths with spaces (\" is not valid cmd escaping).
if exist "%HACKME_DIR%\run_watchdog_hidden.vbs" (
  wscript //B //Nologo "%HACKME_DIR%\run_watchdog_hidden.vbs"
) else (
  start "hackme-watchdog" /min /D "%HACKME_DIR%" cmd /c "call watchdog_pool_worker.bat >> logs\watchdog_worker.log 2>&1"
)

cd /d "%HACKME_DIR%"
hackme.exe
set EC=%ERRORLEVEL%
if not %EC%==0 (
  echo Node exited with code %EC%.
  echo If you saw admin-token errors, delete hackme.env and run setup_hackme_miner.bat
  pause
)
exit /b %EC%
