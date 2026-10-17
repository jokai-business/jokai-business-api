package quote

import (
	"bytes"
	"embed"
	"fmt"
	"github.com/go-pdf/fpdf"
	"strings"
)

//go:embed fonts/*.ttf
var fonts embed.FS

// PDF produces the standard quote template from the persisted quote snapshot.
func PDF(q Quote) ([]byte, error) { return RenderPDF(q, nil) }

// InvoicePDFFields supplies invoice-specific labels to the shared document renderer.
type InvoicePDFFields struct{ IssueDate, DueDate, Tax, Paid, Outstanding, QuoteNumber string }

func RenderPDF(q Quote, invoice *InvoicePDFFields) ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(16, 18, 16)
	p.SetAutoPageBreak(true, 20)
	regular, _ := fonts.ReadFile("fonts/DejaVuSans.ttf")
	bold, _ := fonts.ReadFile("fonts/DejaVuSans-Bold.ttf")
	p.AddUTF8FontFromBytes("Sans", "", regular)
	p.AddUTF8FontFromBytes("Sans", "B", bold)
	p.SetTitle(q.Number+" - "+q.CustomerName, true)
	p.SetAuthor(q.CompanyName, true)
	p.AliasNbPages("")
	p.SetHeaderFunc(func() {
		p.SetFont("Sans", "", 8)
		p.SetTextColor(105, 115, 134)
		p.SetXY(16, 9)
		p.CellFormat(178, 5, "BUSINESS OS  /  "+q.Number, "", 0, "R", false, 0, "")
		p.SetY(21)
	})
	p.SetFooterFunc(func() {
		p.SetY(-13)
		p.SetFont("Sans", "", 8)
		p.SetTextColor(105, 115, 134)
		p.CellFormat(178, 5, fmt.Sprintf("%s  |  %s  |  Page %d/{nb}", q.Number, strings.ToUpper(q.Status), p.PageNo()), "", 0, "C", false, 0, "")
	})
	p.AddPage()
	p.SetTextColor(81, 70, 223)
	p.SetFont("Sans", "B", 10)
	p.MultiCell(178, 6, q.CompanyName, "", "L", false)
	p.Ln(5)
	p.SetTextColor(32, 37, 54)
	p.SetFont("Sans", "B", 26)
	title := "QUOTE"
	if invoice != nil {
		title = "INVOICE"
	}
	p.CellFormat(178, 13, title, "", 1, "L", false, 0, "")
	p.SetFont("Sans", "", 10)
	p.CellFormat(178, 7, q.Number+"  /  "+strings.ToUpper(q.Status), "", 1, "L", false, 0, "")
	p.Ln(5)
	p.SetFont("Sans", "B", 9)
	p.SetTextColor(105, 115, 134)
	p.CellFormat(178, 6, "PREPARED FOR", "", 1, "L", false, 0, "")
	p.SetFont("Sans", "B", 12)
	p.SetTextColor(32, 37, 54)
	p.MultiCell(178, 7, q.CustomerName, "", "L", false)
	p.SetFont("Sans", "", 10)
	if q.CustomerEmail != "" {
		p.MultiCell(178, 6, q.CustomerEmail, "", "L", false)
	}
	p.Ln(3)
	if invoice == nil {
		p.MultiCell(178, 6, "Opportunity: "+q.OpportunityTitle, "", "L", false)
	} else {
		p.MultiCell(178, 6, "Source quote: "+invoice.QuoteNumber, "", "L", false)
	}
	dates := "Issued: " + q.CreatedAt.UTC().Format("2006-01-02") + "    Valid through: " + q.ExpiryDate
	if invoice != nil {
		dates = "Issue date: " + invoice.IssueDate + "    Due date: " + invoice.DueDate
	}
	p.MultiCell(178, 7, dates+"    Currency: "+q.Currency, "", "L", false)
	p.Ln(6)
	for i, item := range q.Items {
		if p.GetY() > 236 {
			p.AddPage()
		}
		p.SetFillColor(246, 247, 251)
		p.SetTextColor(32, 37, 54)
		p.SetFont("Sans", "B", 10)
		p.MultiCell(178, 7, fmt.Sprintf("%02d  %s", i+1, item.Name), "", "L", true)
		p.SetFont("Sans", "", 9)
		if item.Description != "" {
			p.MultiCell(178, 5, item.Description, "", "L", false)
		}
		p.Ln(2)
		if p.GetY() > 252 {
			p.AddPage()
		}
		labels := []string{"Quantity / unit", "Unit price", "Discount", "Line total"}
		values := []string{item.Quantity + " / " + item.Unit, item.UnitPrice, item.DiscountPercent + "%", item.Total}
		widths := []float64{58, 40, 35, 45}
		p.SetTextColor(105, 115, 134)
		p.SetFont("Sans", "", 8)
		for j, label := range labels {
			p.CellFormat(widths[j], 6, label, "", 0, "L", false, 0, "")
		}
		p.Ln(6)
		p.SetTextColor(32, 37, 54)
		p.SetFont("Sans", "", 9)
		// Unit labels wrap independently while keeping the amount columns aligned.
		y := p.GetY()
		p.MultiCell(widths[0], 5, values[0], "", "L", false)
		bottom := p.GetY()
		x := 16 + widths[0]
		for j := 1; j < 4; j++ {
			p.SetXY(x, y)
			p.CellFormat(widths[j], 5, values[j], "", 0, "L", false, 0, "")
			x += widths[j]
		}
		p.SetXY(16, bottom+5)
		p.SetDrawColor(231, 233, 240)
		p.Line(16, p.GetY(), 194, p.GetY())
		p.Ln(5)
	}
	if p.GetY() > 230 {
		p.AddPage()
	}
	p.Ln(3)
	p.SetFont("Sans", "", 10)
	summary := [][2]string{{"Subtotal", q.Subtotal}, {"Discount", q.DiscountTotal}}
	if invoice != nil {
		summary = [][2]string{{"Subtotal after discounts", q.Subtotal}, {"Tax", invoice.Tax}}
	}
	for _, row := range summary {
		p.CellFormat(100, 7, row[0], "", 0, "R", false, 0, "")
		p.CellFormat(78, 7, q.Currency+" "+row[1], "", 1, "R", false, 0, "")
	}
	p.SetFont("Sans", "B", 14)
	p.SetTextColor(81, 70, 223)
	p.CellFormat(100, 12, "Total", "", 0, "R", false, 0, "")
	p.CellFormat(78, 12, q.Currency+" "+q.Total, "", 1, "R", false, 0, "")
	if invoice != nil {
		p.SetFont("Sans", "", 10)
		for _, row := range [][2]string{{"Paid", invoice.Paid}, {"Outstanding", invoice.Outstanding}} {
			p.CellFormat(100, 7, row[0], "", 0, "R", false, 0, "")
			p.CellFormat(78, 7, q.Currency+" "+row[1], "", 1, "R", false, 0, "")
		}
	}
	p.Ln(8)
	if p.GetY() > 251 {
		p.AddPage()
	}
	p.SetTextColor(32, 37, 54)
	p.SetFont("Sans", "B", 10)
	p.CellFormat(178, 7, "Terms", "", 1, "L", false, 0, "")
	p.SetFont("Sans", "", 9)
	terms := q.Terms
	if terms == "" {
		terms = "No additional terms specified."
	}
	p.MultiCell(178, 5, terms, "", "L", false)
	if invoice == nil && (q.Status == "draft" || q.Status == "submitted" || q.Status == "rejected") {
		p.Ln(5)
		p.SetTextColor(125, 85, 35)
		p.MultiCell(178, 5, "This quote has not been approved for sending to the customer.", "", "L", false)
	}
	if invoice != nil && q.Status == "draft" {
		p.Ln(5)
		p.MultiCell(178, 5, "DRAFT - not issued for payment.", "", "L", false)
	}
	var out bytes.Buffer
	if e := p.Output(&out); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
