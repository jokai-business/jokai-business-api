package user

import (
	"context"
	"testing"
)

type fakeStore struct {
	org     string
	created bool
	request CreateRequest
}

func (f *fakeStore) Resolve(context.Context, string) (Context, error) { return Context{}, nil }
func (f *fakeStore) List(_ context.Context, org string, _, _ int) ([]User, int, error) {
	f.org = org
	return []User{}, 0, nil
}
func (f *fakeStore) Create(_ context.Context, c Context, r CreateRequest, _ string) (User, error) {
	f.org = c.Organization.ID
	f.created = true
	f.request = r
	return User{}, nil
}
func (f *fakeStore) Login(context.Context, Context, string, string) error { return nil }
func (f *fakeStore) Audit(_ context.Context, org string, _, _ int) ([]Audit, int, error) {
	f.org = org
	return []Audit{}, 0, nil
}
func TestPermissionsAndTenant(t *testing.T) {
	store := &fakeStore{}
	s := Service{store}
	actor := Context{Organization: Organization{ID: "trusted-org"}}
	if _, _, err := s.List(context.Background(), actor, 1, 25); err != ErrForbidden {
		t.Fatal("unprivileged list allowed")
	}
	valid := CreateRequest{Subject: "verified-subject", Name: " Jane ", Email: "JANE@example.test", Role: "sales_user"}
	if _, err := s.Create(context.Background(), actor, valid, "req_test"); err != ErrForbidden || store.created {
		t.Fatal("unprivileged create allowed")
	}
	actor.Permissions = []string{"user.manage"}
	if _, err := s.Create(context.Background(), actor, valid, "req_test"); err != nil {
		t.Fatal(err)
	}
	if store.org != "trusted-org" || store.request.Email != "jane@example.test" || store.request.Name != "Jane" {
		t.Fatal("invalid tenant or normalization")
	}
	valid.Role = "super_admin"
	store.created = false
	if _, err := s.Create(context.Background(), actor, valid, "req_test"); err != ErrInvalid || store.created {
		t.Fatal("invalid role accepted")
	}
}
