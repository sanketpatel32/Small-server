# Small-Server

A tiny, ultra-light REST API testing server. Built with **Go** (stdlib net/http) + **SQLite** (pure-Go driver, no CGO). Perfect for testing webhooks in Postman/Bruno, firing payloads from your AI agents, or just having a scratch API to develop against.

- ⚡ **Single static binary** — ~15MB, no runtime, no dependencies to install
- 🐧 **Cross-platform** — builds for Linux/macOS/Windows from one codebase
- 🗃️ **SQLite** file DB (no separate server, zero config)
- 📦 **Accepts ANY JSON payload** — objects, arrays, nested structs, strings, numbers, booleans, even `null`
- 🔐 **4 endpoints** — public + API-key-secured, each with GET & POST
- 🌐 **Built-in UI** — test everything at `http://localhost:8795/`
- 🔌 **Uncommon port 8795** — won't clash with your other dev servers

---

## 🚀 Quick start

### Option A — `go run` (recommended if you have Go)

```bash
go run .
```

Open → **http://localhost:8795**

### Option B — Build a binary

```bash
# native binary (Linux/macOS)
go build -o small-server .
./small-server

# Windows
go build -o small-server.exe .
small-server.exe
```

### Option C — One-click scripts

| Platform | Command |
|---|---|
| **Windows** | double-click `run.bat` (or run it in a terminal) |
| **macOS / Linux / Git Bash** | `./run.sh` |

Both scripts auto-detect Go and fall back to a prebuilt binary if present.

### Change the port / DB location / API key

```bash
# env vars (all optional, these are the defaults)
PORT=8795
DB_PATH=./data.db
API_KEY=secret-key-123
```

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
| `GET`  | `/openapi.json` | — | OpenAPI 3.1 schema (for importers) |

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
  "created_at": "2026-07-02 12:34:56"
}
```

GET endpoints return an array of these (empty DB → `[]`, not `null`).

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

> Works in Postman **and** Bruno out of the box.

### Run the whole collection from CLI (Newman)

```bash
# Make sure the server is running first
npx newman run postman_collection.json
```

Expected: `7 requests, 10 assertions, 0 failed` — no `--env-var` flags needed.

---

## ☁️ Deploy to Fly.io

Small-Server is Fly.io-ready. SQLite is persisted via a Fly **volume** so your data survives restarts and deploys. A `Dockerfile` (multi-stage Go build) and `fly.toml` are included.

### Prerequisites

1. Install `flyctl`: https://fly.io/docs/flyctl/install/
2. Sign up / sign in: `fly auth signup` (or `fly auth login`)

### Deploy steps

```bash
# 1. Create the app (uses the included Dockerfile + fly.toml)
fly launch --no-deploy

# 2. Create a 1GB persistent volume for SQLite (one-time)
fly volumes create small_server_data --size 1

# 3. Set your API key as a secret (don't commit it)
fly secrets set API_KEY="your-own-secret-key"

# 4. Deploy 🚀
fly deploy
```

When it finishes you'll get a URL like `https://small-server.fly.dev`. Open it → the test UI loads. Done.

> **Region:** the included `fly.toml` uses `ams` (Amsterdam). Change it in `fly.toml` before `fly launch` if you want somewhere closer.

### What's wired up

- **Persistent SQLite** — the `small_server_data` volume mounts at `/data`, and `DB_PATH=/data/data.db` is set in `fly.toml` + the Dockerfile. Your POSTed data survives every restart/redeploy.
- **Tiny image** — multi-stage build produces a ~20MB image (static Go binary on Alpine). Cold start is ~100ms — no more health-check warnings.
- **Auto scale-to-zero** — the VM sleeps when idle, wakes in ~1s on the first request.
- **HTTPS** — Fly auto-provisions TLS. `force_https = true` redirects HTTP → HTTPS.

### Reset the DB on Fly

```bash
fly ssh console -a small-server --command "rm /data/data.db"
# next request recreates it fresh
```

---

## 📁 Files

```
Small-Server/
├── main.go                  # the entire Go app (handlers, DB, auth, embedded UI)
├── index.html               # built-in testing UI (embedded into the binary at build time)
├── openapi.json             # OpenAPI 3.1 schema (embedded; served at /openapi.json)
├── postman_collection.json  # Postman/Bruno v2.1 collection (all endpoints + tests)
├── go.mod / go.sum          # Go module + locked deps (modernc.org/sqlite, pure Go)
├── Dockerfile               # multi-stage Go build → tiny runtime image
├── fly.toml                 # Fly.io config + persistent volume + health check
├── run.bat                  # one-click launcher (Windows)
├── run.sh                   # one-click launcher (macOS/Linux/Git Bash)
├── .gitignore / .dockerignore / .gitattributes
└── README.md                # this file
```

`data.db` is auto-created on first run and git-ignored.

---

## 🛠️ Tips

- **Reset the DB** — just delete `data.db` (+ `data.db-shm`/`data.db-wal` if present); it's recreated on next start.
- **Change the port** — set the `PORT` env var (default `8795` locally, `8080` on Fly).
- **Webhook from an agent** — POST any JSON to `/public/items` (or `/secure/items` with the key) and watch it show up live in the UI at `/`.
- **Cross-compile** — `GOOS=linux GOARCH=arm64 go build -o small-server .` builds for a Raspberry Pi from your laptop. No CGO required.

---

Happy hacking. 🚀
