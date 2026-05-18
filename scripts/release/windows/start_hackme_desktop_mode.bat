@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"
title HackMe Desktop Mode

set "APP_DIR=%~dp0"
set "ENV_FILE=%APP_DIR%.env.desktop.windows"
set "PID_FILE=%APP_DIR%logs\desktop\node.pid"
set "LOG_FILE=%APP_DIR%logs\desktop\node.log"

if not exist "%APP_DIR%logs\desktop" mkdir "%APP_DIR%logs\desktop" >nul 2>&1

if /I not "%SKIP_TOOLCHAINS%"=="1" (
  if exist "%APP_DIR%install_from_code_toolchains.bat" (
    call "%APP_DIR%install_from_code_toolchains.bat" >nul 2>&1
  ) else if exist "%APP_DIR%install_from_code_toolchains.ps1" (
    powershell -NoProfile -ExecutionPolicy Bypass -File "%APP_DIR%install_from_code_toolchains.ps1" -AppDir "%APP_DIR%" >nul 2>&1
  )
)
if exist "%APP_DIR%.hackme-toolchains.env" (
  for /f "usebackq tokens=1,* delims==" %%A in ("%APP_DIR%.hackme-toolchains.env") do (
    if /I "%%A"=="PATH" set "PATH=%%B;%PATH%"
  )
)

if not exist "%APP_DIR%hackme.exe" (
  echo ERROR: hackme.exe not found in this folder.
  pause
  exit /b 1
)

if not exist "%ENV_FILE%" (
  for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "$b=New-Object byte[] 24; [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b); ($b|ForEach-Object ToString x2) -join ''"`) do set "GEN_TOKEN=%%i"
  > "%ENV_FILE%" (
    echo HACKME_BIND_ADDR=127.0.0.1:8080
    echo HACKME_ADMIN_TOKEN=!GEN_TOKEN!
    echo HACKME_REQUIRE_ADMIN_TOKEN=1
    echo HACKME_DESKTOP_MODE=1
    echo DESKTOP_PROFILE=worker
    echo HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
    echo HACKME_CANONICAL_CHAIN_URL=https://hackme.tech
  )
  echo Created %ENV_FILE% with generated admin token.
)

for /f "tokens=1,* delims==" %%A in (%ENV_FILE%) do (
  set "%%A=%%B"
)

echo Starting HackMe Desktop Mode...
rem Browser after bind (same folder as node — see HACKME_WORKING_DIR / exe chdir in hackme.exe)
start "hackme-browser-delay" cmd /c "timeout /t 2 /nobreak >nul && start http://127.0.0.1:8080/#mining"

rem Inherit env in child; cwd is already APP_DIR so hackme.exe resolves without nested quotes
start "hackme-node" /b cmd /c "hackme.exe >> \"%LOG_FILE%\" 2>&1"
echo %RANDOM% > "%PID_FILE%"

echo Dashboard: http://127.0.0.1:8080
echo Log: %LOG_FILE%
echo.
echo Keep this window open for startup checks, then you can close it.
timeout /t 2 /nobreak >nul
curl -s http://127.0.0.1:8080/api/status >nul 2>&1
if errorlevel 1 (
  echo WARN: Node is still starting. Check log if needed.
) else (
  echo Node is up.
)

exit /b 0
