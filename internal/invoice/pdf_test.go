package invoice

import (
	"ai-business-api/internal/quote"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPDF(t *testing.T) {
	v := Invoice{Input: Input{IssueDate: "2026-09-29", DueDate: "2026-10-29", Tax: "225.09", Notes: "Payment by bank transfer within 30 days. Include the invoice number as the reference."}, Number: "INV-2026-000001", QuoteNumber: "Q-2026-000010", CompanyName: "Business OS Services", CustomerName: "Sample Customer Company", CustomerEmail: "finance@example.test", Currency: "USD", Subtotal: "2250.90", Total: "2475.99", Paid: "1000.00", Outstanding: "1475.99", Status: "partially_paid"}
	v.Items = []quote.Item{{ItemInput: quote.ItemInput{Quantity: "2.000", UnitPrice: "1250.50", DiscountPercent: "10.00"}, Name: "Customer portal implementation", Description: "Discovery, delivery planning and implementation of the customer workspace.", Unit: "project", Total: "2250.90"}}
	for _, name := range []string{"invoice", "invoice-long"} {
		if name == "invoice-long" {
			v.Items = nil
			for i := 0; i < 24; i++ {
				v.Items = append(v.Items, quote.Item{ItemInput: quote.ItemInput{Quantity: "1.000", UnitPrice: "100.00", DiscountPercent: "0.00"}, Name: fmt.Sprintf("Delivery work package %02d", i+1), Description: "Requirements review, implementation and acceptance support for the agreed service scope.", Unit: "service", Total: "100.00"})
			}
			v.Subtotal = "2400.00"
			v.Tax = "240.00"
			v.Total = "2640.00"
			v.Paid = "0.00"
			v.Outstanding = "2640.00"
			v.Status = "draft"
		}
		data, e := PDF(v)
		if e != nil || !bytes.HasPrefix(data, []byte("%PDF")) {
			t.Fatalf("PDF failed %v", e)
		}
		if dir := os.Getenv("PDF_REVIEW_DIR"); dir != "" {
			if e = os.WriteFile(filepath.Join(dir, name+".pdf"), data, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
}
