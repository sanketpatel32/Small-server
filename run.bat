@echo off
REM One-command launcher for Windows. Double-click or run: run.bat
REM uv auto-installs deps from pyproject.toml; falls back to pip if uv missing.
where uv >nul 2>&1
if %errorlevel%==0 (
    echo [small-server] starting with uv ...
    uv run uvicorn main:app --host 0.0.0.0 --port 8795 --reload
) else (
    echo [small-server] uv not found, using python ...
    python -m pip install -q -r requirements.txt
    python -m uvicorn main:app --host 0.0.0.0 --port 8795 --reload
)
