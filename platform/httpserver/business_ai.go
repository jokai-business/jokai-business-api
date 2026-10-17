package httpserver

import (
	"ai-business-api/internal/businessai"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

func (b business) registerBusinessAI(mux *http.ServeMux, s *businessai.Service) {
	mux.HandleFunc("GET /api/v1/ai/summaries/{kind}/{id}", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("kind"), r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/ai/summaries/{kind}/{id}", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in businessai.Input
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.Request(r.Context(), a, r.PathValue("kind"), r.PathValue("id"), in, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 202, map[string]bool{"queued": true})
	}))
	mux.HandleFunc("GET /api/v1/ai/questions/{kind}/{id}", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetQuestion(r.Context(), a, r.PathValue("kind"), r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/ai/questions/{kind}/{id}", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in businessai.QuestionInput
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.RequestQuestion(r.Context(), a, r.PathValue("kind"), r.PathValue("id"), in, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 202, map[string]bool{"queued": true})
	}))
}
