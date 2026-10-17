package httpserver

import (
	"ai-business-api/internal/businessai"
	"ai-business-api/internal/customer"
	"ai-business-api/internal/document"
	"ai-business-api/internal/gmail"
	"ai-business-api/internal/imports"
	"ai-business-api/internal/invoice"
	"ai-business-api/internal/notification"
	"ai-business-api/internal/opportunity"
	"ai-business-api/internal/product"
	"ai-business-api/internal/project"
	"ai-business-api/internal/quote"
	"ai-business-api/internal/scope"
	"ai-business-api/internal/settings"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type business struct {
	service  *user.Service
	verifier auth.Verifier
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}
func success(w http.ResponseWriter, r *http.Request, status int, data any) {
	write(w, status, map[string]any{"data": data, "meta": map[string]any{"requestId": requestID(r)}})
}
func list(w http.ResponseWriter, r *http.Request, data any, page, size, total int) {
	write(w, 200, map[string]any{"data": data, "meta": map[string]any{"requestId": requestID(r), "page": page, "pageSize": size, "total": total}})
}
func pageParams(r *http.Request) (int, int, error) {
	page, size := 1, 25
	var err error
	if v := r.URL.Query().Get("page"); v != "" {
		page, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if v := r.URL.Query().Get("pageSize"); v != "" {
		size, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return 0, 0, user.ErrInvalid
	}
	return page, size, nil
}
func domainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, imports.ErrWorkbook):
		fail(w, r, 400, "IMPORT_WORKBOOK", "Use the standard single-sheet .xlsx template: up to 500 rows and 2 MB; no formulas, hidden rows, merged cells, macros or extra columns.")
	case errors.Is(err, imports.ErrChanged):
		fail(w, r, 409, "IMPORT_CHANGED", "The import has errors, its references changed, or the workbook was already imported. Upload a corrected workbook for a new preview.")
	case errors.Is(err, imports.ErrNotFound):
		fail(w, r, 404, "IMPORT_NOT_FOUND", "Import not found or preview expired.")
	case errors.Is(err, imports.ErrLimit):
		fail(w, r, 429, "IMPORT_LIMIT", "You have 20 pending previews. Previews expire after 24 hours.")
	case errors.Is(err, document.ErrExtractionDisabled):
		fail(w, r, 503, "AI_DISABLED", "AI extraction is not configured. Contact your deployment administrator.")
	case errors.Is(err, document.ErrExtractionChanged):
		fail(w, r, 409, "AI_EXTRACTION_CHANGED", "Approved text or AI suggestions changed. Reload before continuing.")
	case errors.Is(err, document.ErrExtractionLimit):
		fail(w, r, 429, "AI_EXTRACTION_LIMIT", "Wait for your pending extraction. Up to 20 extraction requests per member per hour are allowed.")
	case errors.Is(err, businessai.ErrNotFound):
		fail(w, r, 404, "AI_SOURCE_NOT_FOUND", "AI source was not found.")
	case errors.Is(err, businessai.ErrChanged):
		fail(w, r, 409, "AI_SUMMARY_CHANGED", "The source or AI result changed. Reload the preview before continuing.")
	case errors.Is(err, businessai.ErrDisabled):
		fail(w, r, 503, "AI_DISABLED", "AI is not configured. Contact your deployment administrator.")
	case errors.Is(err, businessai.ErrLimit):
		fail(w, r, 429, "AI_SUMMARY_LIMIT", "Wait for your pending request. Up to 20 requests per assistance type per member per hour are allowed.")
	case errors.Is(err, businessai.ErrContext):
		fail(w, r, 422, "AI_CONTEXT_LIMIT", "The source and conversation exceed the supported size. Start an independent question without previous conversation.")
	case errors.Is(err, gmail.ErrAIDisabled):
		fail(w, r, 503, "AI_DISABLED", "AI assistance is not configured. Contact your deployment administrator.")
	case errors.Is(err, gmail.ErrAIConflict):
		fail(w, r, 409, "AI_CHANGED", "The source or AI result changed. Reload before continuing.")
	case errors.Is(err, gmail.ErrAINotFound):
		fail(w, r, 404, "EMAIL_NOT_FOUND", "Email was not found in your recent inbox.")
	case errors.Is(err, gmail.ErrAIRate):
		fail(w, r, 429, "AI_LIMIT", "Wait for your pending AI request. Each member can request up to 20 generations per hour.")
	case errors.Is(err, gmail.ErrDisabled):
		fail(w, r, 503, "GMAIL_DISABLED", "Gmail is not configured. Ask your deployment administrator to configure Google OAuth.")
	case errors.Is(err, gmail.ErrState):
		fail(w, r, 409, "GMAIL_CONNECTION_CHANGED", "Start a new Gmail connection request.")
	case errors.Is(err, settings.ErrConflict):
		fail(w, r, 409, "SETTINGS_CHANGED", "Organization settings changed. Reload the latest settings before saving.")
	case errors.Is(err, settings.ErrLogo):
		fail(w, r, 400, "INVALID_LOGO", "Choose a valid PNG or JPEG up to 2 MiB and 2048 × 2048 pixels (4 million pixels maximum).")
	case errors.Is(err, settings.ErrNotFound):
		fail(w, r, 404, "LOGO_NOT_FOUND", "No organization logo is configured.")
	case errors.Is(err, notification.ErrEmailDisabled):
		fail(w, r, 503, "EMAIL_DISABLED", "Email delivery is not configured. Ask your deployment administrator to configure SMTP.")
	case errors.Is(err, notification.ErrEmailAction):
		fail(w, r, 409, "EMAIL_ACTION_UNAVAILABLE", "This delivery is no longer eligible for retry, or its manual retry has already been used. Refresh delivery history.")
	case errors.Is(err, notification.ErrEmailRate):
		fail(w, r, 429, "EMAIL_TEST_LIMIT", "Test emails are limited to one per minute and ten per hour per organization.")
	case errors.Is(err, notification.ErrNotFound):
		fail(w, r, 404, "NOTIFICATION_NOT_FOUND", "Notification or reminder was not found")
	case errors.Is(err, notification.ErrConflict):
		fail(w, r, 409, "REMINDER_CONFLICT", "This reminder has already been processed, or you have reached 100 scheduled reminders.")
	case errors.Is(err, document.ErrCaptureInvalid):
		fail(w, r, 400, "INVOICE_FIELDS_INVALID", "Use valid invoice fields. Recording requires all eight fields, a due date on or after the invoice date, and subtotal plus tax equal to total.")
	case errors.Is(err, document.ErrCaptureConflict):
		fail(w, r, 409, "INVOICE_FIELDS_CHANGED", "Approve the source text first. The invoice fields may have changed or already been recorded; reload before continuing.")
	case errors.Is(err, document.ErrOCRDisabled):
		fail(w, r, 503, "OCR_UNAVAILABLE", "OCR is not configured. Contact your administrator.")
	case errors.Is(err, document.ErrStorageUnavailable):
		fail(w, r, 503, "DOCUMENT_STORAGE_UNAVAILABLE", "Document storage is unavailable. Retry or contact your administrator.")
	case errors.Is(err, document.ErrNotFound):
		fail(w, r, 404, "DOCUMENT_NOT_FOUND", "Document was not found")
	case errors.Is(err, document.ErrPolicy):
		fail(w, r, 400, "DOCUMENT_POLICY", "This file exceeds your organization upload limit or its type is disabled. Reload the upload form to see the current policy.")
	case errors.Is(err, document.ErrType):
		fail(w, r, 415, "DOCUMENT_TYPE", "Upload a PDF, PNG or JPEG with matching file contents")
	case errors.Is(err, document.ErrSize):
		fail(w, r, 413, "DOCUMENT_TOO_LARGE", "Files must be 10 MiB or smaller")
	case errors.Is(err, document.ErrConflict):
		fail(w, r, 409, "DOCUMENT_CHANGED", "The document has already been reviewed. Reload to see the result.")
	case errors.Is(err, scope.ErrNotFound):
		fail(w, r, 404, "SCOPE_NOT_FOUND", "Scope, contribution or approved revision was not found.")
	case errors.Is(err, scope.ErrConflict):
		fail(w, r, 409, "SCOPE_CHANGED", "The scope changed or is locked for review. Reload before continuing.")
	case errors.Is(err, scope.ErrExists):
		fail(w, r, 409, "SCOPE_EXISTS", "This opportunity already has a service scope.")
	case errors.Is(err, scope.ErrIncomplete):
		fail(w, r, 400, "SCOPE_INCOMPLETE", "Add objectives and at least one team contribution before review.")
	case errors.Is(err, scope.ErrSelfReview):
		fail(w, r, 403, "INDEPENDENT_REVIEW_REQUIRED", "A manager who did not create, edit or submit this scope must review it.")
	case errors.Is(err, invoice.ErrNotFound):
		fail(w, r, 404, "INVOICE_NOT_FOUND", "Invoice was not found")
	case errors.Is(err, invoice.ErrConflict):
		fail(w, r, 409, "INVOICE_CHANGED", "Reload the invoice. This action may no longer be available.")
	case errors.Is(err, invoice.ErrExists):
		fail(w, r, 409, "INVOICE_EXISTS", "This quote already has an invoice.")
	case errors.Is(err, invoice.ErrPayment):
		fail(w, r, 400, "INVALID_PAYMENT", "Payment must not exceed the balance or precede the invoice issue date.")
	case errors.Is(err, project.ErrNotFound):
		fail(w, r, 404, "PROJECT_NOT_FOUND", "Project or task was not found")
	case errors.Is(err, project.ErrConflict):
		fail(w, r, 409, "PROJECT_CHANGED", "The project or task changed. Reload before saving.")
	case errors.Is(err, project.ErrExists):
		fail(w, r, 409, "PROJECT_EXISTS", "A delivery project already exists for this opportunity.")
	case errors.Is(err, project.ErrNotEligible):
		fail(w, r, 400, "SOURCE_NOT_READY", "Choose a won opportunity or an accepted quote for this customer.")
	case errors.Is(err, project.ErrIncomplete):
		fail(w, r, 409, "TASKS_INCOMPLETE", "Add and complete all tasks before marking the project completed.")
	case errors.Is(err, project.ErrClosed):
		fail(w, r, 409, "PROJECT_COMPLETED", "Reopen the project before adding or changing tasks.")
	case errors.Is(err, product.ErrNotFound), errors.Is(err, quote.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "The requested record was not found")
	case errors.Is(err, product.ErrConflict), errors.Is(err, quote.ErrConflict):
		fail(w, r, 409, "RECORD_CHANGED", "The record changed or this action is unavailable. Reload before continuing.")
	case errors.Is(err, quote.ErrExpired):
		fail(w, r, 409, "QUOTE_EXPIRED", "This quote has expired. Update the expiry date before submitting.")
	case errors.Is(err, opportunity.ErrNotFound):
		fail(w, r, 404, "OPPORTUNITY_NOT_FOUND", "Opportunity or pipeline stage was not found")
	case errors.Is(err, opportunity.ErrConflict):
		fail(w, r, 409, "RECORD_CHANGED", "This record changed. Reload before saving.")
	case errors.Is(err, customer.ErrNotFound):
		fail(w, r, 404, "CUSTOMER_NOT_FOUND", "Customer was not found")
	case errors.Is(err, customer.ErrConflict):
		fail(w, r, 409, "CUSTOMER_CHANGED", "Customer was changed by another user. Reload before saving.")
	case errors.Is(err, user.ErrForbidden):
		fail(w, r, 403, "FORBIDDEN", "You do not have access to this resource")
	case errors.Is(err, user.ErrInvalid):
		fail(w, r, 400, "VALIDATION_ERROR", "Check the submitted fields")
	case errors.Is(err, user.ErrConflict):
		fail(w, r, 409, "USER_EXISTS", "User is already registered")
	default:
		fail(w, r, 500, "INTERNAL_ERROR", "An unexpected error occurred")
	}
}
func (b business) protected(permission string, fn func(http.ResponseWriter, *http.Request, user.Context, auth.Identity)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := strings.Fields(r.Header.Get("Authorization"))
		if len(header) != 2 || !strings.EqualFold(header[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			fail(w, r, 401, "UNAUTHENTICATED", "Sign in to continue")
			return
		}
		identity, err := b.verifier.Verify(r.Context(), header[1])
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			fail(w, r, 401, "INVALID_TOKEN", "Your session is invalid or expired")
			return
		}
		actor, err := b.service.Resolve(r.Context(), identity.Subject)
		if err != nil {
			domainError(w, r, err)
			return
		}
		if permission != "" && !actor.Allows(permission) {
			domainError(w, r, user.ErrForbidden)
			return
		}
		fn(w, r, actor, identity)
	}
}
func (b business) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/me", b.protected("", func(w http.ResponseWriter, r *http.Request, c user.Context, i auth.Identity) {
		if err := b.service.Login(r.Context(), c, i.SessionID, requestID(r)); err != nil {
			domainError(w, r, err)
			return
		}
		success(w, r, 200, c)
	}))
	mux.HandleFunc("GET /api/v1/organization", b.protected("", func(w http.ResponseWriter, r *http.Request, c user.Context, _ auth.Identity) {
		success(w, r, 200, c.Organization)
	}))
	mux.HandleFunc("GET /api/v1/users", b.protected("user.manage", func(w http.ResponseWriter, r *http.Request, c user.Context, _ auth.Identity) {
		page, size, err := pageParams(r)
		if err != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		users, total, err := b.service.List(r.Context(), c, page, size)
		if err != nil {
			domainError(w, r, err)
			return
		}
		list(w, r, users, page, size, total)
	}))
	mux.HandleFunc("POST /api/v1/users", b.protected("user.manage", func(w http.ResponseWriter, r *http.Request, c user.Context, _ auth.Identity) {
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "Use application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var req user.CreateRequest
		if err := decoder.Decode(&req); err != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			domainError(w, r, user.ErrInvalid)
			return
		}
		result, err := b.service.Create(r.Context(), c, req, requestID(r))
		if err != nil {
			domainError(w, r, err)
			return
		}
		success(w, r, 201, result)
	}))
	mux.HandleFunc("GET /api/v1/audit-logs", b.protected("audit.read", func(w http.ResponseWriter, r *http.Request, c user.Context, _ auth.Identity) {
		page, size, err := pageParams(r)
		if err != nil {
			domainError(w, r, user.ErrInvalid)
			return
		}
		logs, total, err := b.service.Audit(r.Context(), c, page, size)
		if err != nil {
			domainError(w, r, err)
			return
		}
		list(w, r, logs, page, size, total)
	}))
}
