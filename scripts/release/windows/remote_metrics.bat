@echo off
setlocal EnableExtensions
cd /d "%~dp0"
set "ADMIN="
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_ADMIN_TOKEN=" hackme.env 2^>nul') do set "ADMIN=%%B"
if "%ADMIN%"=="" (echo ERR:no_admin & exit /b 1)
echo === processes ===
tasklist /FI "IMAGENAME eq hackme.exe" 2>nul | findstr hackme
tasklist /FI "IMAGENAME eq workerpoh.exe" 2>nul | findstr workerpoh
echo === status ===
curl -fsS -m 8 -H "X-Hackme-Admin-Token: %ADMIN%" http://127.0.0.1:8080/api/status
echo.
echo === worker metrics ===
curl -fsS -m 8 -H "X-Hackme-Admin-Token: %ADMIN%" http://127.0.0.1:8080/api/worker/metrics
echo.
echo === worker status ===
curl -fsS -m 8 -H "X-Hackme-Admin-Token: %ADMIN%" http://127.0.0.1:8080/api/worker/status
echo.
