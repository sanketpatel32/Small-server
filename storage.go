package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const maxUploadBytes = 10 << 20

var (
	filenamePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	objectKeyPattern  = regexp.MustCompile(`^[a-f0-9]{32}-[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	r2EndpointPattern = regexp.MustCompile(`^[a-f0-9]{32}(\.(eu|us|fedramp))?\.r2\.cloudflarestorage\.com$`)
	bucketPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
	objectStorage     = loadR2Storage()
	// Bound buffered upload memory on the 512 MiB Fly machine.
	uploadSlots = make(chan struct{}, 4)
)

type r2Storage struct {
	endpoint, bucket, accessKey, secretKey string
	client                                 *http.Client
}

func loadR2Storage() *r2Storage {
	endpoint := strings.TrimRight(os.Getenv("R2_ENDPOINT"), "/")
	bucket := os.Getenv("R2_BUCKET")
	access, secret := os.Getenv("R2_ACCESS_KEY_ID"), os.Getenv("R2_SECRET_ACCESS_KEY")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || !r2EndpointPattern.MatchString(u.Host) || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || !bucketPattern.MatchString(bucket) || access == "" || secret == "" {
		return nil
	}
	return &r2Storage{endpoint: endpoint, bucket: bucket, accessKey: access, secretKey: secret,
		client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// signS3 signs the exact transmitted URI/query using AWS Signature V4 (R2 region: auto).
func signS3(r *http.Request, body []byte, access, secret, region string, now time.Time) {
	stamp, date := now.UTC().Format("20060102T150405Z"), now.UTC().Format("20060102")
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	r.Header.Set("X-Amz-Date", stamp)
	r.Header.Set("X-Amz-Content-Sha256", hash)
	r.URL.RawQuery = strings.ReplaceAll(r.URL.Query().Encode(), "+", "%20")
	const signedHeaders = "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + r.URL.Host + "\nx-amz-content-sha256:" + hash + "\nx-amz-date:" + stamp + "\n"
	canonical := r.Method + "\n" + r.URL.EscapedPath() + "\n" + r.URL.RawQuery + "\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + hash
	scope := date + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	key := []byte("AWS4" + secret)
	for _, part := range []string{date, region, "s3", "aws4_request"} {
		key = hmacSHA256(key, part)
	}
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+access+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+hex.EncodeToString(hmacSHA256(key, toSign)))
}

func hmacSHA256(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func (s *r2Storage) request(ctx context.Context, method, key string, query url.Values, body []byte, contentType string) (*http.Response, error) {
	path := s.endpoint + "/" + s.bucket
	if key != "" {
		path += "/uploads/" + key
	}
	r, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.URL.RawQuery = query.Encode()
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	signS3(r, body, s.accessKey, s.secretKey, "auto", time.Now())
	return s.client.Do(r)
}

func registerUploads(mux *http.ServeMux, storage *r2Storage) {
	mux.HandleFunc("GET /secure/uploads/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !requireAPIKey(w, r) {
			return
		}
		writeJSON(w, 200, map[string]any{"configured": storage != nil, "max_bytes": maxUploadBytes})
	})
	for _, pattern := range []string{"GET /secure/uploads", "POST /secure/uploads", "GET /secure/uploads/{key}", "DELETE /secure/uploads/{key}"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !requireAPIKey(w, r) {
				return
			}
			if storage == nil {
				writeError(w, 503, "R2 storage is not configured.")
				return
			}
			key := r.PathValue("key")
			if key != "" && !objectKeyPattern.MatchString(key) {
				writeError(w, 400, "Invalid upload key.")
				return
			}
			if key != "" {
				storage.handleObject(w, r, key)
			} else if r.Method == http.MethodPost {
				storage.handleUpload(w, r)
			} else {
				storage.handleList(w, r)
			}
		})
	}
}

func storageFailure(w http.ResponseWriter, res *http.Response, err error, missingFile bool) bool {
	if err != nil {
		writeError(w, 502, "Storage request failed.")
		return true
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == 404 && missingFile {
			writeError(w, 404, "Upload not found.")
		} else {
			writeError(w, 502, "Storage request failed.")
		}
		return true
	}
	return false
}

func (s *r2Storage) handleUpload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("filename")
	if !filenamePattern.MatchString(name) {
		writeError(w, 400, "Filename must start with a letter or digit and contain only letters, digits, dots, underscores, or hyphens (max 128 characters).")
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || len(contentType) > 256 || strings.HasPrefix(mediaType, "multipart/") {
		writeError(w, 400, "Send raw file bytes with a valid Content-Type, not multipart data.")
		return
	}
	select {
	case uploadSlots <- struct{}{}:
		defer func() { <-uploadSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, 429, "Too many concurrent uploads. Retry shortly.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxUploadBytes))
	if err != nil {
		if _, ok := err.(*http.MaxBytesError); ok {
			writeError(w, 413, "Upload exceeds 10 MiB.")
		} else {
			writeError(w, 400, "Could not read upload body.")
		}
		return
	}
	if len(body) == 0 {
		writeError(w, 400, "Upload body must not be empty.")
		return
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	key := hex.EncodeToString(id[:]) + "-" + name
	res, err := s.request(r.Context(), http.MethodPut, key, nil, body, contentType)
	if res != nil {
		defer res.Body.Close()
	}
	if storageFailure(w, res, err, false) {
		return
	}
	w.Header().Set("Location", "/secure/uploads/"+key)
	writeJSON(w, 201, map[string]any{"key": key, "filename": name, "size": len(body), "content_type": contentType, "download_path": "/secure/uploads/" + key})
}

func (s *r2Storage) handleObject(w http.ResponseWriter, r *http.Request, key string) {
	res, err := s.request(r.Context(), r.Method, key, nil, nil, "")
	if res != nil {
		defer res.Body.Close()
	}
	if storageFailure(w, res, err, r.Method == http.MethodGet || r.Method == http.MethodHead) {
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(204)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": key[33:]}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	for _, name := range []string{"Content-Length", "ETag", "Last-Modified"} {
		if value := res.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, res.Body)
	}
}

type storedFile struct {
	Key          string `xml:"Key" json:"key"`
	Size         int64  `xml:"Size" json:"size"`
	ETag         string `xml:"ETag" json:"etag"`
	LastModified string `xml:"LastModified" json:"last_modified"`
}

func (s *r2Storage) handleList(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 2048 {
		writeError(w, 400, "Cursor exceeds 2048 bytes.")
		return
	}
	query := url.Values{"list-type": {"2"}, "prefix": {"uploads/"}, "max-keys": {"100"}}
	if cursor != "" {
		query.Set("continuation-token", cursor)
	}
	res, err := s.request(r.Context(), http.MethodGet, "", query, nil, "")
	if res != nil {
		defer res.Body.Close()
	}
	if storageFailure(w, res, err, false) {
		return
	}
	var page struct {
		Files      []storedFile `xml:"Contents"`
		NextCursor string       `xml:"NextContinuationToken"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&page); err != nil {
		writeError(w, 502, "Invalid storage response.")
		return
	}
	files := []storedFile{}
	for _, file := range page.Files {
		if !strings.HasPrefix(file.Key, "uploads/") {
			continue
		}
		file.Key = strings.TrimPrefix(file.Key, "uploads/")
		if objectKeyPattern.MatchString(file.Key) {
			files = append(files, file)
		}
	}
	writeJSON(w, 200, map[string]any{"files": files, "next_cursor": page.NextCursor})
}
