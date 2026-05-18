@echo off
setlocal EnableExtensions
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install_from_code_toolchains.ps1" -AppDir "%~dp0"
exit /b %ERRORLEVEL%
