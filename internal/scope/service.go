package scope

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"strings"
)

func Validate(in *Input) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Objectives = strings.TrimSpace(in.Objectives)
	if !customer.UUID(in.OpportunityID) || in.Title == "" || !product.Text(in.Title, 200) || !product.Text(in.Objectives, 10000) {
		return user.ErrInvalid
	}
	return nil
}
func ValidateContribution(in *ContributionInput) error {
	for _, v := range []*string{&in.Team, &in.Service, &in.Deliverables, &in.Assumptions, &in.Exclusions, &in.Timeline} {
		*v = strings.TrimSpace(*v)
	}
	if in.Version < 1 || in.Team == "" || in.Service == "" || in.Deliverables == "" || !product.Text(in.Team, 100) || !product.Text(in.Service, 200) || !product.Text(in.Deliverables, 10000) || !product.Text(in.Assumptions, 5000) || !product.Text(in.Exclusions, 5000) || !product.Text(in.Timeline, 2000) {
		return user.ErrInvalid
	}
	return nil
}
func Editable(status string) bool { return status == "draft" || status == "changes_requested" }
func Transition(v Scope, a user.Context, action, note string) (string, error) {
	switch action {
	case "submit":
		if !Editable(v.Status) {
			return "", ErrConflict
		}
		if strings.TrimSpace(v.Objectives) == "" || len(v.Contributions) == 0 {
			return "", ErrIncomplete
		}
		return "in_review", nil
	case "approve", "request-changes":
		for _, id := range v.PreparedBy {
			if a.User.ID == id {
				return "", ErrSelfReview
			}
		}
		if v.Status != "in_review" {
			return "", ErrConflict
		}
		if a.User.ID == v.SubmittedBy || a.User.ID == v.CreatorID || a.User.ID == v.EditedBy {
			return "", ErrSelfReview
		}
		for _, c := range v.Contributions {
			if c.UpdatedBy == a.User.ID {
				return "", ErrSelfReview
			}
		}
		if action == "request-changes" {
			if note == "" {
				return "", user.ErrInvalid
			}
			return "changes_requested", nil
		}
		return "approved", nil
	case "revise":
		if v.Status != "approved" {
			return "", ErrConflict
		}
		return "draft", nil
	}
	return "", user.ErrInvalid
}
func pageValid(p, z int) bool { return p >= 1 && p <= 1000000 && z >= 1 && z <= 100 }
