package quote

import (
	"ai-business-api/internal/user"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testID = "11111111-1111-4111-8111-111111111111"

func sampleInput() Input {
	return Input{OpportunityID: testID, Currency: "USD", ExpiryDate: "2099-12-01", Items: []ItemInput{{ProductID: testID, Quantity: "2", UnitPrice: "1250.50", DiscountPercent: "10"}}}
}
func TestExactTotalsAndRounding(t *testing.T) {
	in := sampleInput()
	items, sub, disc, total, e := Calculate(&in)
	if e != nil || sub != "2501.00" || disc != "250.10" || total != "2250.90" || items[0].Total != total {
		t.Fatalf("incorrect totals: %s %s %s %v", sub, disc, total, e)
	}
	in.Items[0] = ItemInput{ProductID: testID, Quantity: "1.005", UnitPrice: "1.00", DiscountPercent: "50"}
	_, sub, disc, total, e = Calculate(&in)
	if e != nil || sub != "1.01" || disc != "0.51" || total != "0.50" {
		t.Fatal("half-up rounding failed")
	}
	in.Items[0].DiscountPercent = "100"
	_, _, _, total, e = Calculate(&in)
	if e != nil || total != "0.00" {
		t.Fatal("full discount failed")
	}
}
func TestInvalidQuoteValues(t *testing.T) {
	tests := []func(*Input){func(i *Input) { i.Items = nil }, func(i *Input) { i.Items[0].Quantity = "0" }, func(i *Input) { i.Items[0].Quantity = "1.0001" }, func(i *Input) { i.Items[0].UnitPrice = "-1" }, func(i *Input) { i.Items[0].UnitPrice = "1e3" }, func(i *Input) { i.Items[0].DiscountPercent = "100.01" }, func(i *Input) { i.Items[0].DiscountPercent = "-1" }, func(i *Input) { i.ExpiryDate = "2026-02-30" }, func(i *Input) { i.Currency = "bad currency" }, func(i *Input) { i.Items[0].ProductID = "bad" }, func(i *Input) { i.Items[0].Quantity = "9999999.999"; i.Items[0].UnitPrice = "999999999999.99" }}
	for n, change := range tests {
		in := sampleInput()
		change(&in)
		if _, _, _, _, e := Calculate(&in); e != user.ErrInvalid {
			t.Fatalf("accepted invalid case %d", n)
		}
	}
}
func TestOnlyAllowedTransitions(t *testing.T) {
	allowed := map[string]string{"draft/submit": "submitted", "submitted/approve": "approved", "submitted/reject": "rejected", "approved/mark-sent": "sent", "sent/accept": "accepted"}
	for _, status := range []string{"draft", "submitted", "approved", "rejected", "sent", "accepted"} {
		for _, action := range []string{"submit", "approve", "reject", "mark-sent", "accept"} {
			if got := Transition(status, action); got != allowed[status+"/"+action] {
				t.Fatalf("invalid transition %s/%s", status, action)
			}
		}
	}
}
func TestPermissionDeniedBeforeDatabase(t *testing.T) {
	s := Service{}
	a := user.Context{Permissions: []string{"quote.read"}}
	if _, e := s.Save(context.Background(), a, "", sampleInput(), "req"); e != user.ErrForbidden {
		t.Fatal("read-only save allowed")
	}
	if _, e := s.Act(context.Background(), a, testID, "approve", Action{Version: 1}, "req"); e != user.ErrForbidden {
		t.Fatal("read-only approval allowed")
	}
	a.Permissions = []string{"quote.create"}
	if _, e := s.Act(context.Background(), a, testID, "approve", Action{Version: 1}, "req"); e != user.ErrForbidden {
		t.Fatal("sales approval allowed")
	}
}
func TestPDFTemplate(t *testing.T) {
	q := Quote{Number: "Q-000042", CompanyName: "Northstar Studio", CustomerName: "Acme Technology", CustomerEmail: "hello@example.test", OpportunityTitle: "Customer portal implementation", Currency: "USD", Status: "approved", ExpiryDate: "2026-12-01", CreatedAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), Subtotal: "12500.00", DiscountTotal: "1250.00", Total: "11250.00", Terms: "50% on acceptance; balance due on delivery.\nScope includes discovery, implementation and launch support."}
	q.Items = []Item{{ItemInput: ItemInput{Quantity: "1", UnitPrice: "12500.00", DiscountPercent: "10"}, Name: "Customer portal delivery", Description: "Discovery workshop, UX design, secure implementation and launch support.", Unit: "project", Subtotal: "12500.00", DiscountAmount: "1250.00", Total: "11250.00"}}
	pdf, e := PDF(q)
	if e != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(pdf) < 1000 {
		t.Fatal("PDF generation failed", e)
	}
	if dir := os.Getenv("QUOTE_PDF_REVIEW_DIR"); dir != "" {
		if e = os.MkdirAll(dir, 0755); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, "quote-standard.pdf"), pdf, 0644); e != nil {
			t.Fatal(e)
		}
	}
	q.Terms = strings.Repeat("A detailed scope is agreed with the customer before delivery. ", 100)
	q.Items = append(q.Items, q.Items[0], q.Items[0], q.Items[0], q.Items[0], q.Items[0], q.Items[0], q.Items[0])
	pdf, e = PDF(q)
	if e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("QUOTE_PDF_REVIEW_DIR"); dir != "" {
		if e = os.WriteFile(filepath.Join(dir, "quote-multipage.pdf"), pdf, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
