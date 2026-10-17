package httpserver

import (
	"ai-business-api/internal/invoice"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

func (b business) registerInvoices(mux *http.ServeMux, s *invoice.Service) {
	mux.HandleFunc("GET /api/v1/invoices", b.protected("invoice.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, e)
			return
		}
		q := r.URL.Query()
		v, n, e := s.List(r.Context(), a, q.Get("q"), q.Get("status"), q.Get("customerId"), q.Get("projectId"), q.Get("quoteId"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("GET /api/v1/finance/dashboard", b.protected("invoice.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Dashboard(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/invoices", b.protected("invoice.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in invoice.Input
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
	mux.HandleFunc("GET /api/v1/invoices/{id}", b.protected("invoice.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PUT /api/v1/invoices/{id}", b.protected("invoice.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in invoice.Patch
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
	for _, action := range []string{"issue", "mark-sent"} {
		mux.HandleFunc("POST /api/v1/invoices/{id}/"+action, b.protected("invoice.update", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
			var in invoice.Action
			if !decodeBody(w, r, &in) {
				return
			}
			v, e := s.Act(r.Context(), a, r.PathValue("id"), action, in.Version, requestID(r))
			if e != nil {
				domainError(w, r, e)
				return
			}
			success(w, r, 200, v)
		}))
	}
	mux.HandleFunc("POST /api/v1/invoices/{id}/payments", b.protected("payment.create", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in invoice.PaymentInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Pay(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("GET /api/v1/invoices/{id}/pdf", b.protected("invoice.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		data, e := invoice.PDF(v)
		if e != nil {
			domainError(w, r, e)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+v.Number+`.pdf"`)
		w.WriteHeader(200)
		_, _ = w.Write(data)
	}))
}
