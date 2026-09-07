<#
.SYNOPSIS
    Starts the Realtime Clipboard Vault Server in Zero-Config SQLite mode.
#>
$ErrorActionPreference = "Stop"

Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "  Starting Realtime Clipboard Vault Server (Zero-Config) " -ForegroundColor Cyan
Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Mode:      Zero-Config Embedded SQLite" -ForegroundColor Green
Write-Host "Database:  .\data\clipboard.db"
Write-Host "Blobs:     .\data\blobs"
Write-Host "MasterKey: .\data\master.key (persistent)"
Write-Host "URL:       http://localhost:8080" -ForegroundColor Yellow
Write-Host ""

if (-not (Test-Path ".\data")) {
    New-Item -ItemType Directory -Path ".\data" | Out-Null
}

if (Test-Path ".\clipboard-server.exe") {
    & ".\clipboard-server.exe"
} elseif (Test-Path ".\clipboard-server-windows-amd64.exe") {
    & ".\clipboard-server-windows-amd64.exe"
} else {
    Write-Host "[INFO] Running server with 'go run ./cmd/server'..." -ForegroundColor Gray
    go run ./cmd/server
}
