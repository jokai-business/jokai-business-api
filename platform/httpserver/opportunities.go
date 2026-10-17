package httpserver

import (
	"ai-business-api/internal/opportunity"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

func (b business) registerOpportunities(mux *http.ServeMux, s *opportunity.Service) {
	mux.HandleFunc("GET /api/v1/opportunities", b.protected("opportunity.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.List(r.Context(), a, r.URL.Query().Get("q"), r.URL.Query().Get("stageId"), r.URL.Query().Get("customerId"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("GET /api/v1/opportunities/{id}", b.protected("opportunity.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/opportunities", b.protected("opportunity.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in opportunity.Input
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
	mux.HandleFunc("PATCH /api/v1/opportunities/{id}", b.protected("opportunity.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in opportunity.Patch
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Update(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/pipeline-stages", b.protected("opportunity.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Stages(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/pipeline-stages", b.protected("pipeline.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in opportunity.StageInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.SaveStage(r.Context(), a, "", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("PUT /api/v1/pipeline-stages/{id}", b.protected("pipeline.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in opportunity.StageInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.SaveStage(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/opportunities/{id}/activities", b.protected("opportunity.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.Activities(r.Context(), a, r.PathValue("id"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/opportunities/{id}/activities", b.protected("opportunity.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Body string `json:"body"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.AddActivity(r.Context(), a, r.PathValue("id"), in.Body, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
}
