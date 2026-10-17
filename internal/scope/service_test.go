package scope

import (
	"ai-business-api/internal/user"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	in := Input{OpportunityID: "11111111-1111-4111-8111-111111111111", Title: "  Portal scope  ", Objectives: " Clear outcomes "}
	if e := Validate(&in); e != nil || in.Title != "Portal scope" {
		t.Fatal(e)
	}
	for _, title := range []string{" ", strings.Repeat("x", 201), "bad\x00"} {
		in.Title = title
		if Validate(&in) == nil {
			t.Fatal(title)
		}
	}
	c := ContributionInput{Team: " Engineering ", Service: "Portal", Deliverables: "Login and account pages", Version: 1}
	if e := ValidateContribution(&c); e != nil || c.Team != "Engineering" {
		t.Fatal(e)
	}
	c.Deliverables = " "
	if ValidateContribution(&c) == nil {
		t.Fatal("empty deliverables")
	}
}
func TestTransitions(t *testing.T) {
	v := Scope{Input: Input{Objectives: "Deliver portal"}, Status: "draft", CreatorID: "sales", EditedBy: "sales", Contributions: []Contribution{{UpdatedBy: "delivery"}}}
	reviewer := user.Context{User: user.User{ID: "manager"}}
	if got, e := Transition(v, reviewer, "submit", ""); e != nil || got != "in_review" {
		t.Fatal(got, e)
	}
	v.Status = "in_review"
	v.SubmittedBy = "sales"
	for _, id := range []string{"sales", "delivery"} {
		if _, e := Transition(v, user.Context{User: user.User{ID: id}}, "approve", ""); !errors.Is(e, ErrSelfReview) {
			t.Fatal(id, e)
		}
	}
	if _, e := Transition(v, reviewer, "request-changes", ""); !errors.Is(e, user.ErrInvalid) {
		t.Fatal(e)
	}
	if got, e := Transition(v, reviewer, "request-changes", "Define acceptance"); e != nil || got != "changes_requested" {
		t.Fatal(got, e)
	}
	if got, e := Transition(v, reviewer, "approve", ""); e != nil || got != "approved" {
		t.Fatal(got, e)
	}
	v.PreparedBy = []string{"manager"}
	if _, e := Transition(v, reviewer, "approve", ""); !errors.Is(e, ErrSelfReview) {
		t.Fatal("previous preparer was allowed", e)
	}
	v.PreparedBy = nil
	v.Status = "approved"
	if _, e := Transition(v, reviewer, "submit", ""); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if got, e := Transition(v, reviewer, "revise", ""); e != nil || got != "draft" {
		t.Fatal(got, e)
	}
}
func TestIncompleteAndPermissions(t *testing.T) {
	if _, e := Transition(Scope{Status: "draft"}, user.Context{}, "submit", ""); !errors.Is(e, ErrIncomplete) {
		t.Fatal(e)
	}
	s := Service{}
	a := user.Context{}
	ctx := context.Background()
	if _, e := s.Create(ctx, a, Input{}, ""); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Get(ctx, a, ""); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Contribution(ctx, a, "", "", ContributionInput{}, false, ""); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Act(ctx, user.Context{Permissions: []string{"scope.update"}}, "", "approve", Action{}, ""); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Snapshot(ctx, a, "", 1); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
}
