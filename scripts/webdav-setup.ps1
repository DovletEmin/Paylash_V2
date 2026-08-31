# One-time preparation of a Windows machine for mounting Paylash as a
# network drive. Run once per computer, as Administrator.
#
# Windows will not mount this share out of the box, for three separate
# reasons, and each one fails with an unhelpful message:
#
#   1. Caddy issues the site's certificate from its own internal authority.
#      A browser can be told to accept that once; the WebDAV redirector
#      cannot -- it refuses an untrusted certificate outright and reports
#      only "the folder is invalid".
#   2. The redirector caps a single file at 50 MB by default. A drawing or a
#      render is routinely larger, and the copy fails partway with "error
#      0x800700DF: the file size exceeds the limit allowed".
#   3. The WebClient service, which provides the redirector, is set to start
#      manually and is often not running at all.
#
# IMPORTANT: keep this file plain ASCII. Windows PowerShell 5.1 reads a .ps1
# with no BOM using the system's ANSI codepage, so on a Cyrillic-locale
# machine any non-ASCII character here would be misread and can desync the
# parser into an unrelated-looking syntax error. The same reasoning is
# spelled out at the top of backup.ps1.

$ErrorActionPreference = "Stop"

# 4 GB, the largest value the redirector accepts.
$FileSizeLimit = 4294967295
$CaddyContainer = "paylash-caddy"

function Assert-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($id)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Host "This script must be run as Administrator." -ForegroundColor Red
        Write-Host "Right-click PowerShell and choose 'Run as administrator', then run it again."
        exit 1
    }
}

function Step($n, $text) { Write-Host ""; Write-Host "[$n] $text" -ForegroundColor Cyan }

Assert-Admin
Write-Host "Preparing this computer for the Paylash network drive." -ForegroundColor Green

# --- 1. Trust the server's certificate authority -------------------------
Step 1 "Trusting the Paylash certificate authority"
$rootPath = Join-Path $env:TEMP "paylash-root.crt"
$copied = $false

# Preferred source: the server itself, if Docker is available on this
# machine (i.e. this IS the server). On an employee's PC it will not be, so
# the fallback below asks for the file that was exported from the server.
if (Get-Command docker -ErrorAction SilentlyContinue) {
    $inContainer = "/data/caddy/pki/authorities/local/root.crt"
    docker cp "${CaddyContainer}:${inContainer}" $rootPath 2>$null
    if ($LASTEXITCODE -eq 0 -and (Test-Path $rootPath)) { $copied = $true }
}

if (-not $copied) {
    $local = Join-Path $PSScriptRoot "paylash-root.crt"
    if (Test-Path $local) {
        Copy-Item $local $rootPath -Force
        $copied = $true
    }
}

if ($copied) {
    Import-Certificate -FilePath $rootPath -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
    Remove-Item $rootPath -Force -ErrorAction SilentlyContinue
    Write-Host "    certificate authority trusted" -ForegroundColor Green
} else {
    Write-Host "    SKIPPED - root.crt not found" -ForegroundColor Yellow
    Write-Host "    On the server run:"
    Write-Host "      docker cp ${CaddyContainer}:/data/caddy/pki/authorities/local/root.crt paylash-root.crt"
    Write-Host "    then copy that file next to this script and run it again."
}

# --- 2. Raise the single-file size limit ---------------------------------
Step 2 "Raising the WebDAV file size limit to 4 GB"
$key = "HKLM:\SYSTEM\CurrentControlSet\Services\WebClient\Parameters"
if (-not (Test-Path $key)) { New-Item -Path $key -Force | Out-Null }
Set-ItemProperty -Path $key -Name "FileSizeLimitInBytes" -Value $FileSizeLimit -Type DWord
# The redirector also gives up on a slow directory listing after 30s by
# default, which a folder of several hundred drawings can exceed.
Set-ItemProperty -Path $key -Name "FileAttributesLimitInBytes" -Value 10000000 -Type DWord
Write-Host "    limit set" -ForegroundColor Green

# --- 3. Make the WebClient service start by itself ------------------------
Step 3 "Enabling the WebClient service"
Set-Service -Name WebClient -StartupType Automatic
$svc = Get-Service -Name WebClient
if ($svc.Status -ne "Running") { Start-Service -Name WebClient }
# The size limit is read when the service starts, so a service that was
# already running is restarted to pick up step 2.
else { Restart-Service -Name WebClient -Force }
Write-Host "    WebClient is running and set to start automatically" -ForegroundColor Green

Write-Host ""
Write-Host "Done. This computer can now map the drive." -ForegroundColor Green
Write-Host ""
Write-Host "In Explorer: This PC -> Map network drive -> use the address from" -ForegroundColor White
Write-Host "your profile in Paylash (Profile -> Network drive), for example:" -ForegroundColor White
Write-Host "    https://paylash.local/dav" -ForegroundColor Yellow
Write-Host ""
Write-Host "Sign in with your Paylash username and the DEVICE KEY generated" -ForegroundColor White
Write-Host "in the app - not your normal password." -ForegroundColor White
