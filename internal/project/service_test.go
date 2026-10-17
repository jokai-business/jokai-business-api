package project

import (
	"ai-business-api/internal/user"
	"context"
	"testing"
)

const actorID = "11111111-1111-4111-8111-111111111111"
const sourceID = "22222222-2222-4222-8222-222222222222"

type fakeStore struct {
	Store
	org   string
	saved bool
}

func (f *fakeStore) Create(_ context.Context, a user.Context, in Input, _ string) (Project, error) {
	f.org = a.Organization.ID
	f.saved = true
	return Project{Input: in}, nil
}
func input() Input {
	return Input{Name: " Portal delivery ", OpportunityID: sourceID, OwnerID: actorID, StartDate: "2026-09-01", TargetDate: "2026-10-01"}
}
func TestProjectValidationAndTenant(t *testing.T) {
	f := &fakeStore{}
	s := Service{f}
	a := user.Context{User: user.User{ID: actorID}, Organization: user.Organization{ID: "trusted-org"}}
	if _, e := s.Create(context.Background(), a, input(), "req"); e != user.ErrForbidden || f.saved {
		t.Fatal("unauthorized project create")
	}
	a.Permissions = []string{"project.update"}
	in := input()
	in.OwnerID = ""
	p, e := s.Create(context.Background(), a, in, "req")
	if e != nil || p.Name != "Portal delivery" || p.OwnerID != actorID || f.org != "trusted-org" {
		t.Fatal("normalization or tenant context failed")
	}
	for _, change := range []func(*Input){func(i *Input) { i.Name = " " }, func(i *Input) { i.TargetDate = "2026-08-01" }, func(i *Input) { i.StartDate = "2026-02-30" }, func(i *Input) { i.TargetDate = "0000-01-01" }, func(i *Input) { i.OwnerID = "foreign" }, func(i *Input) { i.QuoteID = "bad" }, func(i *Input) { i.Notes = "bad\x00" }} {
		in = input()
		change(&in)
		if Validate(&in) != user.ErrInvalid {
			t.Fatal("invalid project accepted")
		}
	}
}
func TestPartialUpdatesPreserveAndClear(t *testing.T) {
	p := Project{Input: input(), Status: "in_progress"}
	empty := ""
	name := "Updated"
	updated, e := ApplyPatch(p, Patch{Name: &name, TargetDate: &empty})
	if e != nil || updated.Name != name || updated.TargetDate != "" || updated.StartDate != p.StartDate || updated.Status != p.Status {
		t.Fatal("project PATCH lost fields")
	}
	invalid := "archived"
	if _, e = ApplyPatch(p, Patch{Status: &invalid}); e != user.ErrInvalid {
		t.Fatal("unknown status accepted")
	}
	task := Task{TaskInput: TaskInput{Title: "Build", AssigneeID: actorID, DueDate: "2026-10-01", Status: "todo", Priority: "high", Notes: "Keep"}}
	done := "done"
	u, e := ApplyTaskPatch(task, TaskPatch{AssigneeID: &empty, DueDate: &empty, Status: &done})
	if e != nil || u.AssigneeID != "" || u.DueDate != "" || u.Priority != "high" || u.Notes != "Keep" || u.Status != "done" {
		t.Fatal("task PATCH lost fields")
	}
}
func TestTaskValidationAndProgress(t *testing.T) {
	for _, in := range []TaskInput{{Title: "", Status: "todo", Priority: "normal"}, {Title: "Test", Status: "blocked", Priority: "normal"}, {Title: "Test", Status: "todo", Priority: "critical"}, {Title: "Test", Status: "todo", Priority: "normal", AssigneeID: "bad"}, {Title: "Test", Status: "todo", Priority: "normal", DueDate: "2026-02-30"}} {
		if ValidateTask(&in) != user.ErrInvalid {
			t.Fatal("invalid task accepted")
		}
	}
	if Progress(0, 0) != 0 || Progress(1, 3) != 33 || Progress(2, 2) != 100 {
		t.Fatal("progress calculation failed")
	}
}
func TestReadOnlyAndSalesCannotWriteDelivery(t *testing.T) {
	s := Service{}
	for _, permissions := range [][]string{{"project.read"}, {"opportunity.read", "opportunity.update", "quote.create"}} {
		a := user.Context{Permissions: permissions}
		if _, e := s.Update(context.Background(), a, sourceID, Patch{Version: 1}, "req"); e != user.ErrForbidden {
			t.Fatal("project write allowed")
		}
		if _, e := s.AddTask(context.Background(), a, sourceID, TaskInput{Title: "Test"}, "req"); e != user.ErrForbidden {
			t.Fatal("task write allowed")
		}
		if _, _, e := s.Sources(context.Background(), a, "", 1, 25); e != user.ErrForbidden {
			t.Fatal("sources exposed without write permission")
		}
	}
}
