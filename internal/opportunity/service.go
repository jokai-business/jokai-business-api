package opportunity

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Store interface {
	List(context.Context, string, string, string, string, int, int) ([]Opportunity, int, error)
	Get(context.Context, string, string) (Opportunity, error)
	Save(context.Context, user.Context, Opportunity, *Opportunity, string) (Opportunity, error)
	Stages(context.Context, string) ([]Stage, error)
	SaveStage(context.Context, user.Context, string, StageInput, string) (Stage, error)
	Activities(context.Context, string, string, int, int) ([]Activity, int, error)
	AddActivity(context.Context, user.Context, string, string, string) (Activity, error)
}
type Service struct{ Store Store }

var money = regexp.MustCompile(`^(0|[1-9][0-9]{0,13})(\.[0-9]{1,2})?$`)
var currency = regexp.MustCompile(`^[A-Z]{3}$`)

func validText(s string, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
}
func normalize(in *Input) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Requirement = strings.TrimSpace(in.Requirement)
	in.NextAction = strings.TrimSpace(in.NextAction)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Title == "" || !validText(in.Title, 200) || !validText(in.Requirement, 10000) || !validText(in.NextAction, 2000) || !customer.UUID(in.CustomerID) || !customer.UUID(in.OwnerID) || !customer.UUID(in.StageID) || !money.MatchString(in.ExpectedValue) || !currency.MatchString(in.Currency) || in.Probability < 0 || in.Probability > 100 {
		return user.ErrInvalid
	}
	if in.Source != "Manual" && in.Source != "Gmail" && in.Source != "Excel" && in.Source != "Document" {
		return user.ErrInvalid
	}
	if in.ExpectedCloseDate != "" {
		d, e := time.Parse("2006-01-02", in.ExpectedCloseDate)
		if e != nil || d.Year() < 1 {
			return user.ErrInvalid
		}
	}
	return nil
}
func (s *Service) List(ctx context.Context, a user.Context, q, stage, customerID string, p, z int) ([]Opportunity, int, error) {
	if !a.Allows("opportunity.read") {
		return nil, 0, user.ErrForbidden
	}
	if !validText(q, 200) || p < 1 || p > 1000000 || z < 1 || z > 100 || (stage != "" && !customer.UUID(stage)) || (customerID != "" && !customer.UUID(customerID)) {
		return nil, 0, user.ErrInvalid
	}
	return s.Store.List(ctx, a.Organization.ID, strings.TrimSpace(q), stage, customerID, p, z)
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Opportunity, error) {
	if !a.Allows("opportunity.read") {
		return Opportunity{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Opportunity{}, user.ErrInvalid
	}
	return s.Store.Get(ctx, a.Organization.ID, id)
}
func (s *Service) Create(ctx context.Context, a user.Context, in Input, rid string) (Opportunity, error) {
	if !a.Allows("opportunity.create") {
		return Opportunity{}, user.ErrForbidden
	}
	if in.OwnerID == "" {
		in.OwnerID = a.User.ID
	}
	if in.Source == "" {
		in.Source = "Manual"
	}
	if in.ExpectedValue == "" {
		in.ExpectedValue = "0"
	}
	if in.Currency == "" {
		in.Currency = a.Organization.DefaultCurrency
		if in.Currency == "" {
			in.Currency = "USD"
		}
	}
	if e := normalize(&in); e != nil {
		return Opportunity{}, e
	}
	return s.Store.Save(ctx, a, Opportunity{Input: in}, nil, rid)
}
func (s *Service) Update(ctx context.Context, a user.Context, id string, p Patch, rid string) (Opportunity, error) {
	if !a.Allows("opportunity.update") {
		return Opportunity{}, user.ErrForbidden
	}
	if !customer.UUID(id) || p.Version < 1 {
		return Opportunity{}, user.ErrInvalid
	}
	before, e := s.Store.Get(ctx, a.Organization.ID, id)
	if e != nil {
		return Opportunity{}, e
	}
	if before.Version != p.Version {
		return Opportunity{}, ErrConflict
	}
	v := before
	for dst, src := range map[*string]*string{&v.CustomerID: p.CustomerID, &v.Title: p.Title, &v.Requirement: p.Requirement, &v.StageID: p.StageID, &v.ExpectedValue: p.ExpectedValue, &v.Currency: p.Currency, &v.ExpectedCloseDate: p.ExpectedCloseDate, &v.OwnerID: p.OwnerID, &v.Source: p.Source, &v.NextAction: p.NextAction} {
		if src != nil {
			*dst = *src
		}
	}
	if p.Probability != nil {
		v.Probability = *p.Probability
	}
	if e = normalize(&v.Input); e != nil {
		return Opportunity{}, e
	}
	return s.Store.Save(ctx, a, v, &before, rid)
}
func (s *Service) Stages(ctx context.Context, a user.Context) ([]Stage, error) {
	if !a.Allows("opportunity.read") {
		return nil, user.ErrForbidden
	}
	return s.Store.Stages(ctx, a.Organization.ID)
}
func (s *Service) SaveStage(ctx context.Context, a user.Context, id string, in StageInput, rid string) (Stage, error) {
	if !a.Allows("pipeline.manage") {
		return Stage{}, user.ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || !validText(in.Name, 100) || in.Position < 0 || in.Position > 10000 || (in.IsWon && !in.IsClosed) || (id != "" && (!customer.UUID(id) || in.Version < 1)) {
		return Stage{}, user.ErrInvalid
	}
	return s.Store.SaveStage(ctx, a, id, in, rid)
}
func (s *Service) Activities(ctx context.Context, a user.Context, id string, p, z int) ([]Activity, int, error) {
	if _, e := s.Get(ctx, a, id); e != nil {
		return nil, 0, e
	}
	return s.Store.Activities(ctx, a.Organization.ID, id, p, z)
}
func (s *Service) AddActivity(ctx context.Context, a user.Context, id, body, rid string) (Activity, error) {
	if !a.Allows("opportunity.update") {
		return Activity{}, user.ErrForbidden
	}
	body = strings.TrimSpace(body)
	if body == "" || !validText(body, 5000) {
		return Activity{}, user.ErrInvalid
	}
	if _, e := s.Get(ctx, a, id); e != nil {
		return Activity{}, e
	}
	return s.Store.AddActivity(ctx, a, id, body, rid)
}

// NormalizeImport applies the same field rules as interactive opportunity creation.
func NormalizeImport(in *Input) error { return normalize(in) }
