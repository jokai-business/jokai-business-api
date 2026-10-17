package document

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"strings"
)

var ErrExtractionDisabled = errors.New("AI extraction disabled")
var ErrExtractionChanged = errors.New("AI extraction or source changed")
var ErrExtractionLimit = errors.New("AI extraction limit reached")

type ExtractionSource struct {
	Text       string `json:"text"`
	OCRVersion int    `json:"ocrVersion"`
	SHA256     string `json:"sha256"`
}
type ExtractionSuggestion struct {
	Fields   InvoiceFields `json:"fields"`
	Evidence InvoiceFields `json:"evidence"`
}
type ExtractionProvenance struct {
	Provider      string               `json:"provider"`
	Model         string               `json:"model"`
	PromptVersion string               `json:"promptVersion"`
	Suggestion    ExtractionSuggestion `json:"suggestion"`
}

func extractionSource(ctx context.Context, tx pgx.Tx, a user.Context, id string) (ExtractionSource, error) {
	var out ExtractionSource
	if !customer.UUID(id) {
		return out, user.ErrInvalid
	}
	// Reload current membership and permissions in the same snapshot as the source.
	var perms []string
	e := tx.QueryRow(ctx, `SELECT ARRAY(SELECT DISTINCT rp.permission_key FROM user_roles ur JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE ur.organization_id=u.organization_id AND ur.user_id=u.id) FROM users u WHERE u.organization_id=$1 AND u.id=$2 AND u.active`, a.Organization.ID, a.User.ID).Scan(&perms)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, user.ErrForbidden
	}
	if e != nil {
		return out, e
	}
	a.Permissions = perms
	if !a.Allows("document.read") || !a.Allows("document.review") {
		return out, user.ErrForbidden
	}
	var status string
	e = tx.QueryRow(ctx, `SELECT d.status FROM documents d WHERE d.organization_id=$1 AND d.id=$2 AND d.workspace=ANY($3::text[])`, a.Organization.ID, id, Workspaces(a)).Scan(&status)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if status != "accepted" {
		return out, ErrExtractionChanged
	}
	e = tx.QueryRow(ctx, `SELECT reviewed_text,version,source_sha256 FROM document_ocr WHERE organization_id=$1 AND document_id=$2 AND status='approved'`, a.Organization.ID, id).Scan(&out.Text, &out.OCRVersion, &out.SHA256)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrExtractionChanged
	}
	if e != nil {
		return out, e
	}
	if !validOCRText(out.Text) || strings.TrimSpace(out.Text) == "" {
		return out, ErrExtractionChanged
	}
	return out, nil
}
func extractionFingerprint(s ExtractionSource) (string, string, error) {
	b, e := json.Marshal(s)
	return string(b), hash(b), e
}

// Strict objects reject duplicate keys, missing keys, nulls and non-string values.
// A permissive JSON decoder could silently accept conflicting model values.
func extractionObject(raw string, keys []string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(strings.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return nil, ErrCaptureInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		tok, e = d.Token()
		key, ok := tok.(string)
		if e != nil || !ok {
			return nil, ErrCaptureInvalid
		}
		if _, ok = out[key]; ok {
			return nil, ErrCaptureInvalid
		}
		var v json.RawMessage
		if e = d.Decode(&v); e != nil {
			return nil, ErrCaptureInvalid
		}
		out[key] = v
	}
	if _, e = d.Token(); e != nil {
		return nil, ErrCaptureInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrCaptureInvalid
	}
	if len(out) != len(keys) {
		return nil, ErrCaptureInvalid
	}
	for _, k := range keys {
		if _, ok := out[k]; !ok {
			return nil, ErrCaptureInvalid
		}
	}
	return out, nil
}
func parseExtraction(raw, text string) (ExtractionSuggestion, error) {
	var out ExtractionSuggestion
	if !ai.ValidText(raw) {
		return out, ErrCaptureInvalid
	}
	top, e := extractionObject(raw, []string{"fields", "evidence"})
	if e != nil {
		return out, e
	}
	keys := []string{"invoiceNumber", "invoiceDate", "dueDate", "vendor", "currency", "subtotal", "tax", "total"}
	fields, e := extractionObject(string(top["fields"]), keys)
	if e != nil {
		return out, e
	}
	evidence, e := extractionObject(string(top["evidence"]), keys)
	if e != nil {
		return out, e
	}
	for _, key := range keys {
		var value, quote string
		if string(fields[key]) == "null" || string(evidence[key]) == "null" || json.Unmarshal(fields[key], &value) != nil || json.Unmarshal(evidence[key], &quote) != nil {
			return out, ErrCaptureInvalid
		}
		if value != strings.TrimSpace(value) || (value == "") != (quote == "") || len(quote) > 500 || (value != "" && (!strings.Contains(text, quote) || strings.TrimSpace(quote) == "")) {
			return out, ErrCaptureInvalid
		}
	}
	if json.Unmarshal(top["fields"], &out.Fields) != nil || json.Unmarshal(top["evidence"], &out.Evidence) != nil {
		return out, ErrCaptureInvalid
	}
	if e = validateCapture(&out.Fields, false); e != nil {
		return out, e
	}
	f := out.Fields
	if f.InvoiceDate != "" && f.DueDate != "" && f.DueDate < f.InvoiceDate {
		return out, ErrCaptureInvalid
	}
	if f.Subtotal != "" && f.Tax != "" && f.Total != "" {
		sub, _ := captureCents(f.Subtotal)
		tax, _ := captureCents(f.Tax)
		total, _ := captureCents(f.Total)
		if sub+tax != total {
			return out, ErrCaptureInvalid
		}
	}
	return out, nil
}

func extractionProvenance(ctx context.Context, tx pgx.Tx, a user.Context, id string, version int) (*ExtractionProvenance, error) {
	src, e := extractionSource(ctx, tx, a, id)
	if e != nil {
		return nil, e
	}
	_, hash, e := extractionFingerprint(src)
	if e != nil {
		return nil, e
	}
	var out ExtractionProvenance
	var raw string
	e = tx.QueryRow(ctx, `SELECT provider,model,prompt_version,output FROM document_ai_extractions WHERE organization_id=$1 AND user_id=$2 AND document_id=$3 AND version=$4 AND source_hash=$5 AND status='ready' AND requested_at>now()-interval '30 days'`, a.Organization.ID, a.User.ID, id, version, hash).Scan(&out.Provider, &out.Model, &out.PromptVersion, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrExtractionChanged
	}
	if e != nil {
		return nil, e
	}
	out.Suggestion, e = parseExtraction(raw, src.Text)
	return &out, e
}
