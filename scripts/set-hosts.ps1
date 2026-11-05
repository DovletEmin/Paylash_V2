# Points paylash.local at the Paylash server in this computer's hosts file.
# Run as Administrator, on every computer that opens Paylash or the network
# drive, whenever the server's IP changes.
#
#   .\set-hosts.ps1                   uses the default address below
#   .\set-hosts.ps1 -Ip 192.168.2.70  uses another one
#
# Any existing paylash.local line is replaced, so running it twice is safe.
# The old hosts file is kept next to it as hosts.paylash.bak.
#
# IMPORTANT: keep this file plain ASCII. Windows PowerShell 5.1 reads a .ps1
# with no BOM using the system's ANSI codepage, so on a Cyrillic-locale
# machine any non-ASCII character here would be misread. The same reasoning
# is spelled out at the top of backup.ps1.

param(
    [string]$Ip = "192.168.2.67",
    [string]$HostName = "paylash.local"
)

$ErrorActionPreference = "Stop"

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($id)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "This script must be run as Administrator." -ForegroundColor Red
    Write-Host "Right-click PowerShell and choose 'Run as administrator', then run it again."
    exit 1
}

if ($Ip -notmatch '^\d{1,3}(\.\d{1,3}){3}$') {
    Write-Host "'$Ip' is not an IPv4 address." -ForegroundColor Red
    exit 1
}

$hosts = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"
Copy-Item $hosts "$hosts.paylash.bak" -Force

# Keep every line except an active (uncommented) one that names our host.
$escaped = [regex]::Escape($HostName)
$kept = @(Get-Content $hosts | Where-Object { $_ -notmatch "^\s*[^#\s]\S*\s+.*(?<![\w.-])$escaped(?![\w.-])" })
$kept += "$Ip $HostName"

# ASCII: the hosts file is read by the OS, and a BOM at the top of it makes
# the first line unreadable.
Set-Content -Path $hosts -Value $kept -Encoding ASCII

ipconfig /flushdns | Out-Null
Write-Host "$HostName -> $Ip" -ForegroundColor Green

# Say so if the server is not actually answering there, rather than leaving
# the person to find out from a browser error.
if (Test-Connection -ComputerName $Ip -Count 1 -Quiet) {
    Write-Host "The server answers at $Ip." -ForegroundColor Green
} else {
    Write-Host "Nothing answers at $Ip. Check the address and that the server is on." -ForegroundColor Yellow
}
