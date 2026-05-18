@echo off
setlocal
cd /d "%~dp0"
title HackMe — local node
echo.
echo HackMe: keep this window open while the node runs.
echo Dashboard: http://127.0.0.1:8080  ^(same machine^)
echo Mining: use Dashboard - Mining - Start Worker ^(pool coordinator URL^); see README.
echo LAN access: set HACKME_BIND_ADDR=0.0.0.0:8080 and allow port in firewall.
echo.
if not exist "hackme.exe" (
  echo ERROR: hackme.exe not found in this folder.
  pause
  exit /b 1
)
rem Open browser shortly after the HTTP server binds (best-effort).
start "hackme-browser-delay" cmd /c "timeout /t 2 /nobreak >nul && start http://127.0.0.1:8080/#mining"
hackme.exe
set EC=%ERRORLEVEL%
if not %EC%==0 (
  echo.
  echo Node exited with code %EC%.
  pause
)
exit /b %EC%
