package httpserver

import (
	"ai-business-api/internal/document"
	"ai-business-api/internal/settings"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"io"
	"net/http"
	"strconv"
)

func (b business) registerSettings(mux *http.ServeMux, s *settings.Service) {
	if s.Health != nil {
		mux.HandleFunc("GET /api/v1/settings/health", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
			v, e := s.Health.Check(r.Context(), a)
			if e != nil {
				domainError(w, r, e)
				return
			}
			success(w, r, 200, v)
		}))
	}

	mux.HandleFunc("GET /api/v1/settings/documents", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetDocumentPolicy(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PUT /api/v1/settings/documents", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in document.UploadPolicy
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.SaveDocumentPolicy(r.Context(), a, in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))

	mux.HandleFunc("GET /api/v1/settings/integrations", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, err := s.GetIntegrations(a)
		if err != nil {
			domainError(w, r, err)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/settings/sales", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetSales(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PUT /api/v1/settings/sales", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in settings.Sales
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.SaveSales(r.Context(), a, in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/quotes/defaults", b.protected("quote.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetQuoteDefaults(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))

	mux.HandleFunc("GET /api/v1/settings/organization", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, e := s.Get(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, p)
	}))
	mux.HandleFunc("PUT /api/v1/settings/organization", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in settings.Input
		if !decodeBody(w, r, &in) {
			return
		}
		p, e := s.Save(r.Context(), a, in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, p)
	}))
	mux.HandleFunc("GET /api/v1/settings/organization/logo", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		data, e := s.Logo(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", `inline; filename="organization-logo.png"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(200)
		_, _ = w.Write(data)
	}))
	mux.HandleFunc("PUT /api/v1/settings/organization/logo", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		r.Body = http.MaxBytesReader(w, r.Body, settings.MaxLogoSize+65536)
		e := r.ParseMultipartForm(settings.MaxLogoSize + 65536)
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		if e != nil {
			domainError(w, r, settings.ErrLogo)
			return
		}
		if r.MultipartForm == nil || len(r.MultipartForm.Value) != 1 || len(r.MultipartForm.Value["version"]) != 1 || len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
			domainError(w, r, user.ErrInvalid)
			return
		}
		version, e := strconv.Atoi(r.PostFormValue("version"))
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		f, _, e := r.FormFile("file")
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		defer f.Close()
		data, e := io.ReadAll(io.LimitReader(f, settings.MaxLogoSize+1))
		if e != nil {
			domainError(w, r, settings.ErrLogo)
			return
		}
		p, e := s.SetLogo(r.Context(), a, version, data, false, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, p)
	}))
	mux.HandleFunc("DELETE /api/v1/settings/organization/logo", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Version int `json:"version"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		p, e := s.SetLogo(r.Context(), a, in.Version, nil, true, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, p)
	}))
}
