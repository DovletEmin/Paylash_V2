# Daily backup of the Paylash Postgres database and MinIO object storage.
# Run via Task Scheduler (see BACKUP_SETUP.txt) -- writes to
# <project>\backups\YYYY-MM-DD\ and deletes any dated backup folder older
# than $RetentionDays to keep disk usage bounded.
#
# $PSScriptRoot makes this portable across machines, the same reasoning as
# autostart.bat's %~dp0 -- unlike autostart.ps1, which hardcodes one PC's
# path and was superseded by autostart.bat for exactly that reason. Keep
# this file inside <project>\scripts and it finds everything else itself.
#
# IMPORTANT: keep this file plain ASCII. Windows PowerShell 5.1 reads a
# .ps1 file with no BOM using the SYSTEM's default ANSI codepage, not
# UTF-8 -- on a machine whose codepage isn't UTF-8 (Cyrillic-locale
# Windows installs commonly default to windows-1251, for example), any
# multi-byte character (an em dash, a curly quote, box-drawing characters)
# gets silently misread as several garbage characters, which can desync
# the parser badly enough to fail with a completely unrelated-looking
# "missing string terminator" error elsewhere in the file. Non-ASCII text
# is fine in this project's Go/JS source (always read as UTF-8 there) --
# it is NOT safe in a .ps1 meant to run via Task Scheduler on an unknown
# machine's default locale.

$ErrorActionPreference = "Stop"
$RetentionDays = 14

$ProjectDir = Split-Path -Parent $PSScriptRoot
$LogFile = Join-Path $PSScriptRoot "backup.log"
$BackupRoot = Join-Path $ProjectDir "backups"

function Write-Log($msg) {
    "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')  $msg" | Out-File -FilePath $LogFile -Append -Encoding utf8
}

# Reads KEY=VALUE out of the project's .env -- used for the Postgres/MinIO
# credentials docker-compose itself already reads from there, so nothing
# needs to be duplicated or hardcoded in this script.
function Read-EnvValue($key) {
    $envFile = Join-Path $ProjectDir ".env"
    $line = Get-Content $envFile | Where-Object { $_ -match "^$key=" } | Select-Object -First 1
    if (-not $line) { return $null }
    return ($line -split '=', 2)[1].Trim()
}

Write-Log "backup triggered"
Set-Location $ProjectDir

$today = Get-Date -Format "yyyy-MM-dd"
$dest = Join-Path $BackupRoot $today
New-Item -ItemType Directory -Force -Path (Join-Path $dest "minio") | Out-Null

$pgUser = Read-EnvValue "POSTGRES_USER"
$pgDb = Read-EnvValue "POSTGRES_DB"
$minioUser = Read-EnvValue "MINIO_ROOT_USER"
$minioPass = Read-EnvValue "MINIO_ROOT_PASSWORD"

$overallOk = $true

# -- Postgres: plain-text pg_dump (not the -Fc custom binary format) so a
#    restore never needs anything beyond `psql < postgres.sql` -- no
#    pg_restore flags to remember during an actual emergency. stdout is
#    redirected via cmd.exe rather than PowerShell's own `>`/Set-Content:
#    PowerShell's pipeline can re-encode a native command's stdout (BOM
#    insertion, line-ending translation), which is exactly the kind of
#    silent corruption you must NOT discover the first time you try to
#    restore a backup. cmd.exe's `>` redirects raw bytes with no such risk.
#    stderr goes to its own temp file instead of straight into backup.log:
#    that file is written through Write-Log elsewhere (PowerShell's
#    Out-File, UTF-8), and mixing in raw cmd-redirected text side by side
#    risks the exact same kind of mixed-encoding garbling -- funnel
#    everything through one function instead.
try {
    $pgDumpPath = Join-Path $dest "postgres.sql"
    $pgErrPath = Join-Path $dest "postgres.stderr.tmp"
    $cmd = "docker compose exec -T postgres pg_dump -U $pgUser -d $pgDb > `"$pgDumpPath`" 2> `"$pgErrPath`""
    cmd /c $cmd
    $pgExit = $LASTEXITCODE
    if (Test-Path $pgErrPath) {
        Get-Content $pgErrPath | Where-Object { $_ } | ForEach-Object { Write-Log "  pg_dump: $_" }
        Remove-Item $pgErrPath -Force
    }
    if ($pgExit -ne 0) { throw "pg_dump exited with code $pgExit" }
    $size = (Get-Item $pgDumpPath).Length
    if ($size -eq 0) { throw "pg_dump produced an empty file" }
    Write-Log "postgres backup OK ($size bytes) -> $pgDumpPath"
} catch {
    Write-Log "postgres backup FAILED: $_"
    $overallOk = $false
}

# -- MinIO: mirror every bucket to the host via a throwaway mc container on
#    the same compose network (paylash_default), addressing MinIO by its
#    internal service name -- works regardless of what's published to the
#    host, and never touches the S3 API over the network the browser uses.
try {
    if (-not $minioUser -or -not $minioPass) { throw "MINIO_ROOT_USER/MINIO_ROOT_PASSWORD not found in .env" }
    $minioDest = (Join-Path $dest "minio") -replace '\\', '/'
    # Captured as PowerShell objects (2>&1, not raw file redirection) so
    # every line goes through Write-Log's own consistent UTF-8 encoding
    # instead of docker's native-command output potentially landing in a
    # different one side by side in the same file.
    $mcOutput = & docker run --rm --network paylash_default `
        -v "${minioDest}:/backup" `
        -e "MC_HOST_src=http://${minioUser}:${minioPass}@minio:9000" `
        minio/mc mirror --overwrite --quiet src /backup 2>&1
    $mcExit = $LASTEXITCODE
    if ($mcExit -ne 0) {
        # Full per-file detail only on failure -- on every normal successful
        # day this would otherwise add one line per object in the whole
        # instance to the log, forever.
        $mcOutput | Where-Object { $_ } | ForEach-Object { Write-Log "  mc: $_" }
        throw "mc mirror exited with code $mcExit"
    }
    $fileCount = ($mcOutput | Where-Object { $_ -match '^`.*`\s*->\s*`.*`$' }).Count
    Write-Log "minio backup OK ($fileCount files) -> $dest\minio"
} catch {
    Write-Log "minio backup FAILED: $_"
    $overallOk = $false
}

# -- Rotation: remove dated backup folders older than $RetentionDays. Parses
#    the folder NAME as the authoritative date (not filesystem timestamps,
#    which a copy/move could reset) and skips anything that isn't named
#    like a backup folder at all, so this can never delete something
#    unrelated that was placed under backups\ by hand.
try {
    $cutoff = (Get-Date).AddDays(-$RetentionDays)
    Get-ChildItem -Path $BackupRoot -Directory -ErrorAction SilentlyContinue | ForEach-Object {
        $parsed = [DateTime]::MinValue
        $isDated = [DateTime]::TryParseExact(
            $_.Name, "yyyy-MM-dd", $null,
            [System.Globalization.DateTimeStyles]::None, [ref]$parsed)
        if ($isDated -and $parsed -lt $cutoff) {
            Write-Log "removing old backup: $($_.Name)"
            Remove-Item -Recurse -Force $_.FullName
        }
    }
} catch {
    Write-Log "rotation FAILED: $_"
}

Write-Log "backup finished, overall ok = $overallOk"
if (-not $overallOk) { exit 1 }
