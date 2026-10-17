package httpserver

import (
	"ai-business-api/internal/dashboard"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

func (b business) registerDashboard(mux *http.ServeMux, s *dashboard.Service) {
	mux.HandleFunc("GET /api/v1/dashboard", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
}
