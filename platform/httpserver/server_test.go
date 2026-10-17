package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }
func TestHealthAndReadiness(t *testing.T) {
	for _, tc := range []struct {
		path, method string
		err          error
		status       int
	}{
		{"/health", "GET", errors.New("password=secret"), 200},
		{"/ready", "GET", nil, 200},
		{"/ready", "GET", errors.New("password=secret"), 503},
		{"/api/v1/me", "GET", nil, 404},
		{"/health", "POST", nil, 405},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			h := New(fakeDB{tc.err}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("X-Request-ID", "untrusted")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			id := w.Header().Get("X-Request-ID")
			if !strings.HasPrefix(id, "req_") {
				t.Fatal("request ID missing")
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("leaked internal error")
			}
			if w.Code >= 400 && !strings.Contains(w.Body.String(), id) {
				t.Fatal("error missing request ID")
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing security header")
			}
		})
	}
}

func TestRateLimitRetainsRequestEnvelope(t *testing.T) {
	h := New(fakeDB{}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for n := 0; n < 1000; n++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/unknown", nil))
		if w.Code == 429 {
			id := w.Header().Get("X-Request-ID")
			if !strings.HasPrefix(id, "req_") || !strings.Contains(w.Body.String(), id) || w.Header().Get("Retry-After") != "1" {
				t.Fatal("incomplete rate limit response")
			}
			return
		}
	}
	t.Fatal("requests were not rate limited")
}
