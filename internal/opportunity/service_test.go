package opportunity

import (
	"ai-business-api/internal/user"
	"context"
	"testing"
)

const actorID = "11111111-1111-4111-8111-111111111111"
const recordID = "22222222-2222-4222-8222-222222222222"

type fakeStore struct {
	Store
	org     string
	current Opportunity
	saved   bool
}

func (f *fakeStore) Get(_ context.Context, org, _ string) (Opportunity, error) {
	f.org = org
	return f.current, nil
}
func (f *fakeStore) Save(_ context.Context, a user.Context, v Opportunity, _ *Opportunity, _ string) (Opportunity, error) {
	f.org = a.Organization.ID
	f.saved = true
	return v, nil
}
func validInput() Input {
	return Input{Title: "Migration", CustomerID: recordID, StageID: recordID, OwnerID: actorID, ExpectedValue: "123.45", Currency: "USD", Source: "Manual", Probability: 50}
}
func TestValidationProtectsMoneyAndDates(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Input)
	}{
		{"negative", func(v *Input) { v.ExpectedValue = "-1" }}, {"precision", func(v *Input) { v.ExpectedValue = "1.001" }}, {"exponent", func(v *Input) { v.ExpectedValue = "1e8" }}, {"overflow", func(v *Input) { v.ExpectedValue = "100000000000000" }}, {"currency", func(v *Input) { v.Currency = "DOLLARS" }}, {"probability", func(v *Input) { v.Probability = 101 }}, {"invalid date", func(v *Input) { v.ExpectedCloseDate = "2026-02-30" }}, {"zero year", func(v *Input) { v.ExpectedCloseDate = "0000-01-01" }}, {"owner", func(v *Input) { v.OwnerID = "bad" }}, {"source", func(v *Input) { v.Source = "Telegram" }}, {"null byte", func(v *Input) { v.Requirement = "bad\x00" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := validInput()
			tt.change(&v)
			if normalize(&v) != user.ErrInvalid {
				t.Fatal("invalid value accepted")
			}
		})
	}
	v := validInput()
	v.ExpectedValue = "99999999999999.99"
	v.Currency = "usd"
	v.ExpectedCloseDate = "2028-02-29"
	if normalize(&v) != nil || v.Currency != "USD" {
		t.Fatal("valid decimal or leap day rejected")
	}
}
func TestPermissionsDefaultsAndTenant(t *testing.T) {
	ctx := context.Background()
	f := &fakeStore{}
	s := Service{f}
	a := user.Context{User: user.User{ID: actorID}, Organization: user.Organization{ID: "trusted-org"}}
	if _, e := s.Create(ctx, a, validInput(), "req"); e != user.ErrForbidden {
		t.Fatal("unauthorized create")
	}
	if _, e := s.SaveStage(ctx, a, "", StageInput{Name: "Review"}, "req"); e != user.ErrForbidden {
		t.Fatal("unauthorized stage configuration")
	}
	a.Permissions = []string{"opportunity.create"}
	v := validInput()
	v.OwnerID = ""
	v.Source = ""
	v.ExpectedValue = ""
	v.Currency = ""
	out, e := s.Create(ctx, a, v, "req")
	if e != nil || out.OwnerID != actorID || out.Source != "Manual" || out.Currency != "USD" || f.org != "trusted-org" {
		t.Fatal("defaults or tenant scope failed")
	}
	a.Permissions = []string{"opportunity.read"}
	if _, e = s.Update(ctx, a, recordID, Patch{Version: 1}, "req"); e != user.ErrForbidden {
		t.Fatal("read-only write")
	}
	if _, e = s.AddActivity(ctx, a, recordID, "Note", "req"); e != user.ErrForbidden {
		t.Fatal("read-only note")
	}
}
func TestPatchPreservesFieldsAndRejectsStaleEdit(t *testing.T) {
	f := &fakeStore{current: Opportunity{Input: validInput(), ID: recordID, Version: 2}}
	f.current.ExpectedCloseDate = "2026-12-01"
	s := Service{f}
	a := user.Context{Organization: user.Organization{ID: "org"}, Permissions: []string{"opportunity.update"}}
	title := "Changed"
	empty := ""
	v, e := s.Update(context.Background(), a, recordID, Patch{Title: &title, ExpectedCloseDate: &empty, Version: 2}, "req")
	if e != nil || v.Title != title || v.ExpectedCloseDate != "" || v.ExpectedValue != "123.45" || v.CustomerID != recordID {
		t.Fatal("PATCH lost omitted fields or failed to clear date")
	}
	f.saved = false
	if _, e = s.Update(context.Background(), a, recordID, Patch{Version: 1}, "req"); e != ErrConflict || f.saved {
		t.Fatal("stale edit saved")
	}
}
