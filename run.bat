@echo off
REM One-command launcher for Windows. Double-click or run: run.bat
REM Runs via `go run .` if Go is installed, else the prebuilt binary.
where go >nul 2>&1
if %errorlevel%==0 (
    echo [small-server] starting with: go run .
    if "%PORT%"=="" set PORT=8795
    go run .
) else (
    if exist small-server.exe (
        echo [small-server] starting prebuilt binary
        if "%PORT%"=="" set PORT=8795
        small-server.exe
    ) else (
        echo [small-server] Go not found and no small-server.exe. Install Go or build in Docker.
        exit /b 1
    )
)
