package product

import (
	"ai-business-api/internal/user"
	"context"
	"testing"
)

func TestValidation(t *testing.T) {
	valid := func() Input {
		return Input{Name: " Product ", Unit: "hour", Active: true, Prices: []Price{{Currency: "usd", Amount: "100.50"}}}
	}
	v := valid()
	if Validate(&v) != nil || v.Name != "Product" || v.Prices[0].Currency != "USD" {
		t.Fatal("normalization failed")
	}
	for _, change := range []func(*Input){func(i *Input) { i.Prices = nil }, func(i *Input) { i.Unit = "" }, func(i *Input) { i.Prices[0].Amount = "-1" }, func(i *Input) { i.Prices[0].Amount = "0.001" }, func(i *Input) { i.Prices = append(i.Prices, Price{Currency: "USD", Amount: "3"}) }} {
		v = valid()
		change(&v)
		if Validate(&v) != user.ErrInvalid {
			t.Fatal("invalid product accepted")
		}
	}
	s := Service{}
	if _, e := s.Save(context.Background(), user.Context{}, "", v, "req"); e != user.ErrForbidden {
		t.Fatal("unauthorized product write")
	}
}
