package customer

import (
	"ai-business-api/internal/user"
	"context"
	"testing"
)

const actorID = "11111111-1111-4111-8111-111111111111"
const recordID = "22222222-2222-4222-8222-222222222222"

type fakeStore struct {
	org     string
	saved   bool
	current Customer
}

func (f *fakeStore) List(_ context.Context, org, _, _ string, _, _ int) ([]Customer, int, error) {
	f.org = org
	return []Customer{}, 0, nil
}
func (f *fakeStore) Get(_ context.Context, org, _ string) (Customer, error) {
	f.org = org
	return f.current, nil
}
func (f *fakeStore) Save(_ context.Context, a user.Context, c Customer, _ *Customer, _ string) (Customer, error) {
	f.saved = true
	f.org = a.Organization.ID
	return c, nil
}
func (f *fakeStore) Owners(context.Context, string, int, int) ([]Owner, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) Contacts(context.Context, string, string, int, int) ([]Contact, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) AddContact(context.Context, user.Context, string, ContactInput, string) (Contact, error) {
	f.saved = true
	return Contact{}, nil
}
func (f *fakeStore) Activity(context.Context, string, string, int, int) ([]user.Audit, int, error) {
	return nil, 0, nil
}
func TestCustomerPermissionsValidationAndTenant(t *testing.T) {
	store := &fakeStore{}
	s := Service{store}
	a := user.Context{User: user.User{ID: actorID}, Organization: user.Organization{ID: "trusted-org"}}
	if _, err := s.Create(context.Background(), a, Input{Name: "Acme"}, "req"); err != user.ErrForbidden || store.saved {
		t.Fatal("unauthorized write accepted")
	}
	a.Permissions = []string{"customer.read", "customer.create", "customer.update"}
	c, err := s.Create(context.Background(), a, Input{Name: " Acme ", Email: "SALES@example.test"}, "req")
	if err != nil || store.org != "trusted-org" || c.Name != "Acme" || c.Email != "sales@example.test" || c.OwnerID != actorID || c.Status != "prospect" {
		t.Fatalf("unexpected normalized customer: %v", err)
	}
	for _, in := range []Input{{Name: " "}, {Name: "Acme", Website: "javascript:alert(1)"}, {Name: "Acme", Email: "invalid"}, {Name: "Acme", OwnerID: "bad"}, {Name: "Acme", Status: "deleted"}, {Name: "Acme", Notes: "nul\x00"}} {
		store.saved = false
		if _, err = s.Create(context.Background(), a, in, "req"); err != user.ErrInvalid || store.saved {
			t.Fatal("invalid input accepted")
		}
	}
	if _, _, err = s.List(context.Background(), a, "", "", -1, 25); err != user.ErrInvalid {
		t.Fatal("negative page accepted")
	}
	if _, err = s.Get(context.Background(), a, "bad-id"); err != user.ErrInvalid {
		t.Fatal("invalid UUID accepted")
	}
}
func TestPatchPreservesOmittedFieldsAndRejectsStaleVersion(t *testing.T) {
	store := &fakeStore{current: Customer{ID: recordID, Name: "Before", Email: "before@example.test", OwnerID: actorID, Status: "active", Version: 2}}
	s := Service{store}
	a := user.Context{Organization: user.Organization{ID: "org"}, Permissions: []string{"customer.update"}}
	name := "After"
	c, err := s.Update(context.Background(), a, recordID, Patch{Name: &name, Version: 2}, "req")
	if err != nil || c.Name != "After" || c.Email != "before@example.test" {
		t.Fatal("patch erased an omitted field")
	}
	store.saved = false
	if _, err = s.Update(context.Background(), a, recordID, Patch{Name: &name, Version: 1}, "req"); err != ErrConflict || store.saved {
		t.Fatal("stale write accepted")
	}
}
func TestReadOnlyCannotAddContacts(t *testing.T) {
	s := Service{&fakeStore{}}
	a := user.Context{Permissions: []string{"customer.read"}}
	if _, err := s.AddContact(context.Background(), a, recordID, ContactInput{Name: "Jane"}, "req"); err != user.ErrForbidden {
		t.Fatal("read-only contact write accepted")
	}
}
