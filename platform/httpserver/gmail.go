package httpserver

import (
	"ai-business-api/internal/gmail"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

const gmailCookie = "__Host-gmail_oauth"

func (b business) registerGmail(mux *http.ServeMux, s *gmail.Service) {
	mux.HandleFunc("GET /api/v1/gmail/ai", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		success(w, r, 200, s.AIStatus())
	}))
	mux.HandleFunc("GET /api/v1/gmail/messages/{id}/assistance", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Assistance(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/gmail/messages/{id}/assistance", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in gmail.AssistanceRequest
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.RequestAssistance(r.Context(), a, r.PathValue("id"), in, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 202, map[string]bool{"queued": true})
	}))
	mux.HandleFunc("PUT /api/v1/gmail/messages/{id}/draft", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in gmail.DraftReview
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.ReviewDraft(r.Context(), a, r.PathValue("id"), in, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]bool{"saved": true})
	}))
	mux.HandleFunc("GET /api/v1/gmail/sync", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.SyncStatus(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PUT /api/v1/gmail/sync", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Enabled   *bool `json:"enabled"`
			Confirmed bool  `json:"confirmed"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if in.Enabled == nil || !in.Confirmed {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, e := s.SetSync(r.Context(), a, *in.Enabled, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/gmail/messages", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		page, size, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		filter := r.URL.Query().Get("filter")
		if filter == "" {
			filter = "all"
		}
		items, total, e := s.Messages(r.Context(), a, filter, r.URL.Query().Get("customerId"), page, size)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, items, page, size, total)
	}))

	mux.HandleFunc("GET /api/v1/gmail", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Status(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/gmail/connect", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Confirmed bool `json:"confirmed"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if !in.Confirmed {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, e := s.Begin(r.Context(), a, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: gmailCookie, Value: v.BrowserSecret, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
		success(w, r, 200, v)
	}))
	mux.HandleFunc("DELETE /api/v1/gmail", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Confirmed bool `json:"confirmed"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if !in.Confirmed {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, e := s.Disconnect(r.Context(), a, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		clearGmailCookie(w)
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/gmail/callback", func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie(gmailCookie)
		outcome := "failed"
		q := r.URL.Query()
		if e == nil && len(q["state"]) == 1 && len(q["code"]) <= 1 && len(q["error"]) <= 1 {
			if s.Complete(r.Context(), q.Get("state"), c.Value, q.Get("code"), q.Get("error") != "", requestID(r)) == nil {
				outcome = "connected"
			}
		}
		clearGmailCookie(w)
		http.Redirect(w, r, "/gmail?connection="+outcome, http.StatusSeeOther)
	})
}
func clearGmailCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: gmailCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
