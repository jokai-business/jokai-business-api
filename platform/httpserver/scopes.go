package httpserver

import (
	"ai-business-api/internal/scope"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
	"strconv"
)

func (b business) registerScopes(mux *http.ServeMux, s *scope.Service) {
	mux.HandleFunc("GET /api/v1/scopes", b.protected("scope.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, e)
			return
		}
		q := r.URL.Query()
		v, n, e := s.List(r.Context(), a, q.Get("q"), q.Get("status"), q.Get("opportunityId"), q.Get("customerId"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/scopes", b.protected("scope.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in scope.Input
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Create(r.Context(), a, in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("GET /api/v1/scopes/{id}", b.protected("scope.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PUT /api/v1/scopes/{id}", b.protected("scope.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in scope.Edit
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Edit(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/scopes/{id}/contributions", b.protected("scope.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in scope.ContributionInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Contribution(r.Context(), a, r.PathValue("id"), "", in, false, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("PUT /api/v1/scopes/{id}/contributions/{contributionId}", b.protected("scope.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in scope.ContributionInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Contribution(r.Context(), a, r.PathValue("id"), r.PathValue("contributionId"), in, false, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("DELETE /api/v1/scopes/{id}/contributions/{contributionId}", b.protected("scope.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in scope.Action
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Contribution(r.Context(), a, r.PathValue("id"), r.PathValue("contributionId"), scope.ContributionInput{Version: in.Version}, true, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	for _, action := range []string{"submit", "approve", "request-changes", "revise"} {
		permission := "scope.update"
		if action == "approve" || action == "request-changes" {
			permission = "scope.approve"
		}
		mux.HandleFunc("POST /api/v1/scopes/{id}/"+action, b.protected(permission, func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
			var in scope.Action
			if !decodeBody(w, r, &in) {
				return
			}
			v, e := s.Act(r.Context(), a, r.PathValue("id"), action, in, requestID(r))
			if e != nil {
				domainError(w, r, e)
				return
			}
			success(w, r, 200, v)
		}))
	}
	mux.HandleFunc("GET /api/v1/scopes/{id}/history", b.protected("scope.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, e)
			return
		}
		v, n, e := s.History(r.Context(), a, r.PathValue("id"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("GET /api/v1/scopes/{id}/revisions/{revision}", b.protected("scope.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		revision, e := strconv.Atoi(r.PathValue("revision"))
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, e := s.Snapshot(r.Context(), a, r.PathValue("id"), revision)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
}
