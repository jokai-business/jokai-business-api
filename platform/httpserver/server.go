package httpserver

import (
	"ai-business-api/internal/businessai"
	"ai-business-api/internal/customer"
	"ai-business-api/internal/dashboard"
	"ai-business-api/internal/document"
	"ai-business-api/internal/gmail"
	"ai-business-api/internal/imports"
	"ai-business-api/internal/invoice"
	"ai-business-api/internal/notification"
	"ai-business-api/internal/opportunity"
	"ai-business-api/internal/product"
	"ai-business-api/internal/project"
	"ai-business-api/internal/quote"
	"ai-business-api/internal/scope"
	"ai-business-api/internal/settings"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Checker interface{ Ping(context.Context) error }
type statusResponse struct {
	Status string `json:"status"`
}
type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}
type errorResponse struct {
	Error errorDetail `json:"error"`
}
type requestIDKey struct{}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	write(w, status, errorResponse{Error: errorDetail{Code: code, Message: message, RequestID: id}})
}

type Options struct {
	Imports       *imports.Service
	BusinessAI    *businessai.Service
	Gmail         *gmail.Service
	Settings      *settings.Service
	Notifications *notification.Service
	Dashboard     *dashboard.Service
	Documents     *document.Service
	Scopes        *scope.Service
	Invoices      *invoice.Service
	Projects      *project.Service
	Products      *product.Service
	Quotes        *quote.Service
	Opportunities *opportunity.Service
	Customers     *customer.Service
	Service       *user.Service
	Verifier      auth.Verifier
	Origin        string
}

func New(db Checker, logger *slog.Logger, options ...Options) http.Handler {
	mux := http.NewServeMux()
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	if option.Service != nil && option.Verifier != nil {
		b := business{option.Service, option.Verifier}
		b.register(mux)
		if option.Imports != nil {
			b.registerImports(mux, option.Imports)
		}
		if option.BusinessAI != nil {
			b.registerBusinessAI(mux, option.BusinessAI)
		}
		if option.Gmail != nil {
			b.registerGmail(mux, option.Gmail)
		}
		if option.Settings != nil {
			b.registerSettings(mux, option.Settings)
		}
		if option.Notifications != nil {
			b.registerNotifications(mux, option.Notifications)
		}
		if option.Dashboard != nil {
			b.registerDashboard(mux, option.Dashboard)
		}
		if option.Documents != nil {
			b.registerDocuments(mux, option.Documents)
		}
		if option.Scopes != nil {
			b.registerScopes(mux, option.Scopes)
		}
		if option.Invoices != nil {
			b.registerInvoices(mux, option.Invoices)
		}
		if option.Projects != nil {
			b.registerProjects(mux, option.Projects)
		}
		if option.Products != nil && option.Quotes != nil {
			b.registerQuotes(mux, option.Products, option.Quotes)
		}
		if option.Opportunities != nil {
			b.registerOpportunities(mux, option.Opportunities)
		}
		if option.Customers != nil {
			b.registerCustomers(mux, option.Customers)
		}
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			fail(w, r, 405, "METHOD_NOT_ALLOWED", "Method is not allowed")
			return
		}
		write(w, 200, statusResponse{Status: "ok"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			fail(w, r, 405, "METHOD_NOT_ALLOWED", "Method is not allowed")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			fail(w, r, 503, "NOT_READY", "Database is unavailable")
			return
		}
		write(w, 200, statusResponse{Status: "ready"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, r, 404, "NOT_FOUND", "Endpoint was not found") })
	limited := limit(mux)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			http.Error(w, "Service unavailable", 503)
			return
		}
		id := "req_" + hex.EncodeToString(b[:])
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		r = r.WithContext(context.WithValue(ctx, requestIDKey{}, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		rw := &recorder{ResponseWriter: w, status: 200}
		defer func() {
			if recover() != nil {
				fail(rw, r, 500, "INTERNAL_ERROR", "An unexpected error occurred")
			}
			route := "unmatched"
			if r.URL.Path == "/health" || r.URL.Path == "/ready" || r.URL.Path == "/api/v1/me" || r.URL.Path == "/api/v1/organization" || r.URL.Path == "/api/v1/users" || r.URL.Path == "/api/v1/audit-logs" {
				route = r.URL.Path
			}
			if r.Pattern != "" {
				route = r.Pattern
			}
			logger.Info("http_request", "requestId", id, "route", route, "method", r.Method, "status", rw.status, "durationMs", time.Since(start).Milliseconds())
		}()
		if origin := r.Header.Get("Origin"); origin != "" && origin != option.Origin {
			fail(rw, r, 403, "ORIGIN_DENIED", "Origin is not allowed")
			return
		}
		limited.ServeHTTP(rw, r)
	})
	return handler
}

type recorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (w *recorder) WriteHeader(s int) {
	if !w.written {
		w.status = s
		w.written = true
		w.ResponseWriter.WriteHeader(s)
	}
}
func (w *recorder) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
