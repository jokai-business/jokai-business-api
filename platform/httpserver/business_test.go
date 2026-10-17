package httpserver

import (
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

type testVerifier struct{}

func (testVerifier) Verify(_ context.Context, token string) (auth.Identity, error) {
	if token != "good" {
		return auth.Identity{}, errors.New("invalid")
	}
	return auth.Identity{Subject: "subject", SessionID: "session"}, nil
}

type testStore struct {
	permissions []string
	created     bool
}

func (s *testStore) Resolve(context.Context, string) (user.Context, error) {
	return user.Context{User: user.User{ID: "actor"}, Organization: user.Organization{ID: "trusted"}, Permissions: s.permissions}, nil
}
func (s *testStore) List(_ context.Context, org string, _, _ int) ([]user.User, int, error) {
	if org != "trusted" {
		panic("untrusted tenant")
	}
	return []user.User{}, 0, nil
}
func (s *testStore) Create(context.Context, user.Context, user.CreateRequest, string) (user.User, error) {
	s.created = true
	return user.User{}, nil
}
func (s *testStore) Login(context.Context, user.Context, string, string) error { return nil }
func (s *testStore) Audit(context.Context, string, int, int) ([]user.Audit, int, error) {
	return []user.Audit{}, 0, nil
}
func TestProtectedRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, token, body, origin string
		permissions                             []string
		status                                  int
	}{
		{name: "missing token", method: "GET", path: "/api/v1/me", status: 401},
		{name: "invalid token", method: "GET", path: "/api/v1/me", token: "bad", status: 401},
		{name: "me", method: "GET", path: "/api/v1/me", token: "good", status: 200},
		{name: "RBAC", method: "GET", path: "/api/v1/users", token: "good", status: 403},
		{name: "tenant header ignored", method: "GET", path: "/api/v1/users", token: "good", permissions: []string{"user.manage"}, status: 200},
		{name: "tenant injection rejected", method: "POST", path: "/api/v1/users", token: "good", permissions: []string{"user.manage"}, body: `{"organization_id":"evil"}`, status: 400},
		{name: "bad page", method: "GET", path: "/api/v1/users?page=-1", token: "good", permissions: []string{"user.manage"}, status: 400},
		{name: "origin rejected", method: "GET", path: "/api/v1/me", token: "good", origin: "https://evil.example", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testStore{permissions: tc.permissions}
			h := New(fakeDB{}, slog.New(slog.NewJSONHandler(io.Discard, nil)), Options{Service: &user.Service{Store: store}, Verifier: testVerifier{}, Origin: "https://app.example"})
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Organization-ID", "evil")
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}
