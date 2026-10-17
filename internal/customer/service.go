package customer

import (
	"ai-business-api/internal/user"
	"context"
	"encoding/hex"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"
)

type Store interface {
	List(context.Context, string, string, string, int, int) ([]Customer, int, error)
	Get(context.Context, string, string) (Customer, error)
	Save(context.Context, user.Context, Customer, *Customer, string) (Customer, error)
	Owners(context.Context, string, int, int) ([]Owner, int, error)
	Contacts(context.Context, string, string, int, int) ([]Contact, int, error)
	AddContact(context.Context, user.Context, string, ContactInput, string) (Contact, error)
	Activity(context.Context, string, string, int, int) ([]user.Audit, int, error)
}
type Service struct{ Store Store }

func UUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil
}
func text(s string, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
}
func email(s string) bool {
	if s == "" {
		return true
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && len(s) <= 254
}
func normalize(c *Customer) error {
	c.Name = strings.TrimSpace(c.Name)
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	c.Phone = strings.TrimSpace(c.Phone)
	c.Website = strings.TrimSpace(c.Website)
	c.Industry = strings.TrimSpace(c.Industry)
	c.Country = strings.TrimSpace(c.Country)
	c.Notes = strings.TrimSpace(c.Notes)
	if c.Name == "" || !text(c.Name, 200) || !email(c.Email) || !text(c.Phone, 50) || !text(c.Industry, 100) || !text(c.Country, 100) || !text(c.Notes, 10000) || !UUID(c.OwnerID) {
		return user.ErrInvalid
	}
	if c.Status != "active" && c.Status != "inactive" && c.Status != "prospect" {
		return user.ErrInvalid
	}
	if c.Website != "" {
		u, err := url.Parse(c.Website)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || !text(c.Website, 2048) {
			return user.ErrInvalid
		}
	}
	return nil
}
func (s *Service) List(ctx context.Context, a user.Context, q, status string, page, size int) ([]Customer, int, error) {
	if !a.Allows("customer.read") {
		return nil, 0, user.ErrForbidden
	}
	q = strings.TrimSpace(q)
	if !text(q, 200) || page < 1 || size < 1 || size > 100 || page > 1000000 || (status != "" && status != "active" && status != "inactive" && status != "prospect") {
		return nil, 0, user.ErrInvalid
	}
	return s.Store.List(ctx, a.Organization.ID, q, status, page, size)
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Customer, error) {
	if !a.Allows("customer.read") {
		return Customer{}, user.ErrForbidden
	}
	if !UUID(id) {
		return Customer{}, user.ErrInvalid
	}
	return s.Store.Get(ctx, a.Organization.ID, id)
}
func (s *Service) Create(ctx context.Context, a user.Context, in Input, rid string) (Customer, error) {
	if !a.Allows("customer.create") {
		return Customer{}, user.ErrForbidden
	}
	if in.OwnerID == "" {
		in.OwnerID = a.User.ID
	}
	if in.Status == "" {
		in.Status = "prospect"
	}
	c := Customer{Name: in.Name, Email: in.Email, Phone: in.Phone, Website: in.Website, Industry: in.Industry, Country: in.Country, OwnerID: in.OwnerID, Status: in.Status, Notes: in.Notes}
	if err := normalize(&c); err != nil {
		return c, err
	}
	return s.Store.Save(ctx, a, c, nil, rid)
}
func (s *Service) Update(ctx context.Context, a user.Context, id string, p Patch, rid string) (Customer, error) {
	if !a.Allows("customer.update") {
		return Customer{}, user.ErrForbidden
	}
	if !UUID(id) || p.Version < 1 {
		return Customer{}, user.ErrInvalid
	}
	before, err := s.Store.Get(ctx, a.Organization.ID, id)
	if err != nil {
		return Customer{}, err
	}
	if before.Version != p.Version {
		return Customer{}, ErrConflict
	}
	c := before
	for dst, src := range map[*string]*string{&c.Name: p.Name, &c.Email: p.Email, &c.Phone: p.Phone, &c.Website: p.Website, &c.Industry: p.Industry, &c.Country: p.Country, &c.OwnerID: p.OwnerID, &c.Status: p.Status, &c.Notes: p.Notes} {
		if src != nil {
			*dst = *src
		}
	}
	if err = normalize(&c); err != nil {
		return c, err
	}
	return s.Store.Save(ctx, a, c, &before, rid)
}
func (s *Service) Owners(ctx context.Context, a user.Context, page, size int) ([]Owner, int, error) {
	if !a.Allows("customer.read") {
		return nil, 0, user.ErrForbidden
	}
	return s.Store.Owners(ctx, a.Organization.ID, page, size)
}
func (s *Service) Contacts(ctx context.Context, a user.Context, id string, page, size int) ([]Contact, int, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return nil, 0, err
	}
	return s.Store.Contacts(ctx, a.Organization.ID, id, page, size)
}
func (s *Service) AddContact(ctx context.Context, a user.Context, id string, in ContactInput, rid string) (Contact, error) {
	if !a.Allows("customer.update") {
		return Contact{}, user.ErrForbidden
	}
	if !UUID(id) {
		return Contact{}, user.ErrInvalid
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	in.Title = strings.TrimSpace(in.Title)
	if in.Name == "" || !text(in.Name, 200) || !email(in.Email) || !text(in.Phone, 50) || !text(in.Title, 100) {
		return Contact{}, user.ErrInvalid
	}
	if _, err := s.Store.Get(ctx, a.Organization.ID, id); err != nil {
		return Contact{}, err
	}
	return s.Store.AddContact(ctx, a, id, in, rid)
}
func (s *Service) Activity(ctx context.Context, a user.Context, id string, page, size int) ([]user.Audit, int, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return nil, 0, err
	}
	return s.Store.Activity(ctx, a.Organization.ID, id, page, size)
}

// NormalizeImport applies the same field rules as interactive customer creation.
func NormalizeImport(c *Customer) error { return normalize(c) }
