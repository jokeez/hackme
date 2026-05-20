@echo off
rem Legacy name — forwards to one-click miner launcher.
cd /d "%~dp0"
if exist "start_hackme_miner.bat" (
  call "start_hackme_miner.bat"
  exit /b %ERRORLEVEL%
)
echo ERROR: start_hackme_miner.bat missing. Re-extract the release zip.
pause
exit /b 1
