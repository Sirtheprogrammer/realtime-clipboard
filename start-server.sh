#!/usr/bin/env sh
set -e

echo "======================================================="
echo "  Starting Realtime Clipboard Vault Server (Zero-Config)"
echo "======================================================="
echo ""
echo "Mode:      Zero-Config Embedded SQLite"
echo "Database:  ./data/clipboard.db"
echo "Blobs:     ./data/blobs"
echo "MasterKey: ./data/master.key (persistent)"
echo "URL:       http://localhost:8080"
echo ""

mkdir -p ./data

if [ -f "./clipboard-server" ]; then
    chmod +x ./clipboard-server
    ./clipboard-server
elif [ -f "./clipboard-server-linux-amd64" ]; then
    chmod +x ./clipboard-server-linux-amd64
    ./clipboard-server-linux-amd64
elif [ -f "./clipboard-server-darwin-arm64" ]; then
    chmod +x ./clipboard-server-darwin-arm64
    ./clipboard-server-darwin-arm64
else
    echo "[INFO] Running server with 'go run ./cmd/server'..."
    go run ./cmd/server
fi
