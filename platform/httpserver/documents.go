package httpserver

import (
	"ai-business-api/internal/document"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
)

func (b business) registerDocuments(mux *http.ServeMux, s *document.Service) {
	mux.HandleFunc("GET /api/v1/documents/upload-policy", b.protected("document.upload", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetUploadPolicy(r.Context(), a)
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}/ai-extraction", b.protected("document.review", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetExtraction(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/ai-extraction", b.protected("document.review", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in document.ExtractionInput
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.RequestExtraction(r.Context(), a, r.PathValue("id"), in, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 202, map[string]bool{"queued": true})
	}))

	mux.HandleFunc("GET /api/v1/documents/{id}/invoice-capture", b.protected("document.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetInvoiceCapture(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("PUT /api/v1/documents/{id}/invoice-capture", b.protected("document.review", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in document.CaptureInput
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.SaveInvoiceCapture(r.Context(), a, r.PathValue("id"), in, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/invoice-capture/record", b.protected("document.review", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Version   int  `json:"version"`
			Confirmed bool `json:"confirmed"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.RecordInvoiceCapture(r.Context(), a, r.PathValue("id"), in.Version, in.Confirmed, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("GET /api/v1/documents/{id}/ocr", b.protected("document.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.GetOCR(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/ocr", b.protected("document.review", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in struct {
			Version  int    `json:"version"`
			Language string `json:"language"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.RequestOCRLanguage(r.Context(), a, r.PathValue("id"), in.Version, in.Language, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 202, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/ocr/review", b.protected("document.review", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in document.OCRReview
		if !decodeBody(w, r, &in) {
			return
		}
		if e := s.ReviewOCR(r.Context(), a, r.PathValue("id"), in, requestID(r)); e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("GET /api/v1/documents", b.protected("document.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		p, z, e := pageParams(r)
		if e != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		q := r.URL.Query()
		v, n, e := s.List(r.Context(), a, q.Get("q"), q.Get("workspace"), q.Get("status"), q.Get("customerId"), p, z)
		if e != nil {
			domainError(w, r, e)
			return
		}
		list(w, r, v, p, z, n)
	}))
	mux.HandleFunc("POST /api/v1/documents", b.protected("document.upload", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		r.Body = http.MaxBytesReader(w, r.Body, document.MaxSize+65536)
		err := r.ParseMultipartForm(document.MaxSize + 65536)
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		if err != nil {
			var sizeErr *http.MaxBytesError
			if errors.As(err, &sizeErr) {
				domainError(w, r, document.ErrSize)
			} else {
				domainError(w, r, user.ErrInvalid)
			}
			return
		}
		for k, v := range r.MultipartForm.Value {
			if (k != "title" && k != "workspace" && k != "customerId" && k != "notes") || len(v) != 1 {
				domainError(w, r, user.ErrInvalid)
				return
			}
		}
		if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
			domainError(w, r, user.ErrInvalid)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		defer file.Close()
		if header.Size > document.MaxSize {
			domainError(w, r, document.ErrSize)
			return
		}
		data, err := io.ReadAll(io.LimitReader(file, document.MaxSize+1))
		if err != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		in := document.Input{Title: r.PostFormValue("title"), Workspace: r.PostFormValue("workspace"), CustomerID: r.PostFormValue("customerId"), Notes: r.PostFormValue("notes"), Filename: header.Filename}
		v, err := s.Upload(r.Context(), a, in, data, requestID(r))
		if err != nil {
			domainError(w, r, err)
			return
		}
		success(w, r, 201, v)
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}", b.protected("document.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, e := s.Get(r.Context(), a, r.PathValue("id"))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/review", b.protected("document.review", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		var in document.Review
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.Review(r.Context(), a, r.PathValue("id"), in, requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		success(w, r, 200, v)
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}/download", b.protected("document.read", func(w http.ResponseWriter, r *http.Request, a user.Context, _ auth.Identity) {
		v, data, e := s.Download(r.Context(), a, r.PathValue("id"), requestID(r))
		if e != nil {
			domainError(w, r, e)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": v.Filename}))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(200)
		_, _ = w.Write(data)
	}))
}
