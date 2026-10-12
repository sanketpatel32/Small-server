# Small-Server

A tiny, ultra-light REST API testing server. Built with **Go** (stdlib net/http) + **SQLite** (pure-Go driver, no CGO). Perfect for testing webhooks in Postman/Bruno, firing payloads from your AI agents, or just having a scratch API to develop against.

- ⚡ **Single static binary** — ~15MB, no runtime, no dependencies to install
- 🐧 **Cross-platform** — builds for Linux/macOS/Windows from one codebase
- 🗃️ **SQLite** file DB (no separate server, zero config)
- 📦 **Full CRUD** — create, read (list + one), update, delete (one + clear-all)
- 📦 **Accepts ANY JSON payload** — objects, arrays, nested structs, strings, numbers, booleans, even `null`
- 🔐 **Public + API-key-secured routes** — same CRUD on both, scope-isolated
- 🌐 **Built-in UI** — test everything at `http://localhost:8795/`
- 🧪 **HTTP lab** — echo requests, simulate status codes, delays, redirects, and caching
- 📖 **Embedded API docs** — open `/docs` for the API reference and test recipes
- 🌍 **CORS open** — call it from any browser app (React/Vue/etc.)
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
| `GET`    | `/health` | — | Health check → `{"status":"ok"}` |
| `GET`    | `/stats` | — | **Telemetry** — request counts, uptime, methods/statuses (in-memory, resets on restart) |
| **HTTP lab** | | | |
| `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD` | `/lab/echo` | none | Echo request method, path, query, headers, body, JSON parse, and byte count; sensitive headers are redacted |
| `GET`, `HEAD` | `/lab/status/{code}` | none | Return a chosen HTTP status from 200 through 599 |
| `GET`, `HEAD` | `/lab/delay/{ms}` | none | Wait 0–10,000 ms, then return a JSON result; request cancellation stops the wait |
| `GET`, `HEAD` | `/lab/redirect` | none | Redirect to a local absolute path with a selectable redirect status |
| `GET`, `HEAD` | `/lab/cache` | none | Exercise ETag and conditional 304 caching behavior |
| **Public** | | | |
| `GET`    | `/public/items` | none | **List** all public items |
| `POST`   | `/public/items` | none | **Create** — store any JSON payload |
| `GET`    | `/public/items/{id}` | none | **Read** one public item |
| `PUT`    | `/public/items/{id}` | none | **Update** — replace a public item's payload |
| `DELETE` | `/public/items/{id}` | none | **Delete** one public item |
| `DELETE` | `/public/items` | none | **Clear all** public items |
| **Secure** | | | |
| `GET`    | `/secure/items` | `X-API-Key` | **List** all secure items |
| `POST`   | `/secure/items` | `X-API-Key` | **Create** — store any JSON payload |
| `GET`    | `/secure/items/{id}` | `X-API-Key` | **Read** one secure item |
| `PUT`    | `/secure/items/{id}` | `X-API-Key` | **Update** — replace a secure item's payload |
| `DELETE` | `/secure/items/{id}` | `X-API-Key` | **Delete** one secure item |
| `DELETE` | `/secure/items` | `X-API-Key` | **Clear all** secure items |
| **Helpers** | | | |
| `GET`    | `/` | — | Built-in testing UI (stats bar, per-item CRUD actions, search, auto-refresh, compose-any-request) |
| `GET`    | `/openapi.json` | — | OpenAPI 3.1 schema (for importers) |
| `GET` | `/docs` | — | Embedded API documentation page |

### HTTP lab

`GET /lab/echo` is useful for inspecting a request. It returns `method`, `path`, `query` (each query value as an array), `headers` (each header value as an array), `body` (raw text), `json` (parsed JSON or `null`), and `bytes`. `Authorization`, `Cookie`, and `X-API-Key` values are redacted. Bodies larger than 1 MiB receive `413 Payload Too Large`.

The status endpoint accepts any code from 200 to 599 and returns `{"status":N,"message":"..."}`. Statuses 204, 205, and 304 have no response body. A 429 response includes `Retry-After: 5`.

The delay endpoint accepts an integer from 0 to 10,000 milliseconds and returns `{"delay_ms":N,"status":"ok"}` after the wait. The wait is cancellable when the client disconnects. Redirects default to `/lab/echo` and status 302; use `to` for a local absolute path and `code` for 301, 302, 303, 307, or 308. External and protocol-relative destinations are rejected. The cache endpoint returns a fixed ETag (`"small-server-v1"`) and `Cache-Control: public, max-age=60`; matching `If-None-Match` (including `*`) returns 304.

The UI includes lab scenario presets and custom requests with headers, raw bodies, timeouts, cancellation, status assertions, response headers, and request history. UI history is kept in the current browser session only.

> 🔒 Secure routes require header `X-API-Key: secret-key-123` (override via `API_KEY` env var).
> 🔁 **Scope isolation:** an `id` only resolves within its own scope — `/public/items/5` won't return a secure item even if id 5 exists there.

### Request body (POST/PUT endpoints) — accepts ANYTHING

The body can be **any valid JSON**. Whatever you send is stored verbatim and echoed back unchanged. Examples:

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

`GET /.../items` returns an array of these (empty → `[]`, not `null`).
`GET /.../items/{id}` returns one, or `404 {"detail":"Item not found."}` if missing.
`DELETE /.../items/{id}` returns `{"status":"deleted","id":N}`.
`DELETE /.../items` (clear all) returns `{"status":"cleared","scope":"...","removed":N}`.

---

## 🧪 Example calls

### Create — public POST (no auth)

```bash
curl -X POST http://localhost:8795/public/items \
  -H "Content-Type: application/json" \
  -d '{"event":"user.signup","user":{"id":42,"email":"a@b.com"},"tags":["new","trial"]}'
```

### Read one — by id

```bash
curl http://localhost:8795/public/items/1
```

### Update — PUT replaces the payload

```bash
curl -X PUT http://localhost:8795/public/items/1 \
  -H "Content-Type: application/json" \
  -d '{"event":"user.updated","user":{"id":42,"email":"new@b.com"}}'
```

### Delete one — by id

```bash
curl -X DELETE http://localhost:8795/public/items/1
# -> {"status":"deleted","id":1}
```

### Clear all — delete every public item

```bash
curl -X DELETE http://localhost:8795/public/items
# -> {"status":"cleared","scope":"public","removed":3}
```

### Secure — with API key

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

`postman_collection.json` is a ready-to-import collection (v2.1 format) with Meta, HTTP Lab, Public, and Secure folders. **Everything is hardcoded** — full URLs, sample payloads, and the `X-API-Key: secret-key-123` header on secure requests. No environment or setup is needed.

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

The collection includes lab requests as well as CRUD requests; the exact request and assertion totals depend on the collection version. No `--env-var` flags are needed.

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
curl -X DELETE https://YOUR-APP.fly.dev/public/items
curl -X DELETE https://YOUR-APP.fly.dev/secure/items -H "X-API-Key: YOUR-KEY"
```

---

## 📁 Files

```
Small-Server/
├── main.go                  # storage, telemetry, routing, auth, embedded assets
├── lab.go                   # HTTP behavior simulation and request echo
├── main_test.go             # isolated Go API regression tests
├── ui_test.cjs              # dependency-free console behavior checks
├── index.html               # built-in testing UI (embedded into the binary at build time)
├── docs.html                # embedded API documentation page served at /docs
├── openapi.json             # OpenAPI 3.1 schema (embedded; served at /openapi.json)
├── postman_collection.json  # Postman/Bruno v2.1 collection (CRUD + HTTP lab requests)
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

- **Clear test data** — use `DELETE /public/items` and authenticated `DELETE /secure/items`. For a complete local reset, stop the server, then delete `data.db` and its `data.db-shm`/`data.db-wal` files if present; they are recreated on next start.
- **Change the port** — set the `PORT` env var (default `8795` locally, `8080` on Fly).
- **Webhook from an agent** — POST any JSON to `/public/items` (or `/secure/items` with the key) and watch it show up live in the UI at `/`.
- **Cross-compile** — `GOOS=linux GOARCH=arm64 go build -o small-server .` builds for a Raspberry Pi from your laptop. No CGO required.

---

## Development checks

```bash
go test ./...
go vet ./...
go build .
node --test ui_test.cjs
```

Go tests use temporary SQLite files and cover scope isolation, JSON payloads, lab parameters, redaction, caching, cancellation, HEAD, and CORS. The optional Node check uses the built-in test runner to cover console validation, raw requests, timeout/cancel behavior, response escaping, history, and null payloads. No frontend packages are needed. Restart or rebuild the app after changing embedded HTML or OpenAPI.
