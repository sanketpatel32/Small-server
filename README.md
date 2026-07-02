# Small-Server

A tiny, dependency-light REST API testing server. Built with **FastAPI** + **SQLite** (stdlib). Perfect for testing webhooks in Postman, firing payloads from your AI agents, or just having a scratch API to develop against.

- ⚡ **One command to start** — `uv` auto-installs deps
- 🗃️ **SQLite** file DB (no separate server, zero config)
- 📦 **Accepts ANY JSON payload** — objects, arrays, nested structs, strings, numbers, booleans, even `null`
- 🔐 **4 endpoints** — public + API-key-secured, each with GET & POST
- 🌐 **Built-in UI** — test everything at `http://localhost:8795/`
- 📚 **Free Swagger docs** at `/docs`
- 🔌 **Uncommon port 8795** — won't clash with your other dev servers

---

## 🚀 Quick start

### Option A — `uv` (recommended, fastest)

```bash
uv run uvicorn main:app --host 0.0.0.0 --port 8795 --reload
```

`uv` reads `pyproject.toml`, spins up an isolated env, installs FastAPI + uvicorn, and starts the server. No venv juggling.

### Option B — One-click scripts

| Platform | Command |
|---|---|
| **Windows** | double-click `run.bat` (or run it in a terminal) |
| **macOS / Linux / Git Bash** | `./run.sh` |

Both scripts auto-detect `uv` and fall back to plain `python -m pip` if it's missing.

### Option C — Plain Python

```bash
python -m pip install -r requirements.txt
python main.py
```

Once running, open → **http://localhost:8795**

---

## 🔑 Auth

Secure routes are protected by a static API key sent via header:

```
X-API-Key: secret-key-123
```

Change it any time via an environment variable:

```bash
# Windows (cmd)
set API_KEY=your-own-key-xyz && run.bat

# macOS / Linux / Git Bash
API_KEY=your-own-key-xyz ./run.sh
```

---

## 📡 Endpoints

Base URL: `http://localhost:8795`

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET`  | `/health` | — | Health check → `{"status":"ok"}` |
| `GET`  | `/public/items` | none | List public items |
| `POST` | `/public/items` | none | Store **any** JSON payload |
| `GET`  | `/secure/items` | `X-API-Key` | List secure items |
| `POST` | `/secure/items` | `X-API-Key` | Store **any** JSON payload |
| `GET`  | `/` | — | Built-in testing UI |
| `GET`  | `/docs` | — | Swagger UI (auto-generated) |

### Request body (POST endpoints) — accepts ANYTHING

The body can be **any valid JSON**. Whatever you POST is stored verbatim and echoed back unchanged. Examples:

```json
// a nested object
{ "event": "user.signup", "user": { "id": 42, "email": "a@b.com" }, "tags": ["new"] }

// an array
[1, 2, 3, { "nested": true }]

// a plain string
"just a string"

// a number / boolean / null
9001
true
null
```

> 💡 If you POST with no body at all, it's stored as `null` too.

### Response shape

Every stored item looks like this — `payload` is exactly what you sent:

```json
{
  "id": 1,
  "payload": { "event": "user.signup", "user": { "id": 42 }, "tags": ["new"] },
  "scope": "public",
  "created_at": "2026-06-30 12:34:56"
}
```

GET endpoints return an array of these.

---

## 🧪 Example calls

### Public POST — nested object (no auth)

```bash
curl -X POST http://localhost:8795/public/items \
  -H "Content-Type: application/json" \
  -d '{"event":"user.signup","user":{"id":42,"email":"a@b.com"},"tags":["new","trial"]}'
```

### Public POST — plain string

```bash
curl -X POST http://localhost:8795/public/items \
  -H "Content-Type: application/json" \
  -d '"hello world"'
```

### Secure POST — with API key

```bash
curl -X POST http://localhost:8795/secure/items \
  -H "Content-Type: application/json" \
  -H "X-API-Key: secret-key-123" \
  -d '{"secret":"data","ts":1690000000}'
```

### Secure GET — without key → 401

```bash
curl http://localhost:8795/secure/items
# -> {"detail":"Invalid or missing API key. Send header 'X-API-Key'."}
```

---

## 📮 Postman / Bruno

`postman_collection.json` is a ready-to-import collection (v2.1 format) with all 7 endpoints in **Meta / Public / Secure** folders. **Everything is hardcoded** — full URLs, sample payloads, and the `X-API-Key: secret-key-123` header right on the two secure requests. No environment, no variables, no pre-request scripts, no setup.

### Use it (zero config)

1. Start the server (see [Quick start](#-quick-start)).
2. Import `postman_collection.json`:
   - **Postman** → Import → drag the file in.
   - **Bruno** → Collection → Import → choose the file.
3. Open any request, hit **Send**. Done.

> Works in Postman **and** Bruno out of the box. The previous version used `{{variables}}` + a pre-request script that broke in Bruno (`ReferenceError: pm is not defined`) — those are gone. If you change the port or API key, edit them directly in the request URLs / headers (or in `main.py`).

### What's inside

- **7 requests** across Meta / Public / Secure folders.
- **Hardcoded values** — `http://localhost:8795` URLs and `X-API-Key: secret-key-123` on secure routes only.
- **Tests** on every request: Health → 200 + `status == "ok"` · GET lists → 200 + array · POST → 201 + payload echoed / `scope` correct.
- **Example responses** on each request so the preview is populated.

### Run the whole collection from CLI (Newman)

```bash
# Make sure the server is running first
npx newman run postman_collection.json
```

Expected: `7 requests, 10 assertions, 0 failed` — no `--env-var` flags needed.

---

## ☁️ Deploy to Fly.io

Small-Server is Fly.io-ready. SQLite is persisted via a Fly **volume** so your data survives restarts and deploys. A `Dockerfile` and `fly.toml` are included.

### Prerequisites

1. Install `flyctl`: https://fly.io/docs/flyctl/install/
2. Sign up / sign in: `fly auth signup` (or `fly auth login`)

### Deploy steps

```bash
# 1. Create the app (choose a unique name, pick your region)
fly launch --no-deploy        # uses the included fly.toml + Dockerfile

# 2. Create a 1GB persistent volume for SQLite (one-time)
fly volumes create small_server_data --size 1

# 3. Set your API key as a secret (don't commit it)
fly secrets set API_KEY="your-own-secret-key"

# 4. Deploy 🚀
fly deploy
```

When it finishes you'll get a URL like `https://small-server.fly.dev`. Open it → the test UI loads. Done.

> **Region:** the included `fly.toml` uses `bom` (Mumbai). Change it to your nearest — `iad` (US East), `sea` (US West), `lhr` (London), `sin` (Singapore), etc. — in `fly.toml` before `fly launch`.

> **App name:** `fly.toml` has `app = "small-server"`. If that's taken, Fly will prompt you for a new name during `fly launch`; update `app` in `fly.toml` to match.

### What's wired up

- **Persistent SQLite** — the `small_server_data` volume mounts at `/data`, and `DB_PATH=/data/data.db` is set in `fly.toml` + the Dockerfile. Your POSTed data survives every restart/redeploy.
- **Auto scale-to-zero** — `auto_stop_machines = true` stops the VM when idle, so it sips your free allowance. The first request after idle spins it back up in ~1–2s.
- **HTTPS** — Fly auto-provisions TLS. `force_https = true` redirects HTTP → HTTPS.
- **Configurable port** — `main.py` reads `PORT` (default `8795` locally). The container exposes `8080` per Fly convention.

### Reset the DB on Fly

```bash
fly ssh console -C "rm /data/data.db"
# next request recreates it fresh
```

---

## 📁 Files

```
Small-Server/
├── main.py                  # FastAPI app + SQLite logic (all endpoints)
├── index.html               # built-in testing UI (served at /)
├── postman_collection.json  # Postman v2.1 collection (all endpoints + tests)
├── Dockerfile               # Fly.io / container image (uv-based)
├── fly.toml                 # Fly.io config + persistent volume
├── pyproject.toml           # deps (for uv)
├── requirements.txt         # deps (for pip fallback)
├── uv.lock                  # locked deps (for uv reproducible installs)
├── run.bat                  # one-click launcher (Windows)
├── run.sh                   # one-click launcher (macOS/Linux/Git Bash)
├── .gitignore               # ignores venv, data.db, etc.
├── .dockerignore            # keeps image build context lean
├── README.md                # this file
└── data.db                  # SQLite DB (auto-created on first run, git-ignored)
```

---

## 🛠️ Tips

- **Hot reload** is on by default in the launchers (`--reload`). Edit `main.py` and the server picks up changes.
- **Reset the DB** — just delete `data.db`; it's recreated on next start. (On Fly: see the "Reset the DB on Fly" note above.)
- **Change the port** — set the `PORT` env var (default `8795` locally, `8080` on Fly).
- **Point Postman at it** — import from the OpenAPI spec: `http://localhost:8795/openapi.json`.
- **Webhook from an agent** — POST any JSON to `/public/items` (or `/secure/items` with the key) and watch it show up live in the UI at `/`.
- **Schema migration** — if you had data from the old `{title, body}` schema, the table auto-resets to the new `payload` shape on next start (old test data is dropped).

---

Happy hacking. 🚀
