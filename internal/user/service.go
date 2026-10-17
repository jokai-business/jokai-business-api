package user

import (
	"context"
	"net/mail"
	"strings"
	"unicode/utf8"
)

type Store interface {
	Resolve(context.Context, string) (Context, error)
	List(context.Context, string, int, int) ([]User, int, error)
	Create(context.Context, Context, CreateRequest, string) (User, error)
	Login(context.Context, Context, string, string) error
	Audit(context.Context, string, int, int) ([]Audit, int, error)
}
type Service struct{ Store Store }

func (s *Service) Resolve(ctx context.Context, subject string) (Context, error) {
	return s.Store.Resolve(ctx, subject)
}
func (s *Service) List(ctx context.Context, actor Context, page, size int) ([]User, int, error) {
	if !actor.Allows("user.manage") {
		return nil, 0, ErrForbidden
	}
	return s.Store.List(ctx, actor.Organization.ID, page, size)
}
func (s *Service) Create(ctx context.Context, actor Context, req CreateRequest, id string) (User, error) {
	if !actor.Allows("user.manage") {
		return User{}, ErrForbidden
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Subject = strings.TrimSpace(req.Subject)
	address, err := mail.ParseAddress(req.Email)
	if err != nil || address.Address != req.Email || len(req.Email) > 254 || req.Name == "" || utf8.RuneCountInString(req.Name) > 200 || req.Subject == "" || len(req.Subject) > 200 {
		return User{}, ErrInvalid
	}
	switch req.Role {
	case "organization_admin", "sales_manager", "sales_user", "delivery_user", "finance_user", "executive":
	default:
		return User{}, ErrInvalid
	}
	return s.Store.Create(ctx, actor, req, id)
}
func (s *Service) Login(ctx context.Context, actor Context, session, id string) error {
	return s.Store.Login(ctx, actor, session, id)
}
func (s *Service) Audit(ctx context.Context, actor Context, page, size int) ([]Audit, int, error) {
	if !actor.Allows("audit.read") {
		return nil, 0, ErrForbidden
	}
	return s.Store.Audit(ctx, actor.Organization.ID, page, size)
}
