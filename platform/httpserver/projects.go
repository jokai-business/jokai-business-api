package httpserver

import (
	"ai-business-api/internal/project"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

func (b business) registerProjects(mux *http.ServeMux, s *project.Service) {
	mux.HandleFunc("GET /api/v1/project-sources", b.protected("project.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.Sources(r.Context(), a, r.URL.Query().Get("q"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("GET /api/v1/projects", b.protected("project.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.List(r.Context(), a, r.URL.Query().Get("q"), r.URL.Query().Get("status"), r.URL.Query().Get("customerId"), r.URL.Query().Get("opportunityId"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/projects", b.protected("project.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in project.Input
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
	mux.HandleFunc("GET /api/v1/projects/{id}", b.protected("project.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PATCH /api/v1/projects/{id}", b.protected("project.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in project.Patch
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
	mux.HandleFunc("GET /api/v1/projects/{id}/tasks", b.protected("project.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.Tasks(r.Context(), a, r.PathValue("id"), r.URL.Query().Get("status"), r.URL.Query().Get("assigneeId"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/projects/{id}/tasks", b.protected("project.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in project.TaskInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.AddTask(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("PATCH /api/v1/projects/{id}/tasks/{taskId}", b.protected("project.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in project.TaskPatch
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.UpdateTask(r.Context(), a, r.PathValue("id"), r.PathValue("taskId"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/projects/{id}/activity", b.protected("project.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.Activity(r.Context(), a, r.PathValue("id"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
}
