package imports

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/opportunity"
	"ai-business-api/internal/user"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

var ErrChanged = errors.New("import changed, expired, or already imported")
var ErrNotFound = errors.New("import not found")
var ErrLimit = errors.New("too many pending previews")

type Service struct{ DB *pgxpool.Pool }
type Row struct {
	Number      int                `json:"number"`
	Values      []string           `json:"values"`
	Errors      []string           `json:"errors"`
	Customer    *customer.Customer `json:"customer"`
	Opportunity *opportunity.Input `json:"opportunity"`
	RecordID    string             `json:"recordId"`
}
type Report struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Rows        []Row      `json:"rows"`
	CreatedAt   time.Time  `json:"createdAt"`
	ConfirmedAt *time.Time `json:"confirmedAt"`
}

func authorize(ctx context.Context, tx pgx.Tx, a user.Context, kind string) error {
	if _, ok := headers[kind]; !ok {
		return user.ErrInvalid
	}
	var perms []string
	e := tx.QueryRow(ctx, `SELECT ARRAY(SELECT DISTINCT rp.permission_key FROM user_roles ur JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE ur.organization_id=u.organization_id AND ur.user_id=u.id) FROM users u WHERE u.organization_id=$1 AND u.id=$2 AND u.active`, a.Organization.ID, a.User.ID).Scan(&perms)
	if errors.Is(e, pgx.ErrNoRows) {
		return user.ErrForbidden
	}
	if e != nil {
		return e
	}
	a.Permissions = perms
	if !a.Allows(kind+".read") || !a.Allows(kind+".create") || kind == "opportunity" && !a.Allows("customer.read") {
		return user.ErrForbidden
	}
	return nil
}
func resolve(ctx context.Context, tx pgx.Tx, q, org, value string) (string, error) {
	var ids []string
	e := tx.QueryRow(ctx, q, org, value).Scan(&ids)
	if e != nil {
		return "", e
	}
	if len(ids) != 1 {
		return "", nil
	}
	return ids[0], nil
}
func validate(ctx context.Context, tx pgx.Tx, a user.Context, kind string, values [][]string) ([]Row, error) {
	out := []Row{}
	seen := map[string]bool{}
	for i, v := range values {
		row := Row{Number: i + 2, Values: v, Errors: []string{}}
		duplicate := func(key string) {
			if seen[key] {
				row.Errors = append(row.Errors, "Duplicate record within this workbook.")
			}
			seen[key] = true
		}
		var exists bool
		if kind == "customer" {
			c := customer.Customer{Name: v[0], Email: v[1], Phone: v[2], Website: v[3], Industry: v[4], OwnerID: a.User.ID, Status: "prospect"}
			if customer.NormalizeImport(&c) != nil {
				row.Errors = append(row.Errors, "Invalid customer fields: name is required; check email, website URL and field lengths.")
			}
			duplicate("name:" + strings.ToLower(c.Name))
			if c.Email != "" {
				duplicate("email:" + strings.ToLower(c.Email))
			}
			e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE organization_id=$1 AND (lower(btrim(name))=lower($2) OR ($3<>'' AND lower(btrim(email))=lower($3))))`, a.Organization.ID, c.Name, c.Email).Scan(&exists)
			if e != nil {
				return nil, e
			}
			row.Customer = &c
		} else {
			cid, e := resolve(ctx, tx, `SELECT ARRAY(SELECT id::text FROM customers WHERE organization_id=$1 AND (id::text=$2 OR lower(btrim(name))=lower($2)) ORDER BY id)`, a.Organization.ID, v[0])
			if e != nil {
				return nil, e
			}
			sid, e := resolve(ctx, tx, `SELECT ARRAY(SELECT id::text FROM pipeline_stages WHERE organization_id=$1 AND (id::text=$2 OR lower(btrim(name))=lower($2)) ORDER BY id)`, a.Organization.ID, v[4])
			if e != nil {
				return nil, e
			}
			owner := v[5]
			if owner == "" {
				owner = a.User.ID
			}
			uid, e := resolve(ctx, tx, `SELECT ARRAY(SELECT id::text FROM users WHERE organization_id=$1 AND active AND (id::text=$2 OR lower(email)=lower($2)) ORDER BY id)`, a.Organization.ID, owner)
			if e != nil {
				return nil, e
			}
			if cid == "" {
				row.Errors = append(row.Errors, "Customer must match one existing name or ID.")
			}
			if sid == "" {
				row.Errors = append(row.Errors, "Stage must match one existing name or ID.")
			}
			if uid == "" {
				row.Errors = append(row.Errors, "Owner must match one active member email or ID (or leave blank for yourself).")
			}
			o := opportunity.Input{CustomerID: cid, Title: v[1], ExpectedValue: v[2], Currency: v[3], StageID: sid, OwnerID: uid, Source: "Excel"}
			if opportunity.NormalizeImport(&o) != nil {
				row.Errors = append(row.Errors, "Check required fields, nonnegative value with at most two decimals, and three-letter currency.")
			}
			duplicate(cid + ":" + strings.ToLower(o.Title))
			if cid != "" {
				e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM opportunities WHERE organization_id=$1 AND customer_id=$2 AND lower(btrim(title))=lower($3))`, a.Organization.ID, cid, o.Title).Scan(&exists)
				if e != nil {
					return nil, e
				}
			}
			row.Opportunity = &o
		}
		if exists {
			row.Errors = append(row.Errors, "A matching record already exists; this import only creates new records.")
		}
		out = append(out, row)
	}
	return out, nil
}
func (s *Service) Preview(ctx context.Context, a user.Context, kind string, data []byte) (Report, error) {
	var out Report
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = authorize(ctx, tx, a, kind); e != nil {
		return out, e
	}
	values, e := Parse(data, kind)
	if e != nil {
		return out, e
	}
	// Serialize preview quota checks for this member, including first uploads.
	if _, e = tx.Exec(ctx, `SELECT id FROM users WHERE organization_id=$1 AND id=$2 FOR UPDATE`, a.Organization.ID, a.User.ID); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM excel_imports WHERE status='preview' AND created_at<now()-interval '24 hours'`); e != nil {
		return out, e
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM excel_imports WHERE organization_id=$1 AND user_id=$2 AND status='preview'`, a.Organization.ID, a.User.ID).Scan(&count); e != nil {
		return out, e
	}
	if count >= 20 {
		return out, ErrLimit
	}
	rows, e := validate(ctx, tx, a, kind, values)
	if e != nil {
		return out, e
	}
	b, e := json.Marshal(rows)
	if e != nil {
		return out, e
	}
	h := sha256.Sum256(data)
	var used bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM excel_imports WHERE organization_id=$1 AND kind=$2 AND file_hash=$3 AND status='imported')`, a.Organization.ID, kind, hex.EncodeToString(h[:])).Scan(&used); e != nil {
		return out, e
	}
	if used {
		return out, ErrChanged
	}
	out.Kind = kind
	out.Status = "preview"
	out.Rows = rows
	e = tx.QueryRow(ctx, `INSERT INTO excel_imports(organization_id,user_id,kind,file_hash,rows)VALUES($1,$2,$3,$4,$5) RETURNING id,created_at`, a.Organization.ID, a.User.ID, kind, hex.EncodeToString(h[:]), b).Scan(&out.ID, &out.CreatedAt)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func load(ctx context.Context, tx pgx.Tx, a user.Context, id string, lock bool) (Report, error) {
	var out Report
	if !customer.UUID(id) {
		return out, user.ErrInvalid
	}
	q := `SELECT id,kind,status,rows,created_at,confirmed_at FROM excel_imports WHERE organization_id=$1 AND user_id=$2 AND id=$3 AND (status='imported' OR created_at>now()-interval '24 hours')`
	if lock {
		q += " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, q, a.Organization.ID, a.User.ID, id).Scan(&out.ID, &out.Kind, &out.Status, &out.Rows, &out.CreatedAt, &out.ConfirmedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, e
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Report, error) {
	var out Report
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	out, e = load(ctx, tx, a, id, false)
	if e != nil {
		return out, e
	}
	if e = authorize(ctx, tx, a, out.Kind); e != nil {
		return Report{}, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) Confirm(ctx context.Context, a user.Context, id string, confirmed bool, rid string) (Report, error) {
	var out Report
	if !confirmed {
		return out, user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	// Serializes imports within the organization; concurrent confirmations replay
	// the saved report and cannot create the same batch twice.
	if _, e = tx.Exec(ctx, `SELECT id FROM organizations WHERE id=$1 FOR NO KEY UPDATE`, a.Organization.ID); e != nil {
		return out, e
	}
	out, e = load(ctx, tx, a, id, true)
	if e != nil {
		return out, e
	}
	if e = authorize(ctx, tx, a, out.Kind); e != nil {
		return Report{}, e
	}
	if out.Status == "imported" {
		return out, tx.Commit(ctx)
	}
	// Block ordinary writes during the bounded validation/create transaction so
	// duplicate checks and referenced customer/stage/owner matches cannot race.
	if _, e = tx.Exec(ctx, `LOCK TABLE customers,opportunities,pipeline_stages,users IN SHARE ROW EXCLUSIVE MODE`); e != nil {
		return out, e
	}
	values := [][]string{}
	for _, r := range out.Rows {
		values = append(values, r.Values)
	}
	current, e := validate(ctx, tx, a, out.Kind, values)
	if e != nil {
		return out, e
	}
	for i, row := range current {
		if len(row.Errors) > 0 {
			return out, ErrChanged
		}
		old, _ := json.Marshal(out.Rows[i])
		now, _ := json.Marshal(row)
		if string(old) != string(now) {
			return out, ErrChanged
		}
	}
	for i, row := range current {
		var record string
		if row.Customer != nil {
			c := row.Customer
			e = tx.QueryRow(ctx, `INSERT INTO customers(organization_id,name,email,phone,website,industry,owner_id,status)VALUES($1,$2,$3,$4,$5,$6,$7,'prospect') RETURNING id`, a.Organization.ID, c.Name, c.Email, c.Phone, c.Website, c.Industry, c.OwnerID).Scan(&record)
		} else {
			o := row.Opportunity
			e = tx.QueryRow(ctx, `INSERT INTO opportunities(organization_id,customer_id,title,stage_id,expected_value,currency,probability,owner_id,source)VALUES($1,$2,$3,$4,$5,$6,0,$7,'Excel') RETURNING id`, a.Organization.ID, o.CustomerID, o.Title, o.StageID, o.ExpectedValue, o.Currency, o.OwnerID).Scan(&record)
		}
		if e != nil {
			return out, e
		}
		current[i].RecordID = record
		if row.Opportunity != nil {
			if _, e = tx.Exec(ctx, `INSERT INTO opportunity_activities(organization_id,opportunity_id,actor_id,kind,body)VALUES($1,$2,$3,'created','Opportunity created from Excel import')`, a.Organization.ID, record, a.User.ID); e != nil {
				return out, e
			}
		}
		if _, e = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,resource_id,action,request_id,after_value)VALUES($1,$2,$3,$4,$5,jsonb_build_object('importId',$6::text,'row',$7::integer))`, a.Organization.ID, a.User.ID, record, out.Kind+".created", rid, id, current[i].Number); e != nil {
			return out, e
		}
	}
	b, _ := json.Marshal(current)
	e = tx.QueryRow(ctx, `UPDATE excel_imports SET status='imported',rows=$2,confirmed_at=now() WHERE id=$1 RETURNING confirmed_at`, id, b).Scan(&out.ConfirmedAt)
	if e != nil {
		return out, e
	}
	out.Status = "imported"
	out.Rows = current
	return out, tx.Commit(ctx)
}
