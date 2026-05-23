@echo off
setlocal EnableExtensions
cd /d "%~dp0"
title HackMe Install

set "SETUP="
for %%F in ("%~dp0HackMe-Setup-*.exe") do set "SETUP=%%~fF"
if not defined SETUP (
  echo HackMe-Setup-*.exe not found in this folder.
  echo Download from https://hackme.tech/downloads.html
  pause
  exit /b 1
)

echo Starting HackMe installer...
echo If nothing appears: right-click the .exe -^> Run as administrator
echo Or use portable ZIP: hackme_*_windows_setup.zip
echo.

powershell -NoProfile -ExecutionPolicy Bypass -Command "try { Unblock-File -LiteralPath '%SETUP%' -ErrorAction SilentlyContinue } catch {}" 2>nul
start "" "%SETUP%"
timeout /t 3 /nobreak >nul
echo If the setup window did not open, press any key for portable ZIP instructions.
pause >nul
exit /b 0
