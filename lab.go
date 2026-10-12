package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const labBodyLimit = 1 << 20

func registerLab(mux *http.ServeMux) {
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(method+" /lab/echo", handleEcho)
	}
	mux.HandleFunc("GET /lab/status/{code}", handleStatus)
	mux.HandleFunc("GET /lab/delay/{ms}", handleDelay)
	mux.HandleFunc("GET /lab/redirect", handleRedirect)
	mux.HandleFunc("GET /lab/cache", handleCache)
}

func handleEcho(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, labBodyLimit))
	if err != nil {
		if _, ok := err.(*http.MaxBytesError); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "Echo body exceeds 1 MiB.")
		} else {
			writeError(w, http.StatusBadRequest, "Could not read request body.")
		}
		return
	}
	headers := r.Header.Clone()
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-API-Key"} {
		if headers.Get(key) != "" {
			headers.Set(key, "[redacted]")
		}
	}
	var parsed json.RawMessage
	if json.Valid(body) {
		parsed = body
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method": r.Method, "path": r.URL.Path, "query": r.URL.Query(),
		"headers": headers, "body": string(body), "json": parsed, "bytes": len(body),
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	code, err := strconv.Atoi(r.PathValue("code"))
	if err != nil || code < 200 || code > 599 {
		writeError(w, http.StatusBadRequest, "Status must be an integer from 200 to 599.")
		return
	}
	if code == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "5")
	}
	if code == 204 || code == 205 || code == 304 {
		w.WriteHeader(code)
		return
	}
	writeJSON(w, code, map[string]any{"status": code, "message": http.StatusText(code)})
}

func handleDelay(w http.ResponseWriter, r *http.Request) {
	ms, err := strconv.Atoi(r.PathValue("ms"))
	if err != nil || ms < 0 || ms > 10000 {
		writeError(w, http.StatusBadRequest, "Delay must be an integer from 0 to 10000 milliseconds.")
		return
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		return
	case <-timer.C:
		writeJSON(w, http.StatusOK, map[string]any{"delay_ms": ms, "status": "ok"})
	}
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("to")
	if target == "" {
		target = "/lab/echo"
	}
	u, err := url.Parse(target)
	if err != nil || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.ContainsAny(target, "\\\r\n") || u.Host != "" || u.Scheme != "" {
		writeError(w, http.StatusBadRequest, "Redirect target must be a local absolute path.")
		return
	}
	code := 302
	if value, ok := r.URL.Query()["code"]; ok {
		code, err = strconv.Atoi(value[0])
	}
	if err != nil || (code != 301 && code != 302 && code != 303 && code != 307 && code != 308) {
		writeError(w, http.StatusBadRequest, "Redirect code must be 301, 302, 303, 307, or 308.")
		return
	}
	http.Redirect(w, r, target, code)
}

func handleCache(w http.ResponseWriter, r *http.Request) {
	const etag = `"small-server-v1"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=60")
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "Cached response", "version": 1})
}
