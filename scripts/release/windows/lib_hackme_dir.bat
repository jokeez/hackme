@echo off
rem Sets HACKME_DIR to install folder without trailing backslash (safe for PowerShell -InstallDir "..." ).
set "HACKME_DIR=%~1"
if "%HACKME_DIR:~-1%"=="\" set "HACKME_DIR=%HACKME_DIR:~0,-1%"
