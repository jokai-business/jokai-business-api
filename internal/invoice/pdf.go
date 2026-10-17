package invoice

import "ai-business-api/internal/quote"

func PDF(v Invoice) ([]byte, error) {
	return quote.RenderPDF(quote.Quote{Number: v.Number, CompanyName: v.CompanyName, CustomerName: v.CustomerName, CustomerEmail: v.CustomerEmail, Currency: v.Currency, Status: v.Status, Terms: v.Notes, Subtotal: v.Subtotal, Total: v.Total, Items: v.Items}, &quote.InvoicePDFFields{IssueDate: v.IssueDate, DueDate: v.DueDate, Tax: v.Tax, Paid: v.Paid, Outstanding: v.Outstanding, QuoteNumber: v.QuoteNumber})
}
