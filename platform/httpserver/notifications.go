package httpserver

import (
	"ai-business-api/internal/notification"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"net/http"
)

func (b business) registerNotifications(mux *http.ServeMux, s *notification.Service) {
	mux.HandleFunc("GET /api/v1/settings/email", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.EmailStatus(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/settings/email/deliveries", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		f := r.URL.Query().Get("status")
		if f == "" {
			f = "all"
		}
		v, n, e := s.EmailHistory(r.Context(), a, f, p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/settings/email/test", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in notification.TestEmailInput
		if !decodeBody(w, r, &in) {
			return
		}
		id, e := s.QueueTestEmail(r.Context(), a, in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 202, map[string]string{"id": id})
	}))
	mux.HandleFunc("POST /api/v1/settings/email/deliveries/{id}/retry", b.protected("settings.manage", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
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
		if e := s.RetryEmail(r.Context(), a, r.PathValue("id"), requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 202, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/v1/notifications/preferences", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, e := s.Preferences(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, p)
	}))
	mux.HandleFunc("PUT /api/v1/notifications/preferences", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			EmailEnabled *bool `json:"emailEnabled"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if in.EmailEnabled == nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		p, e := s.SetPreferences(r.Context(), a, *in.EmailEnabled, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, p)
	}))
	mux.HandleFunc("GET /api/v1/notifications", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		f := r.URL.Query().Get("filter")
		if f == "" {
			f = "active"
		}
		v, n, e := s.List(r.Context(), a, f, p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("GET /api/v1/notifications/unread", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		n, e := s.Unread(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]int{"count": n})
	}))
	mux.HandleFunc("POST /api/v1/notifications/refresh", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		if e := s.Refresh(r.Context(), a.Organization.ID); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/v1/notifications/{id}/read", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		if e := s.Read(r.Context(), a, r.PathValue("id"), false); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/v1/notifications/read-all", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		if e := s.Read(r.Context(), a, "", true); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/v1/reminders", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		v, n, e := s.Reminders(r.Context(), a, p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/reminders", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in notification.Input
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
	mux.HandleFunc("POST /api/v1/reminders/{id}/cancel", b.protected("", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Cancel(r.Context(), a, r.PathValue("id"), requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
}
