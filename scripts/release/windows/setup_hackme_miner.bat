@echo off
setlocal EnableExtensions
cd /d "%~dp0"
title HackMe — configure miner

set "INSTALL_DIR=%~dp0"
if not "%HACKME_INSTALL_DIR%"=="" set "INSTALL_DIR=%HACKME_INSTALL_DIR%"
if "%INSTALL_DIR:~-1%"=="\" set "INSTALL_DIR=%INSTALL_DIR:~0,-1%"

echo.
echo === HackMe miner setup ===
echo Folder: %INSTALL_DIR%
echo.

if not exist "%INSTALL_DIR%\hackme.exe" (
  echo ERROR: hackme.exe not found.
  if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
  exit /b 1
)

if not exist "%INSTALL_DIR%\pool.miner.token" (
  echo ERROR: pool.miner.token missing — reinstall from https://hackme.tech/downloads.html
  if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
  exit /b 1
)

rem GPU detect JSON for rig profile
if exist "%INSTALL_DIR%\detect_gpu.ps1" (
  powershell -NoProfile -ExecutionPolicy Bypass -File "%INSTALL_DIR%\detect_gpu.ps1" -OutFile "%INSTALL_DIR%\gpu_detect.json" >nul 2>&1
)

set "GPU_ARG=auto"
if /I "%HACKME_GPU_BACKEND_CHOICE%"=="cuda" set "GPU_ARG=cuda"
if /I "%HACKME_GPU_BACKEND_CHOICE%"=="opencl" set "GPU_ARG=opencl"
if /I "%HACKME_GPU_BACKEND_CHOICE%"=="cpu" set "GPU_ARG=cpu"

set "PS_EXTRA="
if /I "%HACKME_SETUP_NONINTERACTIVE%"=="1" set "PS_EXTRA=-NonInteractive"

powershell -NoProfile -ExecutionPolicy Bypass -File "%INSTALL_DIR%\write_hackme_env.ps1" -InstallDir "%INSTALL_DIR%" -GpuBackend %GPU_ARG% %PS_EXTRA%
if errorlevel 1 (
  echo ERROR: failed to write hackme.env
  if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
  exit /b 1
)

echo.
if /I not "%HACKME_SETUP_NONINTERACTIVE%"=="1" pause
exit /b 0
