package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	oldDB, oldPath := db, dbPath
	dbPath = filepath.Join(t.TempDir(), "test.db")
	var err error
	db, err = openDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); db, dbPath = oldDB, oldPath })
	return routes()
}

func request(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLab(t *testing.T) {
	h := testServer(t)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/lab/status/429", 429}, {"/lab/status/500", 500},
		{"/lab/status/204", 204}, {"/lab/status/205", 205}, {"/lab/status/304", 304},
		{"/lab/status/199", 400}, {"/lab/status/600", 400}, {"/lab/status/nope", 400},
		{"/lab/delay/0", 200}, {"/lab/delay/-1", 400}, {"/lab/delay/10001", 400},
		{"/lab/delay/nope", 400}, {"/lab/redirect", 302},
		{"/lab/redirect?to=https://example.com", 400}, {"/lab/redirect?to=//example.com", 400},
		{"/lab/redirect?to=/%5cexample.com", 400}, {"/lab/redirect?code=200", 400},
		{"/lab/redirect?code=bad", 400}, {"/lab/redirect?code=307&to=/health", 307},
		{"/does-not-exist", 404}, {"/docs", 200}, {"/", 200},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := request(h, "GET", tc.path, "", nil)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == 204 || tc.want == 205 || tc.want == 304 {
				if w.Body.Len() != 0 {
					t.Fatal("body must be empty")
				}
			}
		})
	}
	w := request(h, "GET", "/lab/status/429", "", nil)
	if w.Header().Get("Retry-After") != "5" {
		t.Fatal("missing Retry-After")
	}
	w = request(h, "GET", "/lab/redirect?to=/health&code=308", "", nil)
	if w.Code != 308 || w.Header().Get("Location") != "/health" {
		t.Fatal("incorrect redirect")
	}
	if request(h, "POST", "/lab/delay/0", "", nil).Code != 405 {
		t.Fatal("unsupported method must return 405")
	}
}

func TestEcho(t *testing.T) {
	h := testServer(t)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		w := request(h, method, "/lab/echo?tag=one&tag=two", "{\"large\":9007199254740993}", map[string]string{
			"Authorization": "private", "X-API-Key": "private", "Cookie": "private", "Proxy-Authorization": "private", "X-Test-Id": "demo",
		})
		var echo struct {
			Method  string              `json:"method"`
			Query   map[string][]string `json:"query"`
			Headers http.Header         `json:"headers"`
			Body    string              `json:"body"`
			JSON    json.RawMessage     `json:"json"`
			Bytes   int                 `json:"bytes"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &echo) != nil {
			t.Fatal(w.Body.String())
		}
		if echo.Method != method || len(echo.Query["tag"]) != 2 || echo.Bytes != len(echo.Body) || string(echo.JSON) != echo.Body {
			t.Fatalf("incorrect echo: %s", w.Body.String())
		}
		for _, key := range []string{"Authorization", "X-API-Key", "Cookie", "Proxy-Authorization"} {
			if echo.Headers.Get(key) != "[redacted]" {
				t.Fatalf("%s was not redacted", key)
			}
		}
		if echo.Headers.Get("X-Test-Id") != "demo" {
			t.Fatal("custom header lost")
		}
	}
	for _, body := range []string{"plain text", "{broken", "", "null"} {
		w := request(h, "POST", "/lab/echo", body, nil)
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatal("raw echo must accept any body")
		}
	}
	if request(h, "POST", "/lab/echo", strings.Repeat("x", labBodyLimit+1), nil).Code != 413 {
		t.Fatal("oversized body must be rejected")
	}
}

func TestCacheAndCORS(t *testing.T) {
	h := testServer(t)
	w := request(h, "GET", "/lab/cache", "", nil)
	if w.Code != 200 || w.Header().Get("ETag") != `"small-server-v1"` || w.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatal("missing cache metadata")
	}
	for _, tag := range []string{`"small-server-v1"`, `W/"small-server-v1"`, `"other", "small-server-v1"`, "*"} {
		w = request(h, "GET", "/lab/cache", "", map[string]string{"If-None-Match": tag})
		if w.Code != 304 || w.Body.Len() != 0 {
			t.Fatalf("conditional response for %s: %d", tag, w.Code)
		}
	}
	if request(h, "GET", "/lab/cache", "", map[string]string{"If-None-Match": `"other"`}).Code != 200 {
		t.Fatal("nonmatching cache must return 200")
	}
	w = request(h, "OPTIONS", "/lab/echo", "", nil)
	if w.Code != 204 || !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PATCH") || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "If-None-Match") {
		t.Fatal("incomplete CORS preflight")
	}
}

func TestDelayCancellationAndHEAD(t *testing.T) {
	h := testServer(t)
	start := time.Now()
	if request(h, "GET", "/lab/delay/30", "", nil).Code != 200 || time.Since(start) < 30*time.Millisecond {
		t.Fatal("delay did not wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/lab/delay/10000", nil).WithContext(ctx)
	start = time.Now()
	h.ServeHTTP(httptest.NewRecorder(), r)
	if time.Since(start) > time.Second {
		t.Fatal("cancelled delay did not stop")
	}
	server := httptest.NewServer(h)
	defer server.Close()
	for _, path := range []string{"/lab/echo", "/lab/status/500", "/lab/cache", "/lab/delay/0"} {
		r, err := http.Head(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode == 405 {
			t.Fatal("HEAD unsupported")
		}
		var buf [1]byte
		n, _ := r.Body.Read(buf[:])
		r.Body.Close()
		if n != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
}

func TestStorageScopeAndPayloads(t *testing.T) {
	h := testServer(t)
	key := map[string]string{"X-API-Key": apiKey}
	for _, payload := range []string{`null`, `false`, `42`, `"text"`, `[1,2]`, `{"nested":{"ok":true}}`, ""} {
		w := request(h, "POST", "/public/items", payload, nil)
		if w.Code != 201 {
			t.Fatalf("payload %q: %s", payload, w.Body.String())
		}
	}
	w := request(h, "POST", "/secure/items", `{"secret":true}`, key)
	var item ItemOut
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &item) != nil {
		t.Fatal(w.Body.String())
	}
	id := strconv.FormatInt(item.ID, 10)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if request(h, method, "/public/items/"+id, `{"changed":true}`, nil).Code != 404 {
			t.Fatalf("%s crossed scopes", method)
		}
	}
	if request(h, "GET", "/secure/items/"+id, "", key).Code != 200 {
		t.Fatal("cross-scope mutation changed secure item")
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if request(h, method, "/secure/items/1", `null`, key).Code != 404 {
			t.Fatalf("%s crossed from secure to public", method)
		}
	}
	if request(h, "GET", "/public/items/1", "", nil).Code != 200 {
		t.Fatal("cross-scope mutation changed public item")
	}
	if request(h, "GET", "/secure/items", "", nil).Code != 401 || request(h, "POST", "/public/items", "{broken", nil).Code != 400 {
		t.Fatal("auth or JSON regression")
	}
	if request(h, "PUT", "/secure/items/"+id, "null", key).Code != 200 || request(h, "DELETE", "/secure/items/"+id, "", key).Code != 200 || request(h, "GET", "/secure/items/"+id, "", key).Code != 404 {
		t.Fatal("secure CRUD regression")
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (brokenBody) Close() error             { return nil }

func TestStorageReadError(t *testing.T) {
	h := testServer(t)
	request(h, "POST", "/public/items", `{"original":true}`, nil)
	for _, tc := range []struct{ method, path string }{{"POST", "/public/items"}, {"PUT", "/public/items/1"}} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Body = brokenBody{}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("read error returned %d", w.Code)
		}
	}
	w := request(h, "GET", "/public/items/1", "", nil)
	if !strings.Contains(w.Body.String(), `"original":true`) {
		t.Fatal("failed read mutated the item")
	}
}
