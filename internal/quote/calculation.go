package quote

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var quantity = regexp.MustCompile(`^(0|[1-9][0-9]{0,6})(\.[0-9]{1,3})?$`)
var percent = regexp.MustCompile(`^(0|[1-9][0-9]?|100)(\.[0-9]{1,2})?$`)

func scaled(s string, n int) *big.Int {
	parts := strings.Split(s, ".")
	fraction := ""
	if len(parts) > 1 {
		fraction = parts[1]
	}
	v, _ := new(big.Int).SetString(parts[0]+fraction+strings.Repeat("0", n-len(fraction)), 10)
	return v
}
func rounded(v *big.Int, divisor int64) *big.Int {
	return new(big.Int).Quo(new(big.Int).Add(v, big.NewInt(divisor/2)), big.NewInt(divisor))
}
func decimal(v *big.Int) string { n := v.Int64(); return fmt.Sprintf("%d.%02d", n/100, n%100) }
func Calculate(in *Input) ([]Item, string, string, string, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Terms = strings.TrimSpace(in.Terms)
	date, e := time.Parse("2006-01-02", in.ExpiryDate)
	if !customer.UUID(in.OpportunityID) || !product.Currency.MatchString(in.Currency) || e != nil || date.Year() < 1 || !product.Text(in.Terms, 10000) || len(in.Items) < 1 || len(in.Items) > 50 {
		return nil, "", "", "", user.ErrInvalid
	}
	gross, discount, total := new(big.Int), new(big.Int), new(big.Int)
	limit := big.NewInt(9999999999999999)
	out := []Item{}
	for _, item := range in.Items {
		if item.DiscountPercent == "" {
			item.DiscountPercent = "0"
		}
		if !customer.UUID(item.ProductID) || !quantity.MatchString(item.Quantity) || !product.Money.MatchString(item.UnitPrice) || !percent.MatchString(item.DiscountPercent) {
			return nil, "", "", "", user.ErrInvalid
		}
		qty, price, pct := scaled(item.Quantity, 3), scaled(item.UnitPrice, 2), scaled(item.DiscountPercent, 2)
		if qty.Sign() <= 0 || pct.Cmp(big.NewInt(10000)) > 0 {
			return nil, "", "", "", user.ErrInvalid
		}
		g := rounded(new(big.Int).Mul(qty, price), 1000)
		d := rounded(new(big.Int).Mul(g, pct), 10000)
		t := new(big.Int).Sub(g, d)
		gross.Add(gross, g)
		discount.Add(discount, d)
		total.Add(total, t)
		if gross.Cmp(limit) > 0 {
			return nil, "", "", "", user.ErrInvalid
		}
		out = append(out, Item{ItemInput: item, Subtotal: decimal(g), DiscountAmount: decimal(d), Total: decimal(t)})
	}
	return out, decimal(gross), decimal(discount), decimal(total), nil
}
func Transition(status, action string) string {
	switch action {
	case "submit":
		if status == "draft" {
			return "submitted"
		}
	case "approve":
		if status == "submitted" {
			return "approved"
		}
	case "reject":
		if status == "submitted" {
			return "rejected"
		}
	case "mark-sent":
		if status == "approved" {
			return "sent"
		}
	case "accept":
		if status == "sent" {
			return "accepted"
		}
	}
	return ""
}
