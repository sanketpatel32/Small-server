# syntax=docker/dockerfile:1
# Small-Server — Fly.io-ready image. Uses uv for fast, deterministic installs.
FROM python:3.12-slim

# uv: install the binary (no need for the full uv image)
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

WORKDIR /app

# Install deps first (better layer caching — rebuild only when deps change)
COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-dev --no-install-project

# App code
COPY main.py index.html ./

# Persistent SQLite lives here (mounted as a Fly volume in production)
RUN mkdir -p /data
VOLUME ["/data"]

ENV DB_PATH=/data/data.db \
    PORT=8080 \
    UV_LINK_MODE=copy \
    PATH="/app/.venv/bin:$PATH"

# Run the venv's uvicorn directly — no `uv run` overhead on cold start.
# (The 3-4s cold-start delay from `uv run` was tripping Fly's "not listening
# on port" health check, since uv re-resolves the project on every boot.)
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8080"]
