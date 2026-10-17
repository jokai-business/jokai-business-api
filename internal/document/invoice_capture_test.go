package document

import (
	"ai-business-api/internal/user"
	"context"
	"errors"
	"sync"
	"testing"
)

const labelledInvoice = `Invoice Number: TEST-2042
Invoice Date: 2026-10-15
Due Date: 2026-11-15
Vendor: Example Supplies
Currency: USD
Subtotal: 100.00
Tax: 10.00
Total: 110.00`

func TestInvoiceSuggestions(t *testing.T) {
	f := SuggestInvoice(labelledInvoice)
	if f.InvoiceNumber != "TEST-2042" || f.Total != "110.00" || f.Vendor != "Example Supplies" {
		t.Fatal(f)
	}
	ambiguous := SuggestInvoice(labelledInvoice + "\nTotal: 110.00\nVendor: Another")
	if ambiguous.Total != "" || ambiguous.Vendor != "" {
		t.Fatal("guessed repeated fields", ambiguous)
	}
	invalid := SuggestInvoice("Invoice Date: 10/11/2026\nCurrency: dollars\nSubtotal: 1,000.00\nTax: 0\nTotal: 1e3\nVendor: Bad\x00vendor")
	if invalid.InvoiceDate != "" || invalid.Currency != "" || invalid.Subtotal != "" || invalid.Total != "" || invalid.Vendor != "" || invalid.Tax != "0.00" {
		t.Fatal(invalid)
	}
	if e := validateCapture(&f, true); e != nil {
		t.Fatal(e)
	}
}
func TestInvoiceCaptureValidation(t *testing.T) {
	valid := SuggestInvoice(labelledInvoice)
	for _, mutate := range []func(*InvoiceFields){func(f *InvoiceFields) { f.Total = "109.99" }, func(f *InvoiceFields) { f.DueDate = "2026-10-14" }, func(f *InvoiceFields) { f.InvoiceDate = "2026-02-30" }, func(f *InvoiceFields) { f.Currency = "XYZ" }, func(f *InvoiceFields) { f.Tax = "-1" }, func(f *InvoiceFields) { f.Subtotal = "1000000000000" }, func(f *InvoiceFields) { f.Vendor = "" }, func(f *InvoiceFields) { f.InvoiceNumber = "a\nb" }, func(f *InvoiceFields) { f.Total = "1e2" }, func(f *InvoiceFields) { f.Tax = "0.001" }} {
		f := valid
		mutate(&f)
		if validateCapture(&f, true) == nil {
			t.Fatal("accepted invalid fields", f)
		}
	}
	partial := InvoiceFields{Vendor: " Example "}
	if validateCapture(&partial, false) != nil || partial.Vendor != "Example" {
		t.Fatal("cannot save partial draft")
	}
	if validateCapture(&partial, true) == nil {
		t.Fatal("recorded incomplete draft")
	}
	exact := valid
	exact.Subtotal = "999999999999.98"
	exact.Tax = "0.01"
	exact.Total = "999999999999.99"
	if validateCapture(&exact, true) != nil {
		t.Fatal("decimal precision lost")
	}
}
func testInvoiceCapture(t *testing.T, s *Service, a user.Context, id, pending string) {
	t.Helper()
	ctx := context.Background()
	get := func() InvoiceCapture {
		t.Helper()
		v, e := s.GetInvoiceCapture(ctx, a, id)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	v := get()
	if v.Status != "suggested" || v.SourceOCRVersion == 0 || v.SourceSHA256 == "" {
		t.Fatal(v)
	}
	fields := SuggestInvoice(labelledInvoice)
	in := CaptureInput{SourceOCRVersion: v.SourceOCRVersion, Fields: fields}
	before, _ := s.GetInvoiceCapture(ctx, a, pending)
	if before.Status != "awaiting_text" {
		t.Fatal(before)
	}
	if e := s.SaveInvoiceCapture(ctx, a, pending, in, "test"); !errors.Is(e, ErrCaptureConflict) {
		t.Fatal("unapproved text used", e)
	}
	denied := a
	denied.Permissions = []string{"document.read", "document.sales"}
	if e := s.SaveInvoiceCapture(ctx, denied, id, in, "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	foreign := a
	foreign.Organization.ID = "00000000-0000-0000-0000-000000000003"
	if _, e := s.GetInvoiceCapture(ctx, foreign, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("tenant leak", e)
	}
	hidden := a
	hidden.Permissions = []string{"document.read", "document.review", "document.pmo"}
	if _, e := s.GetInvoiceCapture(ctx, hidden, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("team leak", e)
	}
	stale := in
	stale.SourceOCRVersion--
	if e := s.SaveInvoiceCapture(ctx, a, id, stale, "test"); !errors.Is(e, ErrCaptureConflict) {
		t.Fatal("source version", e)
	}
	in.Fields = InvoiceFields{Vendor: "Example Supplies"}
	if e := s.SaveInvoiceCapture(ctx, a, id, in, "test"); e != nil {
		t.Fatal(e)
	}
	v = get()
	if e := s.RecordInvoiceCapture(ctx, a, id, v.Version, true, "test"); !errors.Is(e, ErrCaptureInvalid) {
		t.Fatal("recorded partial", e)
	}
	in.Version = v.Version
	in.Fields = fields
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.SaveInvoiceCapture(ctx, a, id, in, "test") }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrCaptureConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("concurrent save", success)
	}
	v = get()
	if v.Fields.Total != "110.00" || v.Suggestions.Total != "" {
		t.Fatal("suggestions changed", v)
	}
	if e := s.RecordInvoiceCapture(ctx, a, id, v.Version, false, "test"); !errors.Is(e, user.ErrInvalid) {
		t.Fatal("missing confirmation", e)
	}
	if e := s.RecordInvoiceCapture(ctx, denied, id, v.Version, true, "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if e := s.RecordInvoiceCapture(ctx, a, id, v.Version-1, true, "test"); !errors.Is(e, ErrCaptureConflict) {
		t.Fatal("stale record", e)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.RecordInvoiceCapture(ctx, a, id, v.Version, true, "test"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	recorded := get()
	if recorded.Status != "recorded" || recorded.RecordedAt == nil || recorded.ReviewerName != "Reviewer" || recorded.ID != v.ID {
		t.Fatal(recorded)
	}
	if e := s.RecordInvoiceCapture(ctx, a, id, v.Version, true, "test"); e != nil {
		t.Fatal("retry confirmation", e)
	}
	in.Version = recorded.Version
	if e := s.SaveInvoiceCapture(ctx, a, id, in, "test"); !errors.Is(e, ErrCaptureConflict) {
		t.Fatal("overwrote recorded fields", e)
	}
	var n int
	if e := s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE resource_id=$1 AND action='document.source_invoice_recorded'`, id).Scan(&n); e != nil || n != 1 {
		t.Fatal("duplicate audit", n, e)
	}
	if e := s.DB.QueryRow(ctx, `SELECT count(*) FROM invoices`).Scan(&n); e != nil || n != 0 {
		t.Fatal("changed customer invoices", n, e)
	}
	if e := s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action LIKE 'document.%' AND after_value::text LIKE '%Example Supplies%'`).Scan(&n); e != nil || n != 0 {
		t.Fatal("field contents in audit", e)
	}
}
