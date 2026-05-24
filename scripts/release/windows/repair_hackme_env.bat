@echo off
rem Re-apply fair Windows pool env (NVIDIA cooldown 0, no forced 28s sleep).
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0write_hackme_env.ps1" -InstallDir "%~dp0" -RepairOnly -NonInteractive
echo.
echo Done. Restart: start_hackme_miner.bat
pause
