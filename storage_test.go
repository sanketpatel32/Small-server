package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestS3SignatureAWSReference(t *testing.T) {
	// AWS's published S3 ListObjects vector, not a signature computed by this test.
	// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sig-v4-header-based-auth.html
	r, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/?prefix=J&max-keys=2", nil)
	signS3(r, nil, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	if !strings.HasSuffix(r.Header.Get("Authorization"), "Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7") {
		t.Fatal("signature did not match the AWS reference vector")
	}
}

func TestR2Configuration(t *testing.T) {
	for _, key := range []string{"R2_ENDPOINT", "R2_BUCKET", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY"} {
		t.Setenv(key, "")
	}
	if loadR2Storage() != nil {
		t.Fatal("missing config accepted")
	}
	t.Setenv("R2_BUCKET", "test-bucket")
	t.Setenv("R2_ACCESS_KEY_ID", "test-access")
	t.Setenv("R2_SECRET_ACCESS_KEY", "test-secret")
	for _, endpoint := range []string{"https://example.com", "http://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com", "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com/path", "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com?x=1"} {
		t.Setenv("R2_ENDPOINT", endpoint)
		if loadR2Storage() != nil {
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
	t.Setenv("R2_ENDPOINT", "https://0123456789abcdef0123456789abcdef.eu.r2.cloudflarestorage.com/")
	s := loadR2Storage()
	if s == nil || s.client.Timeout != 30*time.Second || s.client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("invalid R2 client configuration")
	}
}

func TestUploadsAuthAndDisabled(t *testing.T) {
	mux := http.NewServeMux()
	registerUploads(mux, nil)
	for _, path := range []string{"/secure/uploads/status", "/secure/uploads", "/secure/uploads/file"} {
		if request(mux, "GET", path, "", nil).Code != 401 {
			t.Fatal("unauthenticated request accepted")
		}
	}
	key := map[string]string{"X-API-Key": apiKey}
	if request(mux, "GET", "/secure/uploads", "", key).Code != 503 {
		t.Fatal("missing storage must return503")
	}
	w := request(mux, "GET", "/secure/uploads/status", "", key)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) || strings.Contains(w.Body.String(), apiKey) {
		t.Fatal("status response incorrect")
	}
}

func TestUploadCapacityAndBodyReadErrors(t *testing.T) {
	mux := http.NewServeMux()
	registerUploads(mux, &r2Storage{})
	auth := map[string]string{"X-API-Key": apiKey}
	for i := 0; i < cap(uploadSlots); i++ {
		uploadSlots <- struct{}{}
	}
	w := request(mux, "POST", "/secure/uploads?filename=file.txt", "hello", auth)
	for i := 0; i < cap(uploadSlots); i++ {
		<-uploadSlots
	}
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("concurrent upload cap is not enforced")
	}
	r := httptest.NewRequest("POST", "/secure/uploads?filename=file.txt", nil)
	r.Header.Set("X-API-Key", apiKey)
	r.Body = brokenBody{}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 400 || len(uploadSlots) != 0 {
		t.Fatal("failed read leaked upload slot or returned wrong status")
	}
	if request(mux, "POST", "/secure/uploads?filename=file.txt", "x", map[string]string{"X-API-Key": apiKey, "Content-Type": "multipart/form-data; boundary=x"}).Code != 400 {
		t.Fatal("multipart accepted")
	}
}

func TestUploadRoundTripAndFailures(t *testing.T) {
	objects := map[string][]byte{}
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(r.Header.Get("Authorization"), "/auto/s3/aws4_request") || r.Header.Get("X-Amz-Content-Sha256") != fmt.Sprintf("%x", sha256.Sum256(body)) || r.Header.Get("X-API-Key") != "" {
			t.Error("invalid signing or credential forwarding")
		}
		if r.URL.Path == "/test-bucket" {
			if r.URL.Query().Get("prefix") != "uploads/" || r.URL.Query().Get("max-keys") != "100" || r.URL.Query().Get("list-type") != "2" {
				t.Error("list scope lost")
			}
			if r.URL.Query().Get("continuation-token") == "broken" {
				_, _ = w.Write([]byte("invalid XML"))
				return
			}
			_, _ = w.Write([]byte("<ListBucketResult><NextContinuationToken>a+b/=</NextContinuationToken>"))
			for key, body := range objects {
				_, _ = fmt.Fprintf(w, "<Contents><Key>%s</Key><Size>%d</Size><ETag>test</ETag><LastModified>2026-10-12T00:00:00Z</LastModified></Contents>", key, len(body))
			}
			_, _ = w.Write([]byte("<Contents><Key>outside.txt</Key><Size>1</Size></Contents></ListBucketResult>"))
			return
		}
		if strings.HasSuffix(r.URL.Path, "-failed.txt") {
			http.Error(w, "private upstream credentials", 403)
			return
		}
		switch r.Method {
		case "PUT":
			objects[r.URL.Path[len("/test-bucket/"):]] = body
			w.WriteHeader(200)
		case "DELETE":
			delete(objects, r.URL.Path[len("/test-bucket/"):])
			w.WriteHeader(204)
		case "GET", "HEAD":
			data, ok := objects[r.URL.Path[len("/test-bucket/"):]]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			if r.Method == "GET" {
				_, _ = w.Write(data)
			}
		}
	}))
	defer upstream.Close()
	s := &r2Storage{endpoint: upstream.URL, bucket: "test-bucket", accessKey: "test-access", secretKey: "test-secret", client: upstream.Client()}
	mux := http.NewServeMux()
	registerUploads(mux, s)
	auth := map[string]string{"X-API-Key": apiKey, "Content-Type": "text/plain"}
	w := request(mux, "POST", "/secure/uploads?filename=hello.txt", "hello R2", auth)
	var uploaded struct {
		Key string `json:"key"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &uploaded) != nil || !objectKeyPattern.MatchString(uploaded.Key) {
		t.Fatalf("upload failed: %s", w.Body.String())
	}
	path := "/secure/uploads/" + uploaded.Key
	w = request(mux, "GET", path, "", auth)
	if w.Code != 200 || w.Body.String() != "hello R2" || w.Header().Get("Content-Type") != "application/octet-stream" || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("unsafe or incorrect download")
	}
	if request(mux, "HEAD", path, "", auth).Body.Len() != 0 {
		t.Fatal("HEAD has a body")
	}
	w = request(mux, "GET", "/secure/uploads?cursor=a%2Bb%2F%3D", "", auth)
	if w.Code != 200 || !strings.Contains(w.Body.String(), uploaded.Key) || strings.Contains(w.Body.String(), "outside.txt") {
		t.Fatal("list scope or XML conversion failed")
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/secure/uploads?filename=../file", "x", 400},
		{"POST", "/secure/uploads?filename=file.txt", "", 400},
		{"POST", "/secure/uploads?filename=file.txt", strings.Repeat("x", maxUploadBytes+1), 413},
		{"GET", "/secure/uploads/invalid", "", 400},
		{"GET", "/secure/uploads?cursor=" + strings.Repeat("x", 2049), "", 400},
		{"GET", "/secure/uploads?cursor=broken", "", 502},
		{"GET", "/secure/uploads/0123456789abcdef0123456789abcdef-missing.txt", "", 404},
		{"GET", "/secure/uploads/0123456789abcdef0123456789abcdef-failed.txt", "", 502},
	} {
		w := request(mux, tc.method, tc.path, tc.body, auth)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "private upstream") {
			t.Fatalf("%s: status%d, wanted%d", tc.path, w.Code, tc.status)
		}
	}
	before := requests
	if request(mux, "POST", "/secure/uploads?filename=x.txt", "x", nil).Code != 401 || requests != before {
		t.Fatal("auth failure reached R2")
	}
	if request(mux, "DELETE", path, "", auth).Code != 204 || request(mux, "GET", path, "", auth).Code != 404 || request(mux, "DELETE", path, "", auth).Code != 204 {
		t.Fatal("delete is not idempotent")
	}
}
