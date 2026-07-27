@echo off
REM Runs at Windows logon (via Task Scheduler) to bring the Paylash stack up
REM the same way `docker compose up -d` would from a terminal: it waits for
REM Postgres/MinIO health checks before starting the app, and for the app's
REM health check before starting Caddy. This avoids the race that happens
REM when Docker's own engine restarts containers directly after a reboot,
REM ignoring depends_on/health-check ordering.
REM
REM Uses %~dp0 to find its own folder, so it works no matter where the
REM project lives on a given PC (just keep this file in <project>\scripts).

setlocal enabledelayedexpansion

set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..") do set "PROJECT_DIR=%%~fI"
set "LOGFILE=%SCRIPT_DIR%autostart.log"

echo %DATE% %TIME%  autostart triggered >> "%LOGFILE%"

REM Wait for the Docker engine to be reachable (Docker Desktop can take a
REM minute or more to finish starting after login), up to 5 minutes (60 x 5s).
set /a ATTEMPTS=0
:waitloop
docker info >nul 2>&1
if %ERRORLEVEL% EQU 0 goto ready
set /a ATTEMPTS+=1
if %ATTEMPTS% GEQ 60 goto notready
timeout /t 5 /nobreak >nul
goto waitloop

:notready
echo %DATE% %TIME%  Docker engine never became ready, giving up >> "%LOGFILE%"
exit /b 1

:ready
echo %DATE% %TIME%  Docker engine ready, running docker compose up -d >> "%LOGFILE%"
cd /d "%PROJECT_DIR%"
docker compose up -d >> "%LOGFILE%" 2>&1
echo %DATE% %TIME%  docker compose up -d exited with code %ERRORLEVEL% >> "%LOGFILE%"
