package documentstorage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func settings() map[string]string {
	return map[string]string{"DOCUMENT_STORAGE_PATH": "/private/files", "DOCUMENT_STORAGE_BACKEND": "s3", "DOCUMENT_S3_BUCKET": "private-documents", "DOCUMENT_S3_REGION": "us-east-1", "DOCUMENT_S3_ENDPOINT": "https://s3.example.test", "DOCUMENT_S3_ACCESS_KEY_ID": "test-key", "DOCUMENT_S3_SECRET_ACCESS_KEY": "test-secret"}
}
func read(v map[string]string) (Settings, error) { return Read(func(k string) string { return v[k] }) }
func TestConfiguration(t *testing.T) {
	base, err := read(settings())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{{"DOCUMENT_STORAGE_BACKEND", "unknown"}, {"DOCUMENT_STORAGE_PATH", "relative"}, {"DOCUMENT_S3_BUCKET", "bad/name"}, {"DOCUMENT_S3_REGION", ""}, {"DOCUMENT_S3_SECRET_ACCESS_KEY", ""}, {"DOCUMENT_S3_ENDPOINT", "http://localhost:9000"}, {"DOCUMENT_S3_ENDPOINT", "https://user:secret@example.test"}, {"DOCUMENT_S3_ENDPOINT", "https://example.test/bucket"}, {"DOCUMENT_S3_ENDPOINT", "https://example.test?token=secret"}, {"DOCUMENT_S3_PATH_STYLE", "maybe"}} {
		v := settings()
		v[tc.key] = tc.value
		if _, err = read(v); err == nil {
			t.Fatalf("accepted %s", tc.key)
		}
	}
	v := settings()
	v["DOCUMENT_S3_SECRET_ACCESS_KEY"] = "rotated"
	rotated, _ := read(v)
	if base.ID() != rotated.ID() {
		t.Fatal("credential rotation changed location")
	}
	v["DOCUMENT_S3_BUCKET"] = "different-bucket"
	changed, _ := read(v)
	if base.ID() == changed.ID() {
		t.Fatal("different bucket reused location")
	}
	v = settings()
	v["DOCUMENT_S3_ENDPOINT"] = "https://s3.example.test/"
	slash, _ := read(v)
	if slash.ID() != base.ID() {
		t.Fatal("trailing slash changed identity")
	}
	v = settings()
	v["DOCUMENT_S3_ENDPOINT"] = "http://localhost:9000"
	v["DOCUMENT_S3_ALLOW_HTTP"] = "true"
	if _, err = read(v); err != nil {
		t.Fatal(err)
	}
}
func TestS3WireContract(t *testing.T) {
	var original []byte
	const key = "/private-documents/documents/10000000-0000-0000-0000-000000000001/20000000-0000-0000-0000-000000000001"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("request not signed")
		}
		if r.Method == "HEAD" && r.URL.Path == "/private-documents" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path != key {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case "PUT":
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("missing immutable-write condition")
			}
			if original != nil {
				w.WriteHeader(412)
				return
			}
			original, _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", "\"example\"")
			w.WriteHeader(200)
		case "GET":
			w.Write(original)
		default:
			w.WriteHeader(405)
		}
	}))
	defer srv.Close()
	v := settings()
	v["DOCUMENT_S3_ENDPOINT"] = srv.URL
	v["DOCUMENT_S3_ALLOW_HTTP"] = "true"
	v["DOCUMENT_S3_PATH_STYLE"] = "true"
	c, err := read(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_ENDPOINT_URL_S3", "http://127.0.0.1:1")
	store, err := Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	org, id := "10000000-0000-0000-0000-000000000001", "20000000-0000-0000-0000-000000000001"
	data := []byte("immutable source")
	if err = store.Put(context.Background(), org, id, data); err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), org, id, []byte("overwrite")); err == nil {
		t.Fatal("overwrote original")
	}
	got, err := store.Get(context.Background(), org, id)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("download mismatch: %v", err)
	}
	if _, err = store.Get(context.Background(), org, "../secret"); err == nil {
		t.Fatal("accepted invalid key")
	}
}
func TestBucketFailureIsSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	v := settings()
	v["DOCUMENT_S3_ENDPOINT"] = srv.URL
	v["DOCUMENT_S3_ALLOW_HTTP"] = "true"
	v["DOCUMENT_S3_PATH_STYLE"] = "true"
	c, _ := read(v)
	_, err := Open(context.Background(), c)
	if err == nil || strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), "test-secret") {
		t.Fatal("unsafe or absent error")
	}
}
