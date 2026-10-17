package httpserver

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "Use application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		domainError(w, r, user.ErrInvalid)
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		domainError(w, r, user.ErrInvalid)
		return false
	}
	return true
}
func (b business) registerCustomers(mux *http.ServeMux, s *customer.Service) {
	mux.HandleFunc("GET /api/v1/customers", b.protected("customer.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.List(r.Context(), a, r.URL.Query().Get("q"), r.URL.Query().Get("status"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/customers", b.protected("customer.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in customer.Input
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
	mux.HandleFunc("GET /api/v1/customers/{id}", b.protected("customer.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PATCH /api/v1/customers/{id}", b.protected("customer.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in customer.Patch
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
	mux.HandleFunc("GET /api/v1/customer-owners", b.protected("customer.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.Owners(r.Context(), a, p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("GET /api/v1/customers/{id}/contacts", b.protected("customer.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.Contacts(r.Context(), a, r.PathValue("id"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/customers/{id}/contacts", b.protected("customer.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in customer.ContactInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.AddContact(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("GET /api/v1/customers/{id}/activity", b.protected("customer.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
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
