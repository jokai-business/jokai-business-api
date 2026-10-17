package httpserver

import (
	"ai-business-api/internal/imports"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"io"
	"net/http"
)

func (b business) registerImports(mux *http.ServeMux, s *imports.Service) {
	mux.HandleFunc("GET /api/v1/imports/{kind}/template", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		kind := r.PathValue("kind")
		if !a.Allows(kind+".read") || !a.Allows(kind+".create") {
			domainError(w, r, user.ErrForbidden)
			return
		}
		data, e := imports.Template(kind)
		if e != nil {
			domainError(w, r, e)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="`+kind+`-import.xlsx"`)
		w.Write(data)
	}))
	mux.HandleFunc("POST /api/v1/imports/{kind}/preview", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		r.Body = http.MaxBytesReader(w, r.Body, imports.MaxFile)
		data, e := io.ReadAll(r.Body)
		if e != nil {
			domainError(w, r, imports.ErrWorkbook)
			return
		}
		v, e := s.Preview(r.Context(), a, r.PathValue("kind"), data)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("GET /api/v1/imports/{id}", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/imports/{id}/confirm", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Confirmed bool `json:"confirmed"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Confirm(r.Context(), a, r.PathValue("id"), in.Confirmed, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
}
