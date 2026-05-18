@echo off
setlocal
title HackMe Desktop Mode Status

echo Checking local status...
curl -s http://127.0.0.1:8080/api/status >nul 2>&1
if errorlevel 1 (
  echo status=down
  exit /b 1
)

for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "$j=Invoke-RestMethod http://127.0.0.1:8080/api/status; 'status=up tip_height=' + $j.tip_height + ' mining=' + $j.mining + ' node=' + $j.node_address"`) do echo %%i
exit /b 0
