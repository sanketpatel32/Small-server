"""
Small-Server — a tiny REST API for testing webhooks & Postman experiments.

Endpoints:
    Public (no auth):
        GET  /public/items          list all public items
        POST /public/items          store ANY JSON payload

    Secure (API key required):
        GET  /secure/items          list all secure items
        POST /secure/items          store ANY JSON payload

    Helpers:
        GET  /                      built-in testing UI
        GET  /health                health check
        GET  /docs                  auto-generated OpenAPI docs (Swagger)

POST bodies accept arbitrary JSON — objects, arrays, nested structs, anything
valid JSON. It's stored verbatim and echoed back on GET.

Auth: send header  X-API-Key: secret-key-123  on the /secure/* routes.
"""

from __future__ import annotations

import json
import os
import sqlite3
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager, contextmanager
from pathlib import Path
from typing import Annotated, Any, Iterator

from fastapi import Body, Depends, FastAPI, Header, HTTPException, status
from fastapi.responses import HTMLResponse
from pydantic import BaseModel

# ─────────────────────────────────────────────────────────────────────────────
# Config
# ─────────────────────────────────────────────────────────────────────────────

API_KEY = os.getenv("API_KEY", "secret-key-123")
# Local: ./data.db  |  Fly.io/containers: /data/data.db (persistent volume)
DB_PATH = Path(os.getenv("DB_PATH", str(Path(__file__).parent / "data.db")))
UI_PATH = Path(__file__).parent / "index.html"
PORT = int(os.getenv("PORT", "8795"))

# ─────────────────────────────────────────────────────────────────────────────
# Database (stdlib sqlite3 — zero extra deps, file-backed)
# ─────────────────────────────────────────────────────────────────────────────


@contextmanager
def get_db() -> Iterator[sqlite3.Connection]:
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row  # rows behave like dicts
    try:
        yield conn
        conn.commit()
    finally:
        conn.close()


def init_db() -> None:
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    with get_db() as conn:
        # Migrate: if an older schema (title/body columns) exists, drop & rebuild.
        cols = [r[1] for r in conn.execute("PRAGMA table_info(items)").fetchall()]
        if cols and "payload" not in cols:
            conn.execute("DROP TABLE items")
            print("[small-server] old schema detected — resetting items table")
        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS items (
                id         INTEGER PRIMARY KEY AUTOINCREMENT,
                payload    TEXT    NOT NULL,   -- raw JSON of whatever was POSTed
                scope      TEXT    NOT NULL DEFAULT 'public',  -- 'public' | 'secure'
                created_at TEXT    NOT NULL DEFAULT (datetime('now'))
            )
            """
        )


# ─────────────────────────────────────────────────────────────────────────────
# App lifespan — modern replacement for the deprecated @app.on_event("startup")
# ─────────────────────────────────────────────────────────────────────────────


@asynccontextmanager
async def app_lifespan(_: FastAPI) -> AsyncIterator[None]:
    init_db()
    yield


# ─────────────────────────────────────────────────────────────────────────────
# Models
# ─────────────────────────────────────────────────────────────────────────────


class ItemOut(BaseModel):
    id: int
    payload: Any  # whatever JSON you POSTed, stored verbatim
    scope: str
    created_at: str


def row_to_item(row: sqlite3.Row) -> ItemOut:
    """Convert a DB row into an ItemOut, parsing the stored JSON payload."""
    d = dict(row)
    d["payload"] = json.loads(d["payload"])
    return ItemOut(**d)


# ─────────────────────────────────────────────────────────────────────────────
# Auth dependency — applied to every /secure/* route
# ─────────────────────────────────────────────────────────────────────────────


def verify_api_key(x_api_key: Annotated[str | None, Header()] = None) -> None:
    if x_api_key != API_KEY:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid or missing API key. Send header 'X-API-Key'.",
            headers={"WWW-Authenticate": "ApiKey"},
        )


# ─────────────────────────────────────────────────────────────────────────────
# App
# ─────────────────────────────────────────────────────────────────────────────

app = FastAPI(
    title="Small-Server",
    version="0.2.0",
    description="A tiny REST API testing server — accepts any JSON payload, SQLite-backed.",
    lifespan=app_lifespan,
)


@app.get("/", response_class=HTMLResponse, include_in_schema=False)
def ui() -> HTMLResponse:
    """Serve the built-in testing UI."""
    return HTMLResponse(UI_PATH.read_text(encoding="utf-8"))


@app.get("/health", tags=["meta"])
def health() -> dict[str, str]:
    return {"status": "ok"}


# ── Public routes ───────────────────────────────────────────────────────────


@app.get("/public/items", response_model=list[ItemOut], tags=["public"])
def list_public() -> list[ItemOut]:
    """List all public items."""
    with get_db() as conn:
        rows = conn.execute(
            "SELECT * FROM items WHERE scope = 'public' ORDER BY id DESC"
        ).fetchall()
    return [row_to_item(r) for r in rows]


@app.post("/public/items", response_model=ItemOut, status_code=201, tags=["public"])
def create_public(payload: Annotated[Any, Body()] = None) -> ItemOut:
    """Store ANY JSON payload as a public item (no auth). `null` is accepted."""
    with get_db() as conn:
        cur = conn.execute(
            "INSERT INTO items (payload, scope) VALUES (?, 'public')",
            (json.dumps(payload),),
        )
        row = conn.execute(
            "SELECT * FROM items WHERE id = ?", (cur.lastrowid,)
        ).fetchone()
    return row_to_item(row)


# ── Secure routes (API key required) ────────────────────────────────────────


@app.get(
    "/secure/items",
    response_model=list[ItemOut],
    tags=["secure"],
    dependencies=[Depends(verify_api_key)],
)
def list_secure() -> list[ItemOut]:
    """List all secure items. Requires X-API-Key header."""
    with get_db() as conn:
        rows = conn.execute(
            "SELECT * FROM items WHERE scope = 'secure' ORDER BY id DESC"
        ).fetchall()
    return [row_to_item(r) for r in rows]


@app.post(
    "/secure/items",
    response_model=ItemOut,
    status_code=201,
    tags=["secure"],
    dependencies=[Depends(verify_api_key)],
)
def create_secure(payload: Annotated[Any, Body()] = None) -> ItemOut:
    """Store ANY JSON payload as a secure item. Requires X-API-Key header."""
    with get_db() as conn:
        cur = conn.execute(
            "INSERT INTO items (payload, scope) VALUES (?, 'secure')",
            (json.dumps(payload),),
        )
        row = conn.execute(
            "SELECT * FROM items WHERE id = ?", (cur.lastrowid,)
        ).fetchone()
    return row_to_item(row)


# ─────────────────────────────────────────────────────────────────────────────
# Entry point — `python main.py` just works
# ─────────────────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=PORT,
        reload=False,
        log_level="info",
    )
