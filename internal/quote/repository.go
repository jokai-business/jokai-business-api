package quote

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Service struct{ DB *pgxpool.Pool }

const columns = `id,customer_id,opportunity_id,creator_id,number,company_name,customer_name,customer_email,opportunity_title,currency,status,expiry_date::text,terms,subtotal::text,discount_total::text,total::text,version,created_at,updated_at`

func scan(row pgx.Row) (Quote, error) {
	q := Quote{Items: []Item{}, Approvals: []Approval{}}
	e := row.Scan(&q.ID, &q.CustomerID, &q.OpportunityID, &q.CreatorID, &q.Number, &q.CompanyName, &q.CustomerName, &q.CustomerEmail, &q.OpportunityTitle, &q.Currency, &q.Status, &q.ExpiryDate, &q.Terms, &q.Subtotal, &q.DiscountTotal, &q.Total, &q.Version, &q.CreatedAt, &q.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return q, e
}
func load(ctx context.Context, tx pgx.Tx, org, id string, lock bool) (Quote, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	q, e := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM quotes WHERE organization_id=$1 AND id=$2`+suffix, org, id))
	if e != nil {
		return q, e
	}
	rows, e := tx.Query(ctx, `SELECT product_id,name,description,unit,quantity::text,unit_price::text,discount_percent::text,subtotal::text,discount_amount::text,total::text FROM quote_items WHERE organization_id=$1 AND quote_id=$2 ORDER BY position`, org, id)
	if e != nil {
		return q, e
	}
	for rows.Next() {
		var item Item
		if e = rows.Scan(&item.ProductID, &item.Name, &item.Description, &item.Unit, &item.Quantity, &item.UnitPrice, &item.DiscountPercent, &item.Subtotal, &item.DiscountAmount, &item.Total); e != nil {
			rows.Close()
			return q, e
		}
		q.Items = append(q.Items, item)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return q, e
	}
	rows, e = tx.Query(ctx, `SELECT ar.id,ar.status,u.name,COALESCE(d.name,''),ar.note,ar.requested_at,ar.decided_at FROM approval_requests ar JOIN users u ON u.organization_id=ar.organization_id AND u.id=ar.requested_by LEFT JOIN users d ON d.organization_id=ar.organization_id AND d.id=ar.decided_by WHERE ar.organization_id=$1 AND ar.quote_id=$2 ORDER BY ar.requested_at DESC,ar.id`, org, id)
	if e != nil {
		return q, e
	}
	for rows.Next() {
		var a Approval
		if e = rows.Scan(&a.ID, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.Note, &a.RequestedAt, &a.DecidedAt); e != nil {
			rows.Close()
			return q, e
		}
		q.Approvals = append(q.Approvals, a)
	}
	rows.Close()
	return q, rows.Err()
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Quote, error) {
	if !a.Allows("quote.read") {
		return Quote{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Quote{}, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Quote{}, e
	}
	defer tx.Rollback(ctx)
	q, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
func (s *Service) List(ctx context.Context, a user.Context, query, status, opportunityID, customerID string, page, size int) ([]Quote, int, error) {
	if !a.Allows("quote.read") {
		return nil, 0, user.ErrForbidden
	}
	if !product.Text(query, 200) || (opportunityID != "" && !customer.UUID(opportunityID)) || (customerID != "" && !customer.UUID(customerID)) {
		return nil, 0, user.ErrInvalid
	}
	if status != "" && status != "draft" && status != "submitted" && status != "approved" && status != "rejected" && status != "sent" && status != "accepted" {
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := ` WHERE organization_id=$1 AND ($2='' OR strpos(lower(number||' '||customer_name||' '||opportunity_title),lower($2))>0) AND ($3='' OR status=$3) AND ($4='' OR opportunity_id=NULLIF($4,'')::uuid) AND ($5='' OR customer_id=NULLIF($5,'')::uuid)`
	total := 0
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM quotes`+where, a.Organization.ID, query, status, opportunityID, customerID).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+columns+` FROM quotes`+where+` ORDER BY created_at DESC,id LIMIT $6 OFFSET $7`, a.Organization.ID, query, status, opportunityID, customerID, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []Quote{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, action, id, rid string, before, after any) error {
	var old any
	if before != nil {
		b, _ := json.Marshal(before)
		old = b
	}
	next, _ := json.Marshal(after)
	_, e := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7)`, a.Organization.ID, a.User.ID, action, id, rid, old, next)
	return e
}
func (s *Service) Save(ctx context.Context, a user.Context, id string, in Input, rid string) (Quote, error) {
	if !a.Allows("quote.create") {
		return Quote{}, user.ErrForbidden
	}
	if id != "" && (!customer.UUID(id) || in.Version < 1) {
		return Quote{}, user.ErrInvalid
	}
	items, subtotal, discount, total, e := Calculate(&in)
	if e != nil {
		return Quote{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Quote{}, e
	}
	defer tx.Rollback(ctx)
	q := Quote{ID: id, OpportunityID: in.OpportunityID, Currency: in.Currency, ExpiryDate: in.ExpiryDate, Terms: in.Terms, Items: items, Subtotal: subtotal, DiscountTotal: discount, Total: total, Status: "draft", CreatorID: a.User.ID, Approvals: []Approval{}}
	var before any
	action := "quote.created"
	if id != "" {
		old, err := load(ctx, tx, a.Organization.ID, id, true)
		if err != nil {
			return q, err
		}
		if old.Version != in.Version || (old.Status != "draft" && old.Status != "rejected") {
			return q, ErrConflict
		}
		if old.CreatorID != a.User.ID && !a.Allows("quote.approve") {
			return q, user.ErrForbidden
		}
		before = old
		q.CreatorID = old.CreatorID
		q.Number = old.Number
		q.CreatedAt = old.CreatedAt
		action = "quote.updated"
	}
	e = tx.QueryRow(ctx, `SELECT o.customer_id,c.name,c.email,o.title,g.name FROM opportunities o JOIN customers c ON c.organization_id=o.organization_id AND c.id=o.customer_id JOIN organizations g ON g.id=o.organization_id WHERE o.organization_id=$1 AND o.id=$2 FOR SHARE OF o,c`, a.Organization.ID, in.OpportunityID).Scan(&q.CustomerID, &q.CustomerName, &q.CustomerEmail, &q.OpportunityTitle, &q.CompanyName)
	if errors.Is(e, pgx.ErrNoRows) {
		return q, user.ErrInvalid
	}
	if e != nil {
		return q, e
	}
	for i := range q.Items {
		item := &q.Items[i]
		e = tx.QueryRow(ctx, `SELECT p.name,p.description,p.unit FROM products p JOIN product_prices pp ON pp.organization_id=p.organization_id AND pp.product_id=p.id AND pp.currency=$3 WHERE p.organization_id=$1 AND p.id=$2 AND p.active FOR SHARE OF p,pp`, a.Organization.ID, item.ProductID, in.Currency).Scan(&item.Name, &item.Description, &item.Unit)
		if errors.Is(e, pgx.ErrNoRows) {
			return q, user.ErrInvalid
		}
		if e != nil {
			return q, e
		}
	}
	if id == "" {
		var number int64
		e = tx.QueryRow(ctx, `INSERT INTO quote_counters(organization_id,next_number) VALUES($1,1) ON CONFLICT(organization_id) DO UPDATE SET next_number=quote_counters.next_number+1 RETURNING next_number`, a.Organization.ID).Scan(&number)
		if e != nil {
			return q, e
		}
		q.Number = fmt.Sprintf("Q-%06d", number)
		e = tx.QueryRow(ctx, `INSERT INTO quotes(organization_id,customer_id,opportunity_id,creator_id,number,company_name,customer_name,customer_email,opportunity_title,currency,status,expiry_date,terms,subtotal,discount_total,total) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'draft',$11::date,$12,$13,$14,$15) RETURNING id,version,created_at,updated_at`, a.Organization.ID, q.CustomerID, q.OpportunityID, q.CreatorID, q.Number, q.CompanyName, q.CustomerName, q.CustomerEmail, q.OpportunityTitle, q.Currency, q.ExpiryDate, q.Terms, q.Subtotal, q.DiscountTotal, q.Total).Scan(&q.ID, &q.Version, &q.CreatedAt, &q.UpdatedAt)
	} else {
		e = tx.QueryRow(ctx, `UPDATE quotes SET customer_id=$3,opportunity_id=$4,company_name=$5,customer_name=$6,customer_email=$7,opportunity_title=$8,currency=$9,expiry_date=$10::date,terms=$11,subtotal=$12,discount_total=$13,total=$14,status='draft',version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2 RETURNING version,updated_at`, a.Organization.ID, id, q.CustomerID, q.OpportunityID, q.CompanyName, q.CustomerName, q.CustomerEmail, q.OpportunityTitle, q.Currency, q.ExpiryDate, q.Terms, q.Subtotal, q.DiscountTotal, q.Total).Scan(&q.Version, &q.UpdatedAt)
	}
	if e != nil {
		return q, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM quote_items WHERE organization_id=$1 AND quote_id=$2`, a.Organization.ID, q.ID); e != nil {
		return q, e
	}
	for i, item := range q.Items {
		_, e = tx.Exec(ctx, `INSERT INTO quote_items(organization_id,quote_id,position,product_id,name,description,unit,quantity,unit_price,discount_percent,subtotal,discount_amount,total) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, a.Organization.ID, q.ID, i, item.ProductID, item.Name, item.Description, item.Unit, item.Quantity, item.UnitPrice, item.DiscountPercent, item.Subtotal, item.DiscountAmount, item.Total)
		if e != nil {
			return q, e
		}
	}
	if e = audit(ctx, tx, a, action, q.ID, rid, before, q); e != nil {
		return q, e
	}
	v, e := load(ctx, tx, a.Organization.ID, q.ID, false)
	if e != nil {
		return q, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Act(ctx context.Context, a user.Context, id, action string, in Action, rid string) (Quote, error) {
	permission := "quote.create"
	if action == "approve" || action == "reject" {
		permission = "quote.approve"
	}
	if !a.Allows(permission) {
		return Quote{}, user.ErrForbidden
	}
	in.Note = strings.TrimSpace(in.Note)
	if !customer.UUID(id) || in.Version < 1 || !product.Text(in.Note, 2000) || (action == "reject" && in.Note == "") {
		return Quote{}, user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Quote{}, e
	}
	defer tx.Rollback(ctx)
	q, e := load(ctx, tx, a.Organization.ID, id, true)
	if e != nil {
		return q, e
	}
	before := q
	next := Transition(q.Status, action)
	if q.Version != in.Version || next == "" {
		return q, ErrConflict
	}
	if action == "approve" || action == "reject" {
		if q.CreatorID == a.User.ID {
			return q, user.ErrForbidden
		}
	} else if q.CreatorID != a.User.ID && !a.Allows("quote.approve") {
		return q, user.ErrForbidden
	}
	if action != "reject" && q.ExpiryDate < time.Now().UTC().Format("2006-01-02") {
		return q, ErrExpired
	}
	if action == "submit" {
		_, e = tx.Exec(ctx, `INSERT INTO approval_requests(organization_id,quote_id,requested_by,status) VALUES($1,$2,$3,'pending')`, a.Organization.ID, id, a.User.ID)
	} else if action == "approve" || action == "reject" {
		result, err := tx.Exec(ctx, `UPDATE approval_requests SET status=$3,decided_by=$4,note=$5,decided_at=now() WHERE organization_id=$1 AND quote_id=$2 AND status='pending'`, a.Organization.ID, id, next, a.User.ID, in.Note)
		e = err
		if e == nil && result.RowsAffected() != 1 {
			return q, ErrConflict
		}
	}
	if e != nil {
		return q, e
	}
	q.Status = next
	e = tx.QueryRow(ctx, `UPDATE quotes SET status=$3,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2 RETURNING version,updated_at`, a.Organization.ID, id, next).Scan(&q.Version, &q.UpdatedAt)
	if e != nil {
		return q, e
	}
	if e = audit(ctx, tx, a, "quote."+next, id, rid, before, q); e != nil {
		return q, e
	}
	q, e = load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
