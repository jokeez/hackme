@echo off
rem Legacy name — forwards to the keep-alive watchdog (do not use one-shot autostart).
cd /d "%~dp0"
call "%~dp0watchdog_pool_worker.bat"
