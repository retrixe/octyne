#!/usr/bin/env pwsh

# Exit on any error
$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

$CONFIG_FILE = "./ecthelion/config.json"
$BACKUP_FILE = "./ecthelion/config.backup.json"

# Backup config file if it exists
if (Test-Path $CONFIG_FILE) {
    Write-Host "Backing up existing config file..."
    Copy-Item $CONFIG_FILE $BACKUP_FILE
}

# Write new config contents
$configContent = @"
{
  "ip": "http://localhost:42069/api",
  "enableCookieAuth": true
}
"@

$configContent | Out-File -FilePath $CONFIG_FILE -Encoding UTF8

# Build Ecthelion
Set-Location ./ecthelion
corepack yarn
corepack yarn export
Set-Location ..

# Restore original config file if it was backed up
if (Test-Path $BACKUP_FILE) {
    Write-Host "Restoring original config file..."
    Move-Item $BACKUP_FILE $CONFIG_FILE -Force
} else {
    Remove-Item $CONFIG_FILE
}

# Build Octyne
go build @args
