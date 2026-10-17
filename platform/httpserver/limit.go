package httpserver

import (
	"golang.org/x/time/rate"
	"net/http"
)

// One API instance, bounded memory, no untrusted proxy-header keying.
// A global bucket deliberately limits total load; per-user policy can be added with CRM.
func limit(next http.Handler) http.Handler {
	limiter := rate.NewLimiter(100, 200)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && r.URL.Path != "/ready" && !limiter.Allow() {
			w.Header().Set("Retry-After", "1")
			fail(w, r, 429, "RATE_LIMITED", "Please retry shortly")
			return
		}
		next.ServeHTTP(w, r)
	})
}
