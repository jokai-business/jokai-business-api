package document

import (
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/currency"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var ErrCaptureInvalid = errors.New("invoice fields are incomplete or inconsistent")
var ErrCaptureConflict = errors.New("invoice capture changed or source text is not approved")

type InvoiceFields struct {
	InvoiceNumber string `json:"invoiceNumber"`
	InvoiceDate   string `json:"invoiceDate"`
	DueDate       string `json:"dueDate"`
	Vendor        string `json:"vendor"`
	Currency      string `json:"currency"`
	Subtotal      string `json:"subtotal"`
	Tax           string `json:"tax"`
	Total         string `json:"total"`
}
type InvoiceCapture struct {
	AIProvenance     *ExtractionProvenance `json:"aiProvenance"`
	ID               string                `json:"id"`
	Status           string                `json:"status"`
	Version          int                   `json:"version"`
	SourceOCRVersion int                   `json:"sourceOcrVersion"`
	SourceSHA256     string                `json:"sourceSha256"`
	Fields           InvoiceFields         `json:"fields"`
	Suggestions      InvoiceFields         `json:"suggestions"`
	RecordedAt       *time.Time            `json:"recordedAt"`
	ReviewerName     string                `json:"reviewerName"`
}
type CaptureInput struct {
	AIVersion        int           `json:"aiVersion"`
	Version          int           `json:"version"`
	SourceOCRVersion int           `json:"sourceOcrVersion"`
	Fields           InvoiceFields `json:"fields"`
}

// Suggestions are deliberately conservative, deterministic label matching, not AI.
// Repeated labels (even identical values) are ambiguous and produce a blank field.
func SuggestInvoice(text string) InvoiceFields {
	f := InvoiceFields{}
	labels := map[string]*string{"invoice number": &f.InvoiceNumber, "invoice date": &f.InvoiceDate, "due date": &f.DueDate, "vendor": &f.Vendor, "currency": &f.Currency, "subtotal": &f.Subtotal, "tax": &f.Tax, "total": &f.Total}
	seen := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if field, ok := labels[key]; ok {
			seen[key]++
			if seen[key] == 1 {
				*field = strings.TrimSpace(value)
			} else {
				*field = ""
			}
		}
	}
	// Discard malformed candidates rather than guessing formats, dates or currency.
	for _, field := range []*string{&f.InvoiceNumber, &f.Vendor} {
		if !cleanCaptureText(*field, 200) {
			*field = ""
		}
	}
	for _, field := range []*string{&f.InvoiceDate, &f.DueDate} {
		if *field != "" && !captureDate(*field) {
			*field = ""
		}
	}
	f.Currency = strings.ToUpper(f.Currency)
	if !captureCurrency(f.Currency) {
		f.Currency = ""
	}
	for _, field := range []*string{&f.Subtotal, &f.Tax, &f.Total} {
		if *field != "" {
			if _, ok := captureCents(*field); !ok {
				*field = ""
			} else {
				*field = normalizeCaptureMoney(*field)
			}
		}
	}
	return f
}
func cleanCaptureText(v string, max int) bool {
	return product.Text(v, max) && strings.IndexFunc(v, unicode.IsControl) < 0
}
func captureDate(v string) bool {
	d, e := time.Parse("2006-01-02", v)
	return e == nil && d.Year() >= 1 && d.Format("2006-01-02") == v
}
func captureCurrency(v string) bool {
	if !product.Currency.MatchString(v) {
		return false
	}
	_, e := currency.ParseISO(v)
	return e == nil
}
func captureCents(v string) (int64, bool) {
	if !product.Money.MatchString(v) {
		return 0, false
	}
	parts := strings.Split(v, ".")
	n, _ := strconv.ParseInt(parts[0], 10, 64)
	var cents int64
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		cents, _ = strconv.ParseInt(fraction, 10, 64)
	}
	return n*100 + cents, true
}
func normalizeCaptureMoney(v string) string {
	n, _ := captureCents(v)
	return strconv.FormatInt(n/100, 10) + "." + string([]byte{'0' + byte(n%100/10), '0' + byte(n%10)})
}
func validateCapture(f *InvoiceFields, complete bool) error {
	f.InvoiceNumber = strings.TrimSpace(f.InvoiceNumber)
	f.Vendor = strings.TrimSpace(f.Vendor)
	f.Currency = strings.ToUpper(strings.TrimSpace(f.Currency))
	if !cleanCaptureText(f.InvoiceNumber, 200) || !cleanCaptureText(f.Vendor, 200) {
		return ErrCaptureInvalid
	}
	for _, field := range []*string{&f.InvoiceDate, &f.DueDate} {
		*field = strings.TrimSpace(*field)
		if *field != "" && !captureDate(*field) {
			return ErrCaptureInvalid
		}
	}
	if f.Currency != "" && !captureCurrency(f.Currency) {
		return ErrCaptureInvalid
	}
	for _, field := range []*string{&f.Subtotal, &f.Tax, &f.Total} {
		*field = strings.TrimSpace(*field)
		if *field != "" {
			if _, ok := captureCents(*field); !ok {
				return ErrCaptureInvalid
			}
			*field = normalizeCaptureMoney(*field)
		}
	}
	if complete {
		if f.InvoiceNumber == "" || f.Vendor == "" || f.InvoiceDate == "" || f.DueDate == "" || f.Currency == "" || f.Subtotal == "" || f.Tax == "" || f.Total == "" || f.DueDate < f.InvoiceDate {
			return ErrCaptureInvalid
		}
		sub, _ := captureCents(f.Subtotal)
		tax, _ := captureCents(f.Tax)
		total, _ := captureCents(f.Total)
		if sub+tax != total {
			return ErrCaptureInvalid
		}
	}
	return nil
}

const captureColumns = `c.id,c.status,c.version,c.source_ocr_version,c.source_sha256,c.fields,c.suggestions,c.recorded_at,COALESCE(u.name,''),c.ai_provenance`
const captureJoined = ` FROM document_invoice_captures c LEFT JOIN users u ON u.organization_id=c.organization_id AND u.id=c.recorded_by`

func scanCapture(row pgx.Row) (InvoiceCapture, error) {
	v := InvoiceCapture{}
	e := row.Scan(&v.ID, &v.Status, &v.Version, &v.SourceOCRVersion, &v.SourceSHA256, &v.Fields, &v.Suggestions, &v.RecordedAt, &v.ReviewerName, &v.AIProvenance)
	return v, e
}
func (s *Service) GetInvoiceCapture(ctx context.Context, a user.Context, id string) (InvoiceCapture, error) {
	o, e := s.GetOCR(ctx, a, id)
	if e != nil {
		return InvoiceCapture{}, e
	}
	v, e := scanCapture(s.DB.QueryRow(ctx, `SELECT `+captureColumns+captureJoined+` WHERE c.organization_id=$1 AND c.document_id=$2`, a.Organization.ID, id))
	if !errors.Is(e, pgx.ErrNoRows) {
		return v, e
	}
	v = InvoiceCapture{Status: "awaiting_text"}
	if o.Status == "approved" {
		v.Status = "suggested"
		v.SourceOCRVersion = o.Version
		v.SourceSHA256 = o.SourceSHA256
		v.Fields = SuggestInvoice(o.ReviewedText)
		v.Suggestions = v.Fields
	}
	return v, nil
}
func (s *Service) SaveInvoiceCapture(ctx context.Context, a user.Context, id string, in CaptureInput, rid string) error {
	if !a.Allows("document.review") {
		return user.ErrForbidden
	}
	o, e := s.GetOCR(ctx, a, id)
	if e != nil {
		return e
	}
	if in.Version < 0 || in.AIVersion < 0 {
		return user.ErrInvalid
	}
	if o.Status != "approved" || o.Version != in.SourceOCRVersion {
		return ErrCaptureConflict
	}
	if e = validateCapture(&in.Fields, false); e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	fields, _ := json.Marshal(in.Fields)
	suggestions, _ := json.Marshal(SuggestInvoice(o.ReviewedText))
	var provenance *ExtractionProvenance
	if in.AIVersion > 0 {
		provenance, e = extractionProvenance(ctx, tx, a, id, in.AIVersion)
		if e != nil {
			return e
		}
		suggestions, _ = json.Marshal(provenance.Suggestion.Fields)
	}
	provenanceJSON, _ := json.Marshal(provenance)
	tag, e := tx.Exec(ctx, `INSERT INTO document_invoice_captures(organization_id,document_id,source_ocr_version,source_sha256,fields,suggestions,updated_by,ai_provenance)
 SELECT $1,$2,$3,$4,$5,$6,$7,$9 WHERE $8=0 ON CONFLICT(organization_id,document_id) DO NOTHING`, a.Organization.ID, id, o.Version, o.SourceSHA256, fields, suggestions, a.User.ID, in.Version, provenanceJSON)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		tag, e = tx.Exec(ctx, `UPDATE document_invoice_captures SET fields=$3,suggestions=CASE WHEN $7>0 THEN $8 ELSE suggestions END,ai_provenance=CASE WHEN $7>0 THEN $9 ELSE ai_provenance END,version=version+1,updated_at=now(),updated_by=$4 WHERE organization_id=$1 AND document_id=$2 AND version=$5 AND source_ocr_version=$6 AND status='draft'`, a.Organization.ID, id, fields, a.User.ID, in.Version, o.Version, in.AIVersion, suggestions, provenanceJSON)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return ErrCaptureConflict
		}
	}
	if e = audit(ctx, tx, a, "document.invoice_fields_saved", id, rid, map[string]any{"version": in.Version + 1, "sourceOcrVersion": o.Version}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) RecordInvoiceCapture(ctx context.Context, a user.Context, id string, version int, confirmed bool, rid string) error {
	if !a.Allows("document.review") {
		return user.ErrForbidden
	}
	if _, e := s.Get(ctx, a, id); e != nil {
		return e
	}
	if !confirmed || version < 1 {
		return user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	v, e := scanCapture(tx.QueryRow(ctx, `SELECT `+captureColumns+captureJoined+` WHERE c.organization_id=$1 AND c.document_id=$2 FOR UPDATE OF c`, a.Organization.ID, id))
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrCaptureConflict
	}
	if e != nil {
		return e
	}
	// A repeated confirmation of the same draft returns the one existing record.
	if v.Status == "recorded" && v.Version == version+1 {
		return nil
	}
	if v.Status != "draft" || v.Version != version {
		return ErrCaptureConflict
	}
	if e = validateCapture(&v.Fields, true); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE document_invoice_captures SET status='recorded',version=version+1,recorded_at=now(),recorded_by=$3 WHERE organization_id=$1 AND document_id=$2`, a.Organization.ID, id, a.User.ID)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, a, "document.source_invoice_recorded", id, rid, map[string]any{"recordId": v.ID, "sourceOcrVersion": v.SourceOCRVersion, "sha256": v.SourceSHA256}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
