@echo off
setlocal EnableExtensions
cd /d "%~dp0"
title HackMe — configure miner

set "HACKME_DIR=%~dp0"
if "%HACKME_DIR:~-1%"=="\" set "HACKME_DIR=%HACKME_DIR:~0,-1%"
if not "%HACKME_INSTALL_DIR%"=="" (
  set "HACKME_DIR=%HACKME_INSTALL_DIR%"
  if "%HACKME_DIR:~-1%"=="\" set "HACKME_DIR=%HACKME_DIR:~0,-1%"
)
cd /d "%HACKME_DIR%"

echo.
echo === HackMe miner setup ===
echo Folder: %HACKME_DIR%
echo.

if not exist "%HACKME_DIR%\hackme.exe" (
  echo ERROR: hackme.exe not found.
  if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
  exit /b 1
)

if not exist "%HACKME_DIR%\pool.miner.token" (
  echo ERROR: pool.miner.token missing — reinstall from https://hackme.tech/downloads.html
  if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
  exit /b 1
)

if exist "%HACKME_DIR%\detect_gpu.ps1" (
  powershell -NoProfile -ExecutionPolicy Bypass -File "%HACKME_DIR%\detect_gpu.ps1" -OutFile "%HACKME_DIR%\gpu_detect.json" >nul 2>&1
)

set "GPU_ARG=auto"
if /I "%HACKME_GPU_BACKEND_CHOICE%"=="cuda" set "GPU_ARG=cuda"
if /I "%HACKME_GPU_BACKEND_CHOICE%"=="opencl" set "GPU_ARG=opencl"
if /I "%HACKME_GPU_BACKEND_CHOICE%"=="cpu" set "GPU_ARG=cpu"

set "PS_EXTRA="
if /I "%HACKME_SETUP_NONINTERACTIVE%"=="1" set "PS_EXTRA=-NonInteractive"
if /I "%~1"=="repair" goto :do_repair
if /I "%HACKME_REPAIR_ONLY%"=="1" goto :do_repair
goto :do_setup

:do_repair
powershell -NoProfile -ExecutionPolicy Bypass -File "%HACKME_DIR%\write_hackme_env.ps1" -InstallDir "%HACKME_DIR%" -RepairOnly %PS_EXTRA%
if errorlevel 1 (
  echo ERROR: failed to repair hackme.env
  if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
  exit /b 1
)
echo.
echo Repaired hackme.env — restart start_hackme_miner.bat
if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
exit /b 0

:do_setup
powershell -NoProfile -ExecutionPolicy Bypass -File "%HACKME_DIR%\write_hackme_env.ps1" -InstallDir "%HACKME_DIR%" -GpuBackend %GPU_ARG% %PS_EXTRA%
if errorlevel 1 (
  echo ERROR: failed to write hackme.env
  if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
  exit /b 1
)

echo.
if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
exit /b 0
