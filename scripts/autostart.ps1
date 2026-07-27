# Runs at Windows logon (via Task Scheduler) to bring the Paylash stack up
# the same way `docker compose up -d` would from a terminal: it waits for
# Postgres/MinIO health checks before starting the app, and for the app's
# health check before starting Caddy. This avoids the race that happens when
# Docker's own engine restarts containers directly after a reboot, ignoring
# depends_on/health-check ordering.

$ErrorActionPreference = "Stop"
$projectDir = "C:\Users\Emin\Documents\Paylash"
$logFile = Join-Path $projectDir "scripts\autostart.log"

function Write-Log($msg) {
    "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')  $msg" | Out-File -FilePath $logFile -Append -Encoding utf8
}

Write-Log "autostart triggered"

# Wait for the Docker engine to be reachable (Docker Desktop can take a
# minute or more to finish starting after login), up to 5 minutes.
$deadline = (Get-Date).AddMinutes(5)
$ready = $false
while ((Get-Date) -lt $deadline) {
    docker info *> $null
    if ($LASTEXITCODE -eq 0) {
        $ready = $true
        break
    }
    Start-Sleep -Seconds 5
}

if (-not $ready) {
    Write-Log "Docker engine never became ready, giving up"
    exit 1
}

Write-Log "Docker engine ready, running docker compose up -d"
Set-Location $projectDir
docker compose up -d *>> $logFile
Write-Log "docker compose up -d exited with code $LASTEXITCODE"
