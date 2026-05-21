@echo off
setlocal EnableExtensions
cd /d "%~dp0"
if not exist logs mkdir logs
start "hackme-node" /MIN cmd /c "cd /d "%~dp0" && hackme.exe >> logs\node.log 2>&1"
timeout /t 6 /nobreak >nul
call autostart_pool_worker.bat >> logs\worker.log 2>&1
