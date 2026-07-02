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

func initDB() {
	// Make sure the parent dir exists (Fly volume starts empty, local dir is fine).
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("initDB: mkdir parent: %v", err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("initDB: open: %v", err)
	}
	db = conn

	// SQLite handles concurrent reads + serialized writes fine for this workload.
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		log.Fatalf("initDB: pragma: %v", err)
	}

	// Migrate: if an older schema (title/body columns) exists, drop & rebuild.
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
			log.Printf("initDB: drop old: %v", err)
		}
		log.Println("[small-server] old schema detected — resetting items table")
	}

	const create = `
	CREATE TABLE IF NOT EXISTS items (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		payload    TEXT    NOT NULL,                      -- raw JSON of whatever was POSTed
		scope      TEXT    NOT NULL DEFAULT 'public',     -- 'public' | 'secure'
		created_at TEXT    NOT NULL DEFAULT (datetime('now'))
	);`
	if _, err := db.Exec(create); err != nil {
		log.Fatalf("initDB: create table: %v", err)
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(sw, r)
		log.Printf("%s %s %s → %d (%s)", r.RemoteAddr, r.Method, r.URL.Path, sw.status, time.Since(start))
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
