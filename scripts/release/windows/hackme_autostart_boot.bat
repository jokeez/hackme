@echo off
rem Optional Windows autostart: local node (public pool follower). Run desktop_mode once to create .env.desktop.windows.
cd /d "%~dp0"
if not exist "hackme.exe" exit /b 0

if exist ".env.desktop.windows" (
  for /f "usebackq tokens=1,* delims==" %%A in (".env.desktop.windows") do (
    if not "%%A"=="" if not "%%~A"=="#" set "%%A=%%B"
  )
) else (
  set "HACKME_BIND_ADDR=127.0.0.1:8080"
  set "HACKME_DESKTOP_MODE=1"
  set "HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech"
  set "HACKME_CANONICAL_CHAIN_URL=https://hackme.tech"
)

if "%HACKME_ADMIN_TOKEN%"=="" (
  rem No token — skip autostart (run start_hackme_desktop_mode.bat once).
  exit /b 0
)

set "HACKME_BIND_ADDR=127.0.0.1:8080"
start /min "" hackme.exe
