#!/usr/bin/env bash
# One-command launcher for macOS / Linux / Git Bash.
# uv auto-installs deps from pyproject.toml; falls back to pip if uv missing.
set -e
cd "$(dirname "$0")"

if command -v uv >/dev/null 2>&1; then
    echo "[small-server] starting with uv ..."
    exec uv run uvicorn main:app --host 0.0.0.0 --port 8795 --reload
else
    echo "[small-server] uv not found, using python ..."
    python -m pip install -q -r requirements.txt
    exec python -m uvicorn main:app --host 0.0.0.0 --port 8795 --reload
fi
