package product

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ErrNotFound = errors.New("product not found")
var ErrConflict = errors.New("product changed")
var Money = regexp.MustCompile(`^(0|[1-9][0-9]{0,11})(\.[0-9]{1,2})?$`)
var Currency = regexp.MustCompile(`^[A-Z]{3}$`)

func Text(s string, n int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= n && !strings.ContainsRune(s, 0)
}

type Price struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}
type Input struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
	Active      bool    `json:"active"`
	Prices      []Price `json:"prices"`
	Version     int     `json:"version"`
}
type Product struct {
	Input
	ID string `json:"id"`
}
type Service struct{ DB *pgxpool.Pool }

func Validate(in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Unit = strings.TrimSpace(in.Unit)
	if in.Name == "" || in.Unit == "" || !Text(in.Name, 200) || !Text(in.Unit, 40) || !Text(in.Description, 2000) || len(in.Prices) < 1 || len(in.Prices) > 10 {
		return user.ErrInvalid
	}
	seen := map[string]bool{}
	for i := range in.Prices {
		p := &in.Prices[i]
		p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
		if !Currency.MatchString(p.Currency) || !Money.MatchString(p.Amount) || seen[p.Currency] {
			return user.ErrInvalid
		}
		seen[p.Currency] = true
	}
	return nil
}
func (s *Service) List(ctx context.Context, a user.Context, q string, active bool, page, size int) ([]Product, int, error) {
	if !a.Allows("product.read") {
		return nil, 0, user.ErrForbidden
	}
	if !Text(q, 200) {
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := ` WHERE organization_id=$1 AND ($2='' OR strpos(lower(name),lower($2))>0) AND (NOT $3 OR active)`
	total := 0
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM products`+where, a.Organization.ID, q, active).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT id,name,description,unit,active,version,(SELECT COALESCE(jsonb_agg(jsonb_build_object('currency',currency,'amount',amount::text) ORDER BY currency),'[]') FROM product_prices pp WHERE pp.organization_id=products.organization_id AND pp.product_id=products.id) FROM products`+where+` ORDER BY name,id LIMIT $4 OFFSET $5`, a.Organization.ID, q, active, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []Product{}
	for rows.Next() {
		var p Product
		var raw []byte
		if e = rows.Scan(&p.ID, &p.Name, &p.Description, &p.Unit, &p.Active, &p.Version, &raw); e != nil {
			rows.Close()
			return nil, 0, e
		}
		if e = json.Unmarshal(raw, &p.Prices); e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, p)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func (s *Service) Save(ctx context.Context, a user.Context, id string, in Input, rid string) (Product, error) {
	if !a.Allows("product.manage") {
		return Product{}, user.ErrForbidden
	}
	if id != "" && (!customer.UUID(id) || in.Version < 1) {
		return Product{}, user.ErrInvalid
	}
	if e := Validate(&in); e != nil {
		return Product{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Product{}, e
	}
	defer tx.Rollback(ctx)
	var before any
	v := Product{Input: in, ID: id}
	action := "product.created"
	if id == "" {
		e = tx.QueryRow(ctx, `INSERT INTO products(organization_id,name,description,unit,active) VALUES($1,$2,$3,$4,$5) RETURNING id,version`, a.Organization.ID, in.Name, in.Description, in.Unit, in.Active).Scan(&v.ID, &v.Version)
	} else {
		var old Product
		old.ID = id
		var raw []byte
		e = tx.QueryRow(ctx, `SELECT name,description,unit,active,version,(SELECT COALESCE(jsonb_agg(jsonb_build_object('currency',currency,'amount',amount::text)),'[]') FROM product_prices WHERE organization_id=$1 AND product_id=$2) FROM products WHERE organization_id=$1 AND id=$2 FOR UPDATE`, a.Organization.ID, id).Scan(&old.Name, &old.Description, &old.Unit, &old.Active, &old.Version, &raw)
		if errors.Is(e, pgx.ErrNoRows) {
			return v, ErrNotFound
		}
		if e != nil {
			return v, e
		}
		if old.Version != in.Version {
			return v, ErrConflict
		}
		if e = json.Unmarshal(raw, &old.Prices); e != nil {
			return v, e
		}
		before = old
		action = "product.updated"
		e = tx.QueryRow(ctx, `UPDATE products SET name=$3,description=$4,unit=$5,active=$6,version=version+1 WHERE organization_id=$1 AND id=$2 RETURNING version`, a.Organization.ID, id, in.Name, in.Description, in.Unit, in.Active).Scan(&v.Version)
	}
	if e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM product_prices WHERE organization_id=$1 AND product_id=$2`, a.Organization.ID, v.ID); e != nil {
		return v, e
	}
	for _, p := range in.Prices {
		if _, e = tx.Exec(ctx, `INSERT INTO product_prices(organization_id,product_id,currency,amount) VALUES($1,$2,$3,$4)`, a.Organization.ID, v.ID, p.Currency, p.Amount); e != nil {
			return v, e
		}
	}
	var old any
	if before != nil {
		b, _ := json.Marshal(before)
		old = b
	}
	next, _ := json.Marshal(v)
	if _, e = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7)`, a.Organization.ID, a.User.ID, action, v.ID, rid, old, next); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
