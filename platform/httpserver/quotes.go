package httpserver

import (
	"ai-business-api/internal/product"
	"ai-business-api/internal/quote"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

func (b business) registerQuotes(mux *http.ServeMux, p *product.Service, s *quote.Service) {
	mux.HandleFunc("GET /api/v1/products", b.protected("product.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		page, size, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		active := r.URL.Query().Get("active")
		if active != "" && active != "true" && active != "false" {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := p.List(r.Context(), a, r.URL.Query().Get("q"), active == "true", page, size)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, page, size, n)
	}))
	mux.HandleFunc("POST /api/v1/products", b.protected("product.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in product.Input
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := p.Save(r.Context(), a, "", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("PUT /api/v1/products/{id}", b.protected("product.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in product.Input
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := p.Save(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/quotes", b.protected("quote.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		page, size, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.List(r.Context(), a, r.URL.Query().Get("q"), r.URL.Query().Get("status"), r.URL.Query().Get("opportunityId"), r.URL.Query().Get("customerId"), page, size)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, page, size, n)
	}))
	mux.HandleFunc("POST /api/v1/quotes", b.protected("quote.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in quote.Input
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Save(r.Context(), a, "", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("PUT /api/v1/quotes/{id}", b.protected("quote.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in quote.Input
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Save(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/quotes/{id}", b.protected("quote.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/quotes/{id}/submit", b.protected("quote.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in quote.Action
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Act(r.Context(), a, r.PathValue("id"), "submit", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/quotes/{id}/approve", b.protected("quote.approve", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in quote.Action
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Act(r.Context(), a, r.PathValue("id"), "approve", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/quotes/{id}/reject", b.protected("quote.approve", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in quote.Action
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Act(r.Context(), a, r.PathValue("id"), "reject", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/quotes/{id}/mark-sent", b.protected("quote.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in quote.Action
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Act(r.Context(), a, r.PathValue("id"), "mark-sent", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/quotes/{id}/accept", b.protected("quote.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in quote.Action
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Act(r.Context(), a, r.PathValue("id"), "accept", in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/quotes/{id}/pdf", b.protected("quote.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		q, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		pdf, e := quote.PDF(q)
		if e != nil {
			domainError(w, r, e)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+q.Number+`.pdf"`)
		w.WriteHeader(200)
		_, _ = w.Write(pdf)
	}))
}
