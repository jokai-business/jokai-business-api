package settings

import (
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type healthFunc func(context.Context) error

func (f healthFunc) CheckHealth(ctx context.Context) error { return f(ctx) }
func (f healthFunc) Ping(ctx context.Context) error        { return f(ctx) }
func TestHealthPermissionsCacheAndSanitization(t *testing.T) {
	var calls atomic.Int32
	good := healthFunc(func(context.Context) error { calls.Add(1); return nil })
	bad := healthFunc(func(context.Context) error { return errors.New("password=secret host=private") })
	h := HealthMonitor{Database: good, Storage: bad, OCR: good}
	if _, err := h.Check(context.Background(), user.Context{}); !errors.Is(err, user.ErrForbidden) || calls.Load() != 0 {
		t.Fatal("unauthorized probe")
	}
	admin := user.Context{Permissions: []string{"settings.manage"}}
	v, err := h.Check(context.Background(), admin)
	if err != nil || v.Database != "reachable" || v.Storage != "unavailable" || v.OCR != "reachable" || v.KhmerOCR != "not_configured" || v.CheckedAt.IsZero() {
		t.Fatalf("%+v %v", v, err)
	}
	data, _ := json.Marshal(v)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") {
		t.Fatal("leaked diagnostic")
	}
	again, _ := h.Check(context.Background(), admin)
	if again != v || calls.Load() != 2 {
		t.Fatal("cache missed")
	}
	h.cached.CheckedAt = time.Now().Add(-16 * time.Second)
	_, _ = h.Check(context.Background(), admin)
	if calls.Load() != 4 {
		t.Fatal("cache did not expire")
	}
}
func TestHealthTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	h := HealthMonitor{OCR: healthFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })}
	v, err := h.Check(ctx, user.Context{Permissions: []string{"settings.manage"}})
	if err != nil || v.OCR != "unavailable" || v.Database != "not_checked" {
		t.Fatalf("%+v %v", v, err)
	}
}
