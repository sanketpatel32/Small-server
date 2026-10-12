<div align="center">

# Small Server

**A tiny, single-binary REST API playground for webhooks, agents, and frontend development.**

Go standard library · SQLite · OpenAPI 3.1 · Postman/Bruno · No CGO

</div>

Store and inspect any valid JSON payload through public or secure CRUD routes. Only `/secure/*` endpoints require the configured API key; public CRUD, the HTTP lab, health, stats, UI, and docs routes remain public. Small Server includes a testing UI, persistent SQLite storage, an OpenAPI schema, ready-to-run API collections, and optional private Cloudflare R2 file storage.

| | |
|---|---|
| **Setup** | One static binary with no runtime or database server |
| **Payloads** | Objects, arrays, primitives, nested values, and `null` |
| **Platforms** | Windows, macOS, and Linux |
| **Interfaces** | Built-in browser UI, REST API, OpenAPI, Postman, and Bruno |
| **HTTP testing** | Echo, status simulation, delays, redirects, caching, and a response inspector |
| **Documentation** | Embedded API guide at `/docs` with test recipes |
| **Deployment** | Docker and Fly.io configuration included |

> [!IMPORTANT]
> Only `/secure/*` routes require `X-API-Key`. Public CRUD, health, stats, the UI, docs, and `/lab/*` do not require a key. Use a strong `API_KEY` and restrict network access before deploying publicly.

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
| `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD` | `/lab/echo` | — | Echo request method, path, query, headers, body, JSON parse, and byte count; sensitive headers are redacted |
| `GET`, `HEAD` | `/lab/status/{code}` | — | Return a chosen HTTP status from 200 through 599 |
| `GET`, `HEAD` | `/lab/delay/{ms}` | — | Wait 0–10,000 ms, then return a JSON result; request cancellation stops the wait |
| `GET`, `HEAD` | `/lab/redirect` | — | Redirect to a local absolute path with a selectable redirect status |
| `GET`, `HEAD` | `/lab/cache` | — | Exercise ETag and conditional 304 caching behavior |
| **Public** | | | |
| `GET`    | `/public/items` | — | **List** all public items |
| `POST`   | `/public/items` | — | **Create** — store any JSON payload |
| `GET`    | `/public/items/{id}` | — | **Read** one public item |
| `PUT`    | `/public/items/{id}` | — | **Update** — replace a public item's payload |
| `DELETE` | `/public/items/{id}` | — | **Delete** one public item |
| `DELETE` | `/public/items` | — | **Clear all** public items |
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
| `GET` | `/secure/uploads/status` | `X-API-Key` | Check optional R2 setup; returns `configured` and the 10 MiB limit without exposing credentials |
| `GET` | `/secure/uploads` | `X-API-Key` | List up to 100 files in the app's private `uploads/` prefix; supports an opaque `cursor` (maximum 2048 bytes) |
| `POST` | `/secure/uploads?filename=example.txt` | `X-API-Key` | Upload nonempty raw binary data up to 10 MiB (not multipart); filename must be ASCII alphanumeric first, then ASCII alphanumeric, `.`, `_`, or `-`, max 128 characters |
| `GET`, `HEAD` | `/secure/uploads/{key}` | `X-API-Key` | Download a protected attachment or inspect its headers |
| `DELETE` | `/secure/uploads/{key}` | `X-API-Key` | Idempotently delete an uploaded file (204) |

### HTTP lab

The HTTP lab is public; only `/secure/*` endpoints require `X-API-Key`. `GET /lab/echo` is useful for inspecting a request. It returns `method`, `path`, `query` (each query value as an array), `headers` (each header value as an array), `body` (raw text), `json` (parsed JSON or `null`), and `bytes`. `Authorization`, `Cookie`, and `X-API-Key` values are redacted. Bodies larger than 1 MiB receive `413 Payload Too Large`.

The status endpoint accepts any code from 200 to 599 and returns `{"status":N,"message":"..."}`. Statuses 204, 205, and 304 have no response body. A 429 response includes `Retry-After: 5`.

The delay endpoint accepts an integer from 0 to 10,000 milliseconds and returns `{"delay_ms":N,"status":"ok"}` after the wait. The wait is cancellable when the client disconnects. Redirects default to `/lab/echo` and status 302; use `to` for a local absolute path and `code` for 301, 302, 303, 307, or 308. External and protocol-relative destinations are rejected. The cache endpoint returns a fixed ETag (`"small-server-v1"`) and `Cache-Control: public, max-age=60`; matching `If-None-Match` (including `*`) returns 304.

The workspace has three sections: **Save data** for messages or JSON, **Try a test** for ready-to-run examples, and **Files** for private uploads. Use **Access key** at the top once to unlock private actions. Advanced request settings, response headers, file keys, and server activity are available in expandable sections. Recent tests stay in memory only until the page is reloaded.

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

### Create — public POST

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

### Upload a file to private Cloudflare R2 storage

R2 storage is optional and uses server-side S3-compatible requests without an extra SDK or dependency. Create an R2 bucket and an R2 API token with **Object Read & Write** permission scoped to that bucket. Keep the bucket private: a public bucket or bucket CORS rules are not needed. Cloudflare shows the Secret Access Key only once; save it securely with the Access Key ID. [Cloudflare's token guide](https://developers.cloudflare.com/r2/api/tokens/) explains token creation and endpoint formats.

Configure these variables locally in the gitignored `.env.r2` file (create it yourself and fill in the R2 values; never commit or share it):

```dotenv
R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
R2_BUCKET=your-bucket-name
R2_ACCESS_KEY_ID=your-access-key-id
R2_SECRET_ACCESS_KEY=your-secret-access-key
```

For jurisdiction-specific buckets, use that jurisdiction's endpoint, such as `https://<account-id>.eu.r2.cloudflarestorage.com`, `https://<account-id>.us.r2.cloudflarestorage.com`, or `https://<account-id>.fedramp.r2.cloudflarestorage.com`. The app reads process environment variables. Load the ignored file in your terminal before starting it:

```bash
set -a; . ./.env.r2; set +a
go run .
```

In PowerShell, load it without printing the values, then start the server:

```powershell
Get-Content .env.r2 | ForEach-Object {
  if ($_ -match '^([^#][^=]*)=(.*)$') {
    [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
  }
}
go run .
```

On the existing Fly app, import the ignored `.env.fly` and `.env.r2` files from the repository root, then deploy:

```powershell
fly secrets import < .env.fly
fly secrets import < .env.r2
fly deploy -a small-server
```

`GET /secure/uploads/status` reports whether all R2 settings are present and the maximum size (`10485760` bytes). Uploads return 503 when R2 is not configured, 400 for an empty body or invalid filename, 413 above 10 MiB, 429 with `Retry-After: 1` when four uploads are already in progress, 404 for a missing download, and a generic 502 on a storage failure. The UI normalizes selected filenames to the allowed ASCII letters, digits, dot, underscore, and hyphen format before upload; API callers must provide a valid filename directly. The random key has a 32-character lowercase hex prefix followed by `-` and the validated filename. At most four uploads run concurrently; an additional upload request receives 429 and `Retry-After: 1`. Listing and file operations are restricted to the app's `uploads/` object prefix.

Use `curl --data-binary` so the request body is sent as raw bytes. Replace `YOUR-API-KEY` with the configured key:

```bash
curl -X POST 'https://small-server.fly.dev/secure/uploads?filename=example.txt' \
  -H 'X-API-Key: YOUR-API-KEY' \
  -H 'Content-Type: application/octet-stream' \
  --data-binary @example.txt

curl 'https://small-server.fly.dev/secure/uploads' \
  -H 'X-API-Key: YOUR-API-KEY'

# Use the key returned by the upload response or listing
curl -OJ 'https://small-server.fly.dev/secure/uploads/0123456789abcdef0123456789abcdef-example.txt' \
  -H 'X-API-Key: YOUR-API-KEY'
curl -X DELETE 'https://small-server.fly.dev/secure/uploads/0123456789abcdef0123456789abcdef-example.txt' \
  -H 'X-API-Key: YOUR-API-KEY'
```

Cloudflare R2 Standard currently includes 10 GB-month storage, 1 million Class A operations, and 10 million Class B operations each month; usage above the free tier is billed by Cloudflare separately from Fly. [See current R2 pricing](https://developers.cloudflare.com/r2/pricing/).

---

## 📮 Postman / Bruno

`postman_collection.json` is a ready-to-import collection (v2.1 format) with Meta, HTTP Lab, Public, Secure, and Uploads folders. Only the Uploads folder has a folder-level `X-API-Key`, set to the development default `secret-key-123`; update that folder auth for a different API key. Secure CRUD requests keep their own key headers, while public and missing-key examples remain unauthenticated.

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

Small-Server is deployed at **https://small-server.fly.dev**. SQLite is persisted via a Fly **volume** so your data survives restarts and deploys. The existing app uses one 512 MB auto-stopping VM in Amsterdam (`ams`) and a 1 GB volume. A `Dockerfile` (multi-stage Go build) and `fly.toml` are included.

### Prerequisites

Install and sign in to `flyctl` if needed: https://fly.io/docs/flyctl/install/

### Deploy steps

```bash
# Import the locally ignored API key file, then deploy the existing app.
fly secrets import < .env.fly
fly deploy -a small-server
```

Open **https://small-server.fly.dev** to use the test UI. Only `/secure/*` endpoints require the API key in `X-API-Key`; public and lab routes do not.

> **Region:** the existing VM and volume are in `ams` (Amsterdam). Review volume and machine placement before changing the app's region.

### What's wired up

- **Persistent SQLite** — the 1 GB `small_server_data` volume mounts at `/data`, and `DB_PATH=/data/data.db` is set in `fly.toml` + the Dockerfile. Your POSTed data survives every restart/redeploy.
- **Tiny image** — multi-stage build produces a ~20MB image (static Go binary on Alpine). Cold start is ~100ms — no more health-check warnings.
- **Auto scale-to-zero** — the VM sleeps when idle, wakes in ~1s on the first request.
- **HTTPS** — Fly auto-provisions TLS. `force_https = true` redirects HTTP → HTTPS.

### Reset the DB on Fly

```bash
curl -X DELETE https://small-server.fly.dev/public/items
curl -X DELETE https://small-server.fly.dev/secure/items -H "X-API-Key: YOUR-KEY"
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
