package invoice

import (
	"ai-business-api/internal/user"
	"context"
	"errors"
	"testing"
)

func TestExactTotals(t *testing.T) {
	for _, tc := range []struct{ s, t, w string }{{"0.10", "0.20", "0.30"}, {"2250.90", "225.09", "2475.99"}, {"99999999999999.98", "0.01", "99999999999999.99"}} {
		v, e := Total(tc.s, tc.t)
		if e != nil || v != tc.w {
			t.Fatalf("%+v: %s %v", tc, v, e)
		}
	}
	for _, tc := range [][2]string{{"0", "0"}, {"-1", "0"}, {"1.001", "0"}, {"99999999999999.99", "0.01"}, {"NaN", "0"}} {
		if _, e := Total(tc[0], tc[1]); e == nil {
			t.Fatal(tc)
		}
	}
}
func TestStatus(t *testing.T) {
	for _, tc := range []struct {
		state, due  string
		total, paid int64
		want        string
	}{{"draft", "2020-01-01", 100, 0, "draft"}, {"issued", "2020-01-01", 100, 100, "paid"}, {"sent", "2020-01-01", 100, 50, "overdue"}, {"sent", "2099-01-01", 100, 50, "partially_paid"}, {"issued", "2099-01-01", 100, 0, "issued"}, {"sent", "2026-09-29", 100, 0, "sent"}} {
		if got := Status(tc.state, tc.due, tc.total, tc.paid, "2026-09-29"); got != tc.want {
			t.Fatalf("%+v got %s", tc, got)
		}
	}
}
func TestValidation(t *testing.T) {
	in := Input{QuoteID: "11111111-1111-4111-8111-111111111111", IssueDate: "2026-09-01", DueDate: "2026-10-01"}
	if e := Validate(&in); e != nil || in.Tax != "0.00" {
		t.Fatal(e)
	}
	in.DueDate = "2026-02-30"
	if Validate(&in) == nil {
		t.Fatal("invalid date")
	}
	p := PaymentInput{Amount: "1.2", PaymentDate: "2026-09-01", Method: "cash", IdempotencyKey: in.QuoteID}
	if e := ValidatePayment(&p); e != nil || p.Amount != "1.20" {
		t.Fatal(e)
	}
	p.Amount = "0"
	if ValidatePayment(&p) == nil {
		t.Fatal("zero payment")
	}
	p.Amount = "1"
	p.PaymentDate = "2099-01-01"
	if ValidatePayment(&p) == nil {
		t.Fatal("future payment")
	}
}
func TestPermissionsBeforeDatabase(t *testing.T) {
	s := Service{}
	ctx := context.Background()
	a := user.Context{}
	if _, e := s.Create(ctx, a, Input{}, ""); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Pay(ctx, a, "", PaymentInput{}, ""); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Update(ctx, a, "", Patch{}, ""); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Dashboard(ctx, a); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
}
