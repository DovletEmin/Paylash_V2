# Restores a Paylash backup produced by backup.ps1. DESTRUCTIVE: overwrites
# the live Postgres database and MinIO buckets with the backup's contents --
# meant for actual disaster recovery, not routine use.
#
# Usage:  .\restore.ps1 -Date 2026-07-28
#         (folder names under backups\ are exactly what backup.ps1 names
#         them -- see scripts\backup.log or just list the backups\ folder)
#
# Keep this file plain ASCII -- see the note at the top of backup.ps1 for
# why (Windows PowerShell 5.1 misreads a non-BOM .ps1 using the system's
# default ANSI codepage, not UTF-8, which can silently corrupt parsing on
# a non-UTF-8-locale machine).

param(
    [Parameter(Mandatory = $true)]
    [string]$Date
)

$ErrorActionPreference = "Stop"
$ProjectDir = Split-Path -Parent $PSScriptRoot
$BackupRoot = Join-Path $ProjectDir "backups"
$Source = Join-Path $BackupRoot $Date

if (-not (Test-Path $Source)) {
    Write-Error "No backup found at $Source"
    exit 1
}

function Read-EnvValue($key) {
    $envFile = Join-Path $ProjectDir ".env"
    $line = Get-Content $envFile | Where-Object { $_ -match "^$key=" } | Select-Object -First 1
    if (-not $line) { return $null }
    return ($line -split '=', 2)[1].Trim()
}

$pgUser = Read-EnvValue "POSTGRES_USER"
$pgDb = Read-EnvValue "POSTGRES_DB"
$minioUser = Read-EnvValue "MINIO_ROOT_USER"
$minioPass = Read-EnvValue "MINIO_ROOT_PASSWORD"

Write-Host "This will OVERWRITE the live '$pgDb' database and every MinIO bucket with the contents of:" -ForegroundColor Yellow
Write-Host "  $Source" -ForegroundColor Yellow
$confirm = Read-Host "Type YES (all caps) to continue"
if ($confirm -ne "YES") {
    Write-Host "Aborted, nothing was changed."
    exit 1
}

Set-Location $ProjectDir

# -- Postgres: drop and recreate the public schema first -- the plain-SQL
#    dump assumes a clean schema to CREATE TABLE/COPY into, not an already-
#    populated database whose existing rows would just conflict.
$sqlFile = Join-Path $Source "postgres.sql"
if (Test-Path $sqlFile) {
    Write-Host "Restoring Postgres from $sqlFile ..."
    docker compose exec -T postgres psql -U $pgUser -d $pgDb -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
    Get-Content $sqlFile -Raw | docker compose exec -T postgres psql -U $pgUser -d $pgDb
    if ($LASTEXITCODE -ne 0) { throw "psql restore exited with code $LASTEXITCODE" }
    Write-Host "Postgres restored." -ForegroundColor Green
} else {
    Write-Warning "No postgres.sql in $Source -- skipping database restore."
}

# -- MinIO: mirror the backup back onto the live server. Same throwaway mc
#    container / internal network as backup.ps1, source and destination
#    swapped -- mc auto-creates any bucket that doesn't already exist.
$minioSrc = (Join-Path $Source "minio") -replace '\\', '/'
if (Test-Path $minioSrc) {
    Write-Host "Restoring MinIO from $minioSrc ..."
    docker run --rm --network paylash_default `
        -v "${minioSrc}:/backup" `
        -e "MC_HOST_dst=http://${minioUser}:${minioPass}@minio:9000" `
        minio/mc mirror --overwrite /backup dst
    if ($LASTEXITCODE -ne 0) { throw "mc mirror exited with code $LASTEXITCODE" }
    Write-Host "MinIO restored." -ForegroundColor Green
} else {
    Write-Warning "No minio\ folder in $Source -- skipping object storage restore."
}

Write-Host ""
Write-Host "Restore complete. Restart the app so nothing is serving stale in-memory state:" -ForegroundColor Cyan
Write-Host "  docker compose restart app"
