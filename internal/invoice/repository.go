package invoice

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/quote"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Service struct{ DB *pgxpool.Pool }

const columns = `id,quote_id,COALESCE(project_id::text,''),customer_id,number,quote_number,company_name,customer_name,customer_email,currency,subtotal::text,tax::text,total::text,paid::text,(total-paid)::text,issue_date::text,due_date::text,notes,state,version,created_at`
const statusSQL = `CASE WHEN state='draft' THEN 'draft' WHEN paid>=total THEN 'paid' WHEN due_date<(now() AT TIME ZONE 'UTC')::date THEN 'overdue' WHEN paid>0 THEN 'partially_paid' ELSE state END`

func scan(row pgx.Row) (Invoice, error) {
	v := Invoice{Items: []quote.Item{}, Payments: []Payment{}}
	e := row.Scan(&v.ID, &v.QuoteID, &v.ProjectID, &v.CustomerID, &v.Number, &v.QuoteNumber, &v.CompanyName, &v.CustomerName, &v.CustomerEmail, &v.Currency, &v.Subtotal, &v.Tax, &v.Total, &v.Paid, &v.Outstanding, &v.IssueDate, &v.DueDate, &v.Notes, &v.State, &v.Version, &v.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	t, _ := cents(v.Total)
	p, _ := cents(v.Paid)
	v.Status = Status(v.State, v.DueDate, t, p, time.Now().UTC().Format("2006-01-02"))
	return v, e
}
func load(ctx context.Context, tx pgx.Tx, org, id string, lock bool) (Invoice, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	v, e := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM invoices WHERE organization_id=$1 AND id=$2`+suffix, org, id))
	return v, e
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, action, id, rid string, before, after any) error {
	var b any
	if before != nil {
		x, _ := json.Marshal(before)
		b = x
	}
	n, _ := json.Marshal(after)
	_, e := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7)`, a.Organization.ID, a.User.ID, action, id, rid, b, n)
	return e
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Invoice, error) {
	if !a.Allows("invoice.read") {
		return Invoice{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Invoice{}, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Invoice{}, e
	}
	defer tx.Rollback(ctx)
	v, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return v, e
	}
	rows, e := tx.Query(ctx, `SELECT name,description,unit,quantity::text,unit_price::text,discount_percent::text,subtotal::text,discount_amount::text,total::text FROM invoice_items WHERE organization_id=$1 AND invoice_id=$2 ORDER BY position`, a.Organization.ID, id)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var i quote.Item
		if e = rows.Scan(&i.Name, &i.Description, &i.Unit, &i.Quantity, &i.UnitPrice, &i.DiscountPercent, &i.Subtotal, &i.DiscountAmount, &i.Total); e != nil {
			rows.Close()
			return v, e
		}
		v.Items = append(v.Items, i)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return v, e
	}
	if a.Allows("payment.read") {
		rows, e = tx.Query(ctx, `SELECT id,amount::text,payment_date::text,reference,method,notes,idempotency_key,created_at FROM payments WHERE organization_id=$1 AND invoice_id=$2 ORDER BY created_at DESC,id`, a.Organization.ID, id)
		if e != nil {
			return v, e
		}
		for rows.Next() {
			var p Payment
			if e = rows.Scan(&p.ID, &p.Amount, &p.PaymentDate, &p.Reference, &p.Method, &p.Notes, &p.IdempotencyKey, &p.CreatedAt); e != nil {
				rows.Close()
				return v, e
			}
			v.Payments = append(v.Payments, p)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return v, e
		}
	}
	return v, tx.Commit(ctx)
}
func (s *Service) List(ctx context.Context, a user.Context, q, status, cust, project, quoteID string, page, size int) ([]Invoice, int, error) {
	if !a.Allows("invoice.read") {
		return nil, 0, user.ErrForbidden
	}
	if !product.Text(q, 200) || page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, 0, user.ErrInvalid
	}
	for _, id := range []string{cust, project, quoteID} {
		if id != "" && !customer.UUID(id) {
			return nil, 0, user.ErrInvalid
		}
	}
	switch status {
	case "", "draft", "issued", "sent", "partially_paid", "paid", "overdue":
	default:
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := ` FROM invoices WHERE organization_id=$1 AND ($2='' OR strpos(lower(number||' '||customer_name||' '||quote_number),lower($2))>0) AND ($3='' OR (` + statusSQL + `)=$3) AND ($4='' OR customer_id=NULLIF($4,'')::uuid) AND ($5='' OR project_id=NULLIF($5,'')::uuid) AND ($6='' OR quote_id=NULLIF($6,'')::uuid)`
	n := 0
	if e = tx.QueryRow(ctx, `SELECT count(*)`+where, a.Organization.ID, q, status, cust, project, quoteID).Scan(&n); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+columns+where+` ORDER BY created_at DESC,id LIMIT $7 OFFSET $8`, a.Organization.ID, q, status, cust, project, quoteID, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []Invoice{}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	return out, n, tx.Commit(ctx)
}
func (s *Service) Create(ctx context.Context, a user.Context, in Input, rid string) (Invoice, error) {
	if !a.Allows("invoice.create") {
		return Invoice{}, user.ErrForbidden
	}
	if e := Validate(&in); e != nil {
		return Invoice{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Invoice{}, e
	}
	defer tx.Rollback(ctx)
	var cust, company, name, email, currency, subtotal, number, opp string
	e = tx.QueryRow(ctx, `SELECT customer_id,company_name,customer_name,customer_email,currency,total::text,number,opportunity_id FROM quotes WHERE organization_id=$1 AND id=$2 AND status='accepted' FOR UPDATE`, a.Organization.ID, in.QuoteID).Scan(&cust, &company, &name, &email, &currency, &subtotal, &number, &opp)
	if errors.Is(e, pgx.ErrNoRows) {
		return Invoice{}, user.ErrInvalid
	}
	if e != nil {
		return Invoice{}, e
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM invoices WHERE organization_id=$1 AND quote_id=$2)`, a.Organization.ID, in.QuoteID).Scan(&exists); e != nil {
		return Invoice{}, e
	}
	if exists {
		return Invoice{}, ErrExists
	}
	if in.ProjectID != "" {
		var p string
		e = tx.QueryRow(ctx, `SELECT id FROM projects WHERE organization_id=$1 AND id=$2 AND customer_id=$3 AND opportunity_id=$4 AND (quote_id IS NULL OR quote_id=$5) FOR SHARE`, a.Organization.ID, in.ProjectID, cust, opp, in.QuoteID).Scan(&p)
		if errors.Is(e, pgx.ErrNoRows) {
			return Invoice{}, user.ErrInvalid
		}
		if e != nil {
			return Invoice{}, e
		}
	}
	total, e := Total(subtotal, in.Tax)
	if e != nil {
		return Invoice{}, e
	}
	var seq int64
	e = tx.QueryRow(ctx, `INSERT INTO invoice_counters(organization_id,next_number) VALUES($1,1) ON CONFLICT(organization_id) DO UPDATE SET next_number=invoice_counters.next_number+1 RETURNING next_number`, a.Organization.ID).Scan(&seq)
	if e != nil {
		return Invoice{}, e
	}
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO invoices(organization_id,quote_id,customer_id,project_id,creator_id,number,quote_number,company_name,customer_name,customer_email,currency,subtotal,tax,total,issue_date,due_date,notes) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::date,$16::date,$17) RETURNING id`, a.Organization.ID, in.QuoteID, cust, in.ProjectID, a.User.ID, fmt.Sprintf("INV-%s-%06d", time.Now().UTC().Format("2006"), seq), number, company, name, email, currency, subtotal, in.Tax, total, in.IssueDate, in.DueDate, in.Notes).Scan(&id)
	if e != nil {
		return Invoice{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO invoice_items SELECT organization_id,$3::uuid,position,name,description,unit,quantity,unit_price,discount_percent,subtotal,discount_amount,total FROM quote_items WHERE organization_id=$1 AND quote_id=$2`, a.Organization.ID, in.QuoteID, id)
	if e != nil {
		return Invoice{}, e
	}
	v, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, "invoice.created", id, rid, nil, v); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Update(ctx context.Context, a user.Context, id string, in Patch, rid string) (Invoice, error) {
	if !a.Allows("invoice.update") {
		return Invoice{}, user.ErrForbidden
	}
	if !customer.UUID(id) || in.Version < 1 {
		return Invoice{}, user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Invoice{}, e
	}
	defer tx.Rollback(ctx)
	old, e := load(ctx, tx, a.Organization.ID, id, true)
	if e != nil {
		return old, e
	}
	if old.State != "draft" || old.Version != in.Version {
		return old, ErrConflict
	}
	v := Input{QuoteID: old.QuoteID, ProjectID: old.ProjectID, IssueDate: in.IssueDate, DueDate: in.DueDate, Tax: in.Tax, Notes: in.Notes}
	if e = Validate(&v); e != nil {
		return old, e
	}
	total, e := Total(old.Subtotal, v.Tax)
	if e != nil {
		return old, e
	}
	_, e = tx.Exec(ctx, `UPDATE invoices SET issue_date=$3::date,due_date=$4::date,tax=$5,total=$6,notes=$7,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id, v.IssueDate, v.DueDate, v.Tax, total, v.Notes)
	if e != nil {
		return old, e
	}
	next, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return next, e
	}
	if e = audit(ctx, tx, a, "invoice.updated", id, rid, old, next); e != nil {
		return next, e
	}
	return next, tx.Commit(ctx)
}
func (s *Service) Act(ctx context.Context, a user.Context, id, action string, version int, rid string) (Invoice, error) {
	if !a.Allows("invoice.update") {
		return Invoice{}, user.ErrForbidden
	}
	if !customer.UUID(id) || version < 1 {
		return Invoice{}, user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Invoice{}, e
	}
	defer tx.Rollback(ctx)
	old, e := load(ctx, tx, a.Organization.ID, id, true)
	if e != nil {
		return old, e
	}
	nextState := ""
	if action == "issue" && old.State == "draft" && old.IssueDate <= time.Now().UTC().Format("2006-01-02") {
		nextState = "issued"
	}
	if action == "mark-sent" && old.State == "issued" {
		nextState = "sent"
	}
	if old.Version != version || nextState == "" {
		return old, ErrConflict
	}
	_, e = tx.Exec(ctx, `UPDATE invoices SET state=$3,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id, nextState)
	if e != nil {
		return old, e
	}
	next, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return next, e
	}
	if e = audit(ctx, tx, a, "invoice."+action, id, rid, old, next); e != nil {
		return next, e
	}
	return next, tx.Commit(ctx)
}
func (s *Service) Pay(ctx context.Context, a user.Context, id string, in PaymentInput, rid string) (Payment, error) {
	if !a.Allows("payment.create") {
		return Payment{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Payment{}, user.ErrInvalid
	}
	if e := ValidatePayment(&in); e != nil {
		return Payment{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Payment{}, e
	}
	defer tx.Rollback(ctx)
	v, e := load(ctx, tx, a.Organization.ID, id, true)
	if e != nil {
		return Payment{}, e
	}
	var old Payment
	e = tx.QueryRow(ctx, `SELECT id,amount::text,payment_date::text,reference,method,notes,idempotency_key,created_at FROM payments WHERE organization_id=$1 AND invoice_id=$2 AND idempotency_key=$3`, a.Organization.ID, id, in.IdempotencyKey).Scan(&old.ID, &old.Amount, &old.PaymentDate, &old.Reference, &old.Method, &old.Notes, &old.IdempotencyKey, &old.CreatedAt)
	if e == nil {
		if old.PaymentInput != in {
			return Payment{}, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return Payment{}, e
	}
	if v.State == "draft" {
		return Payment{}, ErrConflict
	}
	amount, _ := cents(in.Amount)
	balance, _ := cents(v.Outstanding)
	if amount > balance || in.PaymentDate < v.IssueDate {
		return Payment{}, ErrPayment
	}
	p := Payment{PaymentInput: in}
	e = tx.QueryRow(ctx, `INSERT INTO payments(organization_id,invoice_id,creator_id,amount,payment_date,reference,method,notes,idempotency_key) VALUES($1,$2,$3,$4,$5::date,$6,$7,$8,$9) RETURNING id,created_at`, a.Organization.ID, id, a.User.ID, in.Amount, in.PaymentDate, in.Reference, in.Method, in.Notes, in.IdempotencyKey).Scan(&p.ID, &p.CreatedAt)
	if e != nil {
		return p, e
	}
	_, e = tx.Exec(ctx, `UPDATE invoices SET paid=paid+$3::numeric,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id, in.Amount)
	if e != nil {
		return p, e
	}
	if e = audit(ctx, tx, a, "invoice.payment_recorded", id, rid, nil, p); e != nil {
		return p, e
	}
	return p, tx.Commit(ctx)
}
func (s *Service) Dashboard(ctx context.Context, a user.Context) ([]Balance, error) {
	if !a.Allows("invoice.read") || !a.Allows("payment.read") {
		return nil, user.ErrForbidden
	}
	rows, e := s.DB.Query(ctx, `SELECT currency,COALESCE(sum(total) FILTER(WHERE state<>'draft'),0)::text,COALESCE(sum(paid) FILTER(WHERE state<>'draft'),0)::text,COALESCE(sum(total-paid) FILTER(WHERE state<>'draft'),0)::text,COALESCE(sum(total-paid) FILTER(WHERE state<>'draft' AND due_date<(now() AT TIME ZONE 'UTC')::date),0)::text,count(*) FILTER(WHERE state<>'draft' AND paid<total),count(*) FILTER(WHERE state<>'draft' AND paid<total AND due_date<(now() AT TIME ZONE 'UTC')::date),count(*) FILTER(WHERE state='draft') FROM invoices WHERE organization_id=$1 GROUP BY currency ORDER BY currency`, a.Organization.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Balance{}
	for rows.Next() {
		var b Balance
		if e = rows.Scan(&b.Currency, &b.Invoiced, &b.Collected, &b.Outstanding, &b.Overdue, &b.OpenCount, &b.OverdueCount, &b.DraftCount); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
