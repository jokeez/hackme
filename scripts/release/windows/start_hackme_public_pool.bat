@echo off
setlocal
cd /d "%~dp0"
title HackMe — public pool (hackme.tech)
echo.
if not exist "hackme.exe" (
  echo ERROR: hackme.exe not found in this folder.
  pause
  exit /b 1
)

rem Optional first-run template (user edits coordinator token).
if not exist ".env" if not exist "hackme.env" if exist "env.public_pool.example" (
  copy /y "env.public_pool.example" "hackme.env" >nul
  echo Created hackme.env from env.public_pool.example — add HACKME_POOL_COORDINATOR_TOKEN from the pool operator.
  echo.
)

rem Safe default: one URL pins canonical chain + inferred coordinator (see README worker-mode).
set "HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech"

echo Public authority: %HACKME_PUBLIC_AUTHORITY_BASE%
echo Dashboard: http://127.0.0.1:8080/#mining  ^(Mining tab — Start pool worker^)
echo You still need: admin token in the UI and coordinator token ^(hackme.env or paste in dashboard^).
echo.
start "hackme-browser-delay" cmd /c "timeout /t 2 /nobreak >nul && start http://127.0.0.1:8080/#mining"
hackme.exe
set EC=%ERRORLEVEL%
if not %EC%==0 (
  echo.
  echo Node exited with code %EC%.
  pause
)
exit /b %EC%
