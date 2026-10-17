package invoice

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var money = regexp.MustCompile(`^(0|[1-9][0-9]{0,13})(\.[0-9]{1,2})?$`)

func cents(s string) (int64, error) {
	if !money.MatchString(s) {
		return 0, user.ErrInvalid
	}
	p := strings.Split(s, ".")
	f := ""
	if len(p) > 1 {
		f = p[1]
	}
	return strconv.ParseInt(p[0]+f+strings.Repeat("0", 2-len(f)), 10, 64)
}
func decimal(n int64) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }
func Validate(in *Input) error {
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Tax == "" {
		in.Tax = "0.00"
	}
	if !customer.UUID(in.QuoteID) || (in.ProjectID != "" && !customer.UUID(in.ProjectID)) || !product.Text(in.Notes, 10000) {
		return user.ErrInvalid
	}
	a, e := time.Parse("2006-01-02", in.IssueDate)
	b, f := time.Parse("2006-01-02", in.DueDate)
	if e != nil || f != nil || a.Year() < 1 || b.Before(a) {
		return user.ErrInvalid
	}
	_, e = cents(in.Tax)
	return e
}
func Total(subtotal, tax string) (string, error) {
	s, e := cents(subtotal)
	t, f := cents(tax)
	if e != nil || f != nil || s+t <= 0 || s+t > 9999999999999999 {
		return "", user.ErrInvalid
	}
	return decimal(s + t), nil
}
func ValidatePayment(p *PaymentInput) error {
	p.Reference = strings.TrimSpace(p.Reference)
	p.Notes = strings.TrimSpace(p.Notes)
	n, e := cents(p.Amount)
	d, f := time.Parse("2006-01-02", p.PaymentDate)
	if e != nil || f != nil || n <= 0 || d.Year() < 1 || p.PaymentDate > time.Now().UTC().Format("2006-01-02") || !customer.UUID(p.IdempotencyKey) || !product.Text(p.Reference, 200) || !product.Text(p.Notes, 2000) {
		return user.ErrInvalid
	}
	switch p.Method {
	case "bank_transfer", "cash", "card", "other":
	default:
		return user.ErrInvalid
	}
	p.Amount = decimal(n)
	return nil
}
func Status(state, due string, total, paid int64, today string) string {
	if state == "draft" {
		return "draft"
	}
	if paid >= total {
		return "paid"
	}
	if due < today {
		return "overdue"
	}
	if paid > 0 {
		return "partially_paid"
	}
	return state
}
