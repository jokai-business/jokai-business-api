package paddleocr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServingProtocol(t *testing.T) {
	for _, typ := range []string{"application/pdf", "image/png"} {
		t.Run(typ, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in map[string]any
				if json.NewDecoder(r.Body).Decode(&in) != nil {
					t.Error("JSON")
				}
				want := float64(1)
				if typ == "application/pdf" {
					want = 0
				}
				if in["file"] != base64.StdEncoding.EncodeToString([]byte("private source")) || in["fileType"] != want || r.Method != "POST" {
					t.Error("unexpected request")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"errorCode":0,"result":{"ocrResults":[{"prunedResult":{"rec_texts":["Invoice 123","Total 42.00"]}}]}}`))
			}))
			defer server.Close()
			c, _ := New(server.URL + "/ocr")
			v, err := c.Extract(context.Background(), []byte("private source"), typ)
			if err != nil || v.Pages != 1 || v.Text != "Invoice 123\nTotal 42.00" {
				t.Fatalf("result: %+v %v", v, err)
			}
		})
	}
}
func TestRejectsInvalidResults(t *testing.T) {
	for _, body := range []string{`{"errorCode":0,"result":{"ocrResults":[{"prunedResult":{"rec_texts":[]}},{"prunedResult":{"rec_texts":[]}}]}}`, `{}`, `{"errorCode":500}`, `{"errorCode":0,"result":{"ocrResults":[]}}`, `{"errorCode":0,"result":{"ocrResults":[{"prunedResult":{"rec_texts":[]}}]}}`, `{"errorCode":0,"result":{"dataInfo":{"numPages":11},"ocrResults":[{"prunedResult":{"rec_texts":["partial"]}}]}}`, strings.Repeat("x", (16<<20)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		c, _ := New(server.URL)
		if _, err := c.Extract(context.Background(), []byte("x"), "application/pdf"); err == nil {
			t.Error("accepted invalid output")
		}
		server.Close()
	}
}
func TestNoRedirectAndConfig(t *testing.T) {
	for _, s := range []string{"file:///etc/passwd", "https://a:b@host/ocr", "http://host/ocr?token=secret", "/ocr"} {
		if _, err := New(s); err == nil {
			t.Error("accepted invalid config")
		}
	}
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	c, _ := New(redirect.URL)
	if _, err := c.Extract(context.Background(), []byte("private"), "image/png"); err == nil || called {
		t.Fatal("followed redirect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Extract(ctx, []byte("x"), "image/png"); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestHealthProtocol(t *testing.T) {
	for _, body := range []string{`{"status":"ready"}`, `{"status":"ok"}`, `{}`, strings.Repeat("x", 1025)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/health" {
				t.Error("wrong health request")
			}
			w.Write([]byte(body))
		}))
		c, _ := New(server.URL + "/ocr")
		err := c.CheckHealth(context.Background())
		server.Close()
		if (err == nil) != (body == `{"status":"ready"}`) {
			t.Fatalf("accepted incorrect status %q: %v", body, err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", 302) }))
	defer server.Close()
	c, _ := New(server.URL + "/ocr")
	if c.CheckHealth(context.Background()) == nil {
		t.Fatal("followed redirect")
	}
}
