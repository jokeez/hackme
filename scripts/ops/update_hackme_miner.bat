@echo off
REM W1 wrapper — run update_hackme_miner.ps1 from install dir or zip.
setlocal
set "DIR=%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%DIR%update_hackme_miner.ps1" %*
exit /b %ERRORLEVEL%
