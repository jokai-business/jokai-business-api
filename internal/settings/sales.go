package settings

import (
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
	"unicode"
)

type Sales struct {
	ValidityDays int    `json:"validityDays"`
	Terms        string `json:"terms"`
	Version      int    `json:"version"`
}
type QuoteDefaults struct {
	ExpiryDate string `json:"expiryDate"`
	Terms      string `json:"terms"`
}

func ValidateSales(in *Sales) error {
	in.Terms = strings.TrimSpace(in.Terms)
	if in.Version < 1 || in.ValidityDays < 1 || in.ValidityDays > 365 || !product.Text(in.Terms, 10000) || strings.IndexFunc(in.Terms, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return user.ErrInvalid
	}
	return nil
}
func (s *Service) GetSales(ctx context.Context, a user.Context) (Sales, error) {
	var v Sales
	if !a.Allows("settings.manage") {
		return v, user.ErrForbidden
	}
	e := s.DB.QueryRow(ctx, `SELECT quote_validity_days,quote_default_terms,sales_settings_version FROM organizations WHERE id=$1`, a.Organization.ID).Scan(&v.ValidityDays, &v.Terms, &v.Version)
	return v, e
}
func (s *Service) SaveSales(ctx context.Context, a user.Context, in Sales, rid string) (Sales, error) {
	if !a.Allows("settings.manage") {
		return Sales{}, user.ErrForbidden
	}
	if e := ValidateSales(&in); e != nil {
		return Sales{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Sales{}, e
	}
	defer tx.Rollback(ctx)
	var v Sales
	e = tx.QueryRow(ctx, `UPDATE organizations SET quote_validity_days=$2,quote_default_terms=$3,sales_settings_version=sales_settings_version+1 WHERE id=$1 AND sales_settings_version=$4 RETURNING quote_validity_days,quote_default_terms,sales_settings_version`, a.Organization.ID, in.ValidityDays, in.Terms, in.Version).Scan(&v.ValidityDays, &v.Terms, &v.Version)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return Sales{}, ErrConflict
		}
		return Sales{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,resource_id,action,request_id,after_value)VALUES($1,$2,$1,'organization.sales_defaults_updated',$3,jsonb_build_object('version',$4::integer,'validityDays',$5::integer))`, a.Organization.ID, a.User.ID, rid, v.Version, v.ValidityDays)
	if e != nil {
		return Sales{}, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) GetQuoteDefaults(ctx context.Context, a user.Context) (QuoteDefaults, error) {
	var v QuoteDefaults
	if !a.Allows("quote.create") {
		return v, user.ErrForbidden
	}
	var days int
	e := s.DB.QueryRow(ctx, `SELECT quote_validity_days,quote_default_terms FROM organizations WHERE id=$1`, a.Organization.ID).Scan(&days, &v.Terms)
	if e != nil {
		return v, e
	}
	v.ExpiryDate = time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02")
	return v, nil
}
