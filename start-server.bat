@echo off
setlocal
title Realtime Clipboard Vault Server

echo =======================================================
echo   Starting Realtime Clipboard Vault Server (Zero-Config)
echo =======================================================
echo.
echo Mode:     Zero-Config Embedded SQLite
echo Database: .\data\clipboard.db
echo Blobs:    .\data\blobs
echo MasterKey: .\data\master.key (auto-generated)
echo Server:   http://localhost:8080
echo.

if not exist data mkdir data

if exist clipboard-server.exe (
    clipboard-server.exe
) else if exist clipboard-server-windows-amd64.exe (
    clipboard-server-windows-amd64.exe
) else (
    echo [INFO] Running server with 'go run ./cmd/server'...
    go run ./cmd/server
)

pause
