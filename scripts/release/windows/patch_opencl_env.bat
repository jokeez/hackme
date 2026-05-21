@echo off
setlocal EnableExtensions
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$p='hackme.env'; $l=Get-Content -LiteralPath $p; $o=@(); foreach($line in $l){ if($line -match '^HACKME_GPU_BACKEND='){$o+='HACKME_GPU_BACKEND=opencl'} elseif($line -match '^HACKME_CUDA_CALIBRATE_GHS='){$o+='HACKME_CUDA_CALIBRATE_GHS=3.5'} else{$o+=$line}}; Set-Content -LiteralPath $p -Value $o -Encoding Ascii"
echo patched hackme.env for OpenCL
findstr /B HACKME_GPU_BACKEND HACKME_CUDA_CALIBRATE hackme.env
