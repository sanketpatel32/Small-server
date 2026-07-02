// Small-Server — a tiny REST API for testing webhooks & Postman/Bruno experiments.
//
// Endpoints:
//
//	Public (no auth):
//	    GET  /public/items          list all public items
//	    POST /public/items          store ANY JSON payload
//
//	Secure (API key required):
//	    GET  /secure/items          list all secure items
//	    POST /secure/items          store ANY JSON payload
//
//	Helpers:
//	    GET  /                      built-in testing UI (embedded)
//	    GET  /health                health check
//	    GET  /docs                  (alias of /) — Swagger not auto-generated in Go
//	    GET  /openapi.json          static OpenAPI schema for importers
//
// POST bodies accept arbitrary JSON — objects, arrays, nested structs, anything
// valid JSON. It's stored verbatim and echoed back on GET.
//
// Auth: send header  X-API-Key: secret-key-123  on the /secure/* routes.
package main

import (
	"embed"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Pure-Go SQLite driver — no CGO, so static binaries & cross-compile just work.
	_ "modernc.org/sqlite"
)

//go:embed index.html openapi.json
var assets embed.FS

// ─────────────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────────────

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var (
	apiKey = env("API_KEY", "secret-key-123")
	// Local: ./data.db  |  Fly.io/containers: /data/data.db (persistent volume)
	dbPath = env("DB_PATH", filepath.Join(".", "data.db"))
	port   = env("PORT", "8795")
)

// ─────────────────────────────────────────────────────────────────────────────
// Models
// ─────────────────────────────────────────────────────────────────────────────

// ItemOut is the JSON shape returned by every endpoint. Payload is `any` —
// whatever JSON you POSTed is stored verbatim and echoed back unchanged.
type ItemOut struct {
	ID        int64           `json:"id"`
	Payload   json.RawMessage `json:"payload"`
	Scope     string          `json:"scope"`
	CreatedAt string          `json:"created_at"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Database
// ─────────────────────────────────────────────────────────────────────────────

var db *sql.DB

// sqliteDSN builds a modernc.org/sqlite DSN that allows writes only when the
// file/dir are genuinely writable (no silent readonly fallthrough).
func sqliteDSN() string {
	return "file:" + dbPath +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_txlock=immediate"
}

// openDB opens the SQLite file and verifies write access with a REAL write
// (insert into a temp table, then drop it) wrapped in a transaction. This
// catches read-only files that a CREATE-TABLE-IF-NOT-EXISTS probe would miss.
func openDB() (*sql.DB, error) {
	conn, err := sql.Open("sqlite", sqliteDSN())
	if err != nil {
		return nil, err
	}
	// Real write probe: this fails on a readonly file/dir.
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS items (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		payload    TEXT    NOT NULL,
		scope      TEXT    NOT NULL DEFAULT 'public',
		created_at TEXT    NOT NULL DEFAULT (datetime('now'))
	);`); err != nil {
		conn.Close()
		return nil, err
	}
	tx, err := conn.Begin()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS _wprobe (x INTEGER); DELETE FROM _wprobe;`); err != nil {
		tx.Rollback()
		conn.Close()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		conn.Close()
		return nil, err
	}
	_, _ = conn.Exec(`DROP TABLE IF EXISTS _wprobe;`)
	return conn, nil
}

func initDB() {
	// Make sure the parent dir exists (Fly volume starts empty, local dir is fine).
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		log.Fatalf("initDB: mkdir parent: %v", err)
	}

	// Best-effort: relax perms on an existing db file/dir left by a previous
	// deployment that may have run as a different user (e.g. the old Python
	// image ran as root; this one runs as uid 65532).
	_ = os.Chmod(dir, 0o777)
	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		_ = os.Chmod(dbPath, 0o666)
	}

	conn, err := openDB()
	if err != nil {
		// Stale/foreign-owned file we can't write → back it up & start fresh.
		// Keeps old data accessible (renamed) while letting the server boot & write.
		log.Printf("initDB: cannot write existing %s (%v) — backing up & recreating", dbPath, err)
		stamp := time.Now().Format("20060102-150405")
		// Move the db file + any WAL/journal sidecars out of the way.
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			src := dbPath + suffix
			if _, statErr := os.Stat(src); statErr == nil {
				_ = os.Rename(src, src+".stale-"+stamp)
			}
		}
		conn, err = openDB()
		if err != nil {
			log.Fatalf("initDB: fresh open failed: %v", err)
		}
		log.Printf("initDB: started fresh DB (old files backed up with .stale-%s)", stamp)
	}
	db = conn

	// WAL mode is an optimization (better concurrent reads). It needs write
	// access; if it fails the DB is still usable in default rollback-journal
	// mode, so warn instead of fataling.
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		log.Printf("initDB: WAL mode unavailable (%v) — using default journal mode", err)
	}

	// Migrate: if an older schema (title/body columns) exists, drop & rebuild.
	// (openDB already ran CREATE TABLE IF NOT EXISTS with the current schema, so
	// the table exists by now — this only fires for legacy DBs from the Python era.)
	var cols []string
	rows, err := db.Query(`PRAGMA table_info(items);`)
	if err == nil {
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			_ = rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
			cols = append(cols, name)
		}
		rows.Close()
	}
	hasPayload := false
	for _, c := range cols {
		if c == "payload" {
			hasPayload = true
		}
	}
	if len(cols) > 0 && !hasPayload {
		if _, err := db.Exec(`DROP TABLE items;`); err != nil {
			log.Printf("initDB: drop old schema: %v", err)
		} else {
			// Recreate with current schema after dropping the legacy one.
			if _, err := db.Exec(`CREATE TABLE items (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				payload    TEXT    NOT NULL,
				scope      TEXT    NOT NULL DEFAULT 'public',
				created_at TEXT    NOT NULL DEFAULT (datetime('now'))
			);`); err != nil {
				log.Fatalf("initDB: recreate table: %v", err)
			}
			log.Println("[small-server] old schema detected — resetting items table")
		}
	}
}

func rowToItem(id int64, payload []byte, scope, createdAt string) ItemOut {
	// Guarantee valid JSON in the payload field — an empty/corrupt row becomes null.
	if len(payload) == 0 {
		payload = []byte("null")
	}
	return ItemOut{ID: id, Payload: payload, Scope: scope, CreatedAt: createdAt}
}

// listItems returns every item for the given scope, newest first.
func listItems(scope string) ([]ItemOut, error) {
	rows, err := db.Query(
		`SELECT id, payload, scope, created_at FROM items WHERE scope = ? ORDER BY id DESC;`,
		scope,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ItemOut
	for rows.Next() {
		var id int64
		var payload []byte
		var sc, ca string
		if err := rows.Scan(&id, &payload, &sc, &ca); err != nil {
			return nil, err
		}
		out = append(out, rowToItem(id, payload, sc, ca))
	}
	return out, rows.Err()
}

// createItem stores a raw JSON payload under the given scope and returns the new row.
func createItem(payload []byte, scope string) (ItemOut, error) {
	res, err := db.Exec(
		`INSERT INTO items (payload, scope) VALUES (?, ?);`,
		string(payload), scope,
	)
	if err != nil {
		return ItemOut{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ItemOut{}, err
	}
	var p []byte
	var sc, ca string
	err = db.QueryRow(
		`SELECT payload, scope, created_at FROM items WHERE id = ?;`, id,
	).Scan(&p, &sc, &ca)
	if err != nil {
		return ItemOut{}, err
	}
	return rowToItem(id, p, sc, ca), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

// requireAPIKey checks the X-API-Key header. If missing/wrong → 401.
func requireAPIKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-API-Key") != apiKey {
		writeError(w, http.StatusUnauthorized,
			"Invalid or missing API key. Send header 'X-API-Key'.")
		return false
	}
	return true
}

// readBody reads the request body as raw bytes. An empty body becomes "null"
// (matches the Python server's behavior).
func readBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil && err != io.EOF {
		return nil, err
	}
	// Trim a trailing newline so a curl `-d` with no `-H` still parses as the value.
	body = []byte(strings.TrimRight(string(body), "\n"))
	if len(body) == 0 {
		body = []byte("null")
	}
	return body, nil
}

// validateJSON returns nil if b is valid JSON, else an error.
func validateJSON(b []byte) error {
	var js any
	return json.Unmarshal(b, &js)
}

// ─────────────────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────────────────

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleUI(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.ReadFile("index.html")
	if err != nil {
		http.Error(w, "UI not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.ReadFile("openapi.json")
	if err != nil {
		http.Error(w, "schema not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// listItemHandler builds a list handler for a fixed scope.
func listItemHandler(scope string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		items, err := listItems(scope)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}
		if items == nil {
			items = []ItemOut{} // emit `[]` not `null`
		}
		writeJSON(w, http.StatusOK, items)
	}
}

// createItemHandler builds a create handler for a fixed scope.
func createItemHandler(scope string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := readBody(r)
		if err := validateJSON(body); err != nil {
			writeError(w, http.StatusBadRequest, "There was an error parsing the body")
			return
		}
		item, err := createItem(body, scope)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, item)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Routing — tiny hand-rolled mux (no gorilla/chi dep, keeps it stdlib-only)
// ─────────────────────────────────────────────────────────────────────────────

func routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/openapi.json", handleOpenAPI)
	mux.HandleFunc("/docs", handleUI) // no auto-Swagger in Go; alias to the UI
	mux.HandleFunc("/", handleUI)

	mux.HandleFunc("/public/items", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listItemHandler("public")(w, r)
		case http.MethodPost:
			createItemHandler("public")(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	})

	mux.HandleFunc("/secure/items", func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIKey(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			listItemHandler("secure")(w, r)
		case http.MethodPost:
			createItemHandler("secure")(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	})

	// Tiny logging middleware.
	logging := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(sw, r)
		log.Printf("%s %s %s → %d (%s)", r.RemoteAddr, r.Method, r.URL.Path, sw.status, time.Since(start))
	})

	// CORS middleware (outermost): lets browsers call this API from any origin.
	return corsMiddleware(logging)
}

// corsMiddleware adds permissive CORS headers and short-circ OPTIONS preflight.
// Allow-origin is "*" (any source). X-API-Key and Content-Type are exposed so
// secure-route preflights from the browser succeed.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
		h.Set("Access-Control-Max-Age", "86400") // cache preflight for a day

		// Preflight: respond immediately, don't touch the DB.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent) // 204
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter captures the response status code for logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	initDB()
	defer db.Close()

	addr := ":" + port
	log.Printf("[small-server] listening on http://0.0.0.0:%s  (db=%s)", port, dbPath)

	srv := &http.Server{
		Addr:              addr,
		Handler:           routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
