package project

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"context"
	"strings"
	"time"
)

type Store interface {
	Sources(context.Context, string, string, int, int) ([]Source, int, error)
	List(context.Context, string, string, string, string, string, int, int) ([]Project, int, error)
	Get(context.Context, string, string) (Project, error)
	Create(context.Context, user.Context, Input, string) (Project, error)
	Update(context.Context, user.Context, string, Patch, string) (Project, error)
	Tasks(context.Context, string, string, string, string, int, int) ([]Task, int, error)
	AddTask(context.Context, user.Context, string, TaskInput, string) (Task, error)
	UpdateTask(context.Context, user.Context, string, string, TaskPatch, string) (Task, error)
	Activity(context.Context, string, string, int, int) ([]user.Audit, int, error)
}
type Service struct{ Store Store }

func Date(s string) bool {
	if s == "" {
		return true
	}
	d, e := time.Parse("2006-01-02", s)
	return e == nil && d.Year() > 0
}
func status(s string) bool {
	return s == "not_started" || s == "in_progress" || s == "blocked" || s == "completed"
}
func taskStatus(s string) bool { return s == "todo" || s == "in_progress" || s == "done" }
func validPage(p, z int) bool  { return p >= 1 && p <= 1000000 && z >= 1 && z <= 100 }
func Validate(in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Name == "" || !product.Text(in.Name, 200) || !product.Text(in.Notes, 10000) || !customer.UUID(in.OwnerID) || !customer.UUID(in.OpportunityID) || (in.QuoteID != "" && !customer.UUID(in.QuoteID)) || !Date(in.StartDate) || !Date(in.TargetDate) || (in.StartDate != "" && in.TargetDate != "" && in.TargetDate < in.StartDate) {
		return user.ErrInvalid
	}
	return nil
}
func ValidateTask(in *TaskInput) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Title == "" || !product.Text(in.Title, 200) || !product.Text(in.Notes, 5000) || (in.AssigneeID != "" && !customer.UUID(in.AssigneeID)) || !Date(in.DueDate) || !taskStatus(in.Status) || (in.Priority != "low" && in.Priority != "normal" && in.Priority != "high" && in.Priority != "urgent") {
		return user.ErrInvalid
	}
	return nil
}
func ApplyPatch(p Project, in Patch) (Project, error) {
	for dst, src := range map[*string]*string{&p.Name: in.Name, &p.OwnerID: in.OwnerID, &p.StartDate: in.StartDate, &p.TargetDate: in.TargetDate, &p.Notes: in.Notes, &p.Status: in.Status} {
		if src != nil {
			*dst = *src
		}
	}
	if !status(p.Status) {
		return p, user.ErrInvalid
	}
	return p, Validate(&p.Input)
}
func ApplyTaskPatch(t Task, in TaskPatch) (Task, error) {
	for dst, src := range map[*string]*string{&t.Title: in.Title, &t.AssigneeID: in.AssigneeID, &t.DueDate: in.DueDate, &t.Status: in.Status, &t.Priority: in.Priority, &t.Notes: in.Notes} {
		if src != nil {
			*dst = *src
		}
	}
	return t, ValidateTask(&t.TaskInput)
}
func Progress(done, total int) int {
	if total == 0 {
		return 0
	}
	return done * 100 / total
}
func (s *Service) Sources(ctx context.Context, a user.Context, q string, p, z int) ([]Source, int, error) {
	if !a.Allows("project.update") {
		return nil, 0, user.ErrForbidden
	}
	if !product.Text(q, 200) || !validPage(p, z) {
		return nil, 0, user.ErrInvalid
	}
	return s.Store.Sources(ctx, a.Organization.ID, strings.TrimSpace(q), p, z)
}
func (s *Service) List(ctx context.Context, a user.Context, q, st, customerID, opportunityID string, p, z int) ([]Project, int, error) {
	if !a.Allows("project.read") {
		return nil, 0, user.ErrForbidden
	}
	if !product.Text(q, 200) || (st != "" && !status(st)) || (customerID != "" && !customer.UUID(customerID)) || (opportunityID != "" && !customer.UUID(opportunityID)) || !validPage(p, z) {
		return nil, 0, user.ErrInvalid
	}
	return s.Store.List(ctx, a.Organization.ID, strings.TrimSpace(q), st, customerID, opportunityID, p, z)
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Project, error) {
	if !a.Allows("project.read") {
		return Project{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Project{}, user.ErrInvalid
	}
	return s.Store.Get(ctx, a.Organization.ID, id)
}
func (s *Service) Create(ctx context.Context, a user.Context, in Input, rid string) (Project, error) {
	if !a.Allows("project.update") {
		return Project{}, user.ErrForbidden
	}
	if in.OwnerID == "" {
		in.OwnerID = a.User.ID
	}
	if e := Validate(&in); e != nil {
		return Project{}, e
	}
	return s.Store.Create(ctx, a, in, rid)
}
func (s *Service) Update(ctx context.Context, a user.Context, id string, in Patch, rid string) (Project, error) {
	if !a.Allows("project.update") {
		return Project{}, user.ErrForbidden
	}
	if !customer.UUID(id) || in.Version < 1 {
		return Project{}, user.ErrInvalid
	}
	return s.Store.Update(ctx, a, id, in, rid)
}
func (s *Service) Tasks(ctx context.Context, a user.Context, id, st, assignee string, p, z int) ([]Task, int, error) {
	if _, e := s.Get(ctx, a, id); e != nil {
		return nil, 0, e
	}
	if (st != "" && !taskStatus(st)) || (assignee != "" && !customer.UUID(assignee)) || !validPage(p, z) {
		return nil, 0, user.ErrInvalid
	}
	return s.Store.Tasks(ctx, a.Organization.ID, id, st, assignee, p, z)
}
func (s *Service) AddTask(ctx context.Context, a user.Context, id string, in TaskInput, rid string) (Task, error) {
	if !a.Allows("project.update") {
		return Task{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Task{}, user.ErrInvalid
	}
	if in.Status == "" {
		in.Status = "todo"
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if e := ValidateTask(&in); e != nil {
		return Task{}, e
	}
	return s.Store.AddTask(ctx, a, id, in, rid)
}
func (s *Service) UpdateTask(ctx context.Context, a user.Context, id, taskID string, in TaskPatch, rid string) (Task, error) {
	if !a.Allows("project.update") {
		return Task{}, user.ErrForbidden
	}
	if !customer.UUID(id) || !customer.UUID(taskID) || in.Version < 1 {
		return Task{}, user.ErrInvalid
	}
	return s.Store.UpdateTask(ctx, a, id, taskID, in, rid)
}
func (s *Service) Activity(ctx context.Context, a user.Context, id string, p, z int) ([]user.Audit, int, error) {
	if _, e := s.Get(ctx, a, id); e != nil {
		return nil, 0, e
	}
	if !validPage(p, z) {
		return nil, 0, user.ErrInvalid
	}
	return s.Store.Activity(ctx, a.Organization.ID, id, p, z)
}
