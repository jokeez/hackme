@echo off
setlocal
title Stop HackMe Desktop Mode
echo Stopping hackme.exe ...
taskkill /F /IM hackme.exe >nul 2>&1
if errorlevel 1 (
  echo No running hackme.exe found.
) else (
  echo HackMe stopped.
)
exit /b 0
