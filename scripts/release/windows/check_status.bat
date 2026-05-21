@echo off
setlocal EnableExtensions
cd /d "%~dp0"
set "ADMIN="
set "POOL="
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_ADMIN_TOKEN=" hackme.env 2^>nul') do set "ADMIN=%%B"
for /f "tokens=1,* delims==" %%A in ('findstr /B /I "HACKME_POOL_COORDINATOR_TOKEN=" hackme.env 2^>nul') do set "POOL=%%B"
echo ADMIN_SET=%ADMIN:~0,4%...
echo POOL_SET=%POOL:~0,4%... len=%POOL:~0,1%%POOL:~1,1%
tasklist /FI "IMAGENAME eq hackme.exe" 2>nul | findstr hackme
tasklist /FI "IMAGENAME eq workerpoh.exe" 2>nul | findstr workerpoh
if "%ADMIN%"=="" exit /b 1
curl -fsS -m 5 -H "X-Hackme-Admin-Token: %ADMIN%" http://127.0.0.1:8080/api/worker/metrics 2>nul
echo.
curl -fsS -m 5 -H "X-Hackme-Admin-Token: %ADMIN%" http://127.0.0.1:8080/api/status 2>nul
