#!/usr/bin/env bash
# One-command launcher for macOS / Linux / Git Bash.
# Runs the Go server with hot rebuild via `go run`. Falls back to the prebuilt
# binary if Go isn't installed.
set -e
cd "$(dirname "$0")"

if command -v go >/dev/null 2>&1; then
    echo "[small-server] starting with: go run ."
    PORT="${PORT:-8795}" exec go run .
else
    if [ ! -x ./small-server ]; then
        echo "[small-server] Go not found and no prebuilt binary. Install Go or build in Docker." >&2
        exit 1
    fi
    echo "[small-server] starting prebuilt binary"
    PORT="${PORT:-8795}" exec ./small-server
fi
