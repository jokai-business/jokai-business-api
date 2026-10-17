package customer

import (
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ DB *pgxpool.Pool }

const columns = `c.id,c.name,c.email,c.phone,c.website,c.industry,c.country,c.owner_id,u.name,c.status,c.notes,c.version,c.created_at,c.updated_at`

func scan(row pgx.Row) (Customer, error) {
	var c Customer
	err := row.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Website, &c.Industry, &c.Country, &c.OwnerID, &c.OwnerName, &c.Status, &c.Notes, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}
func (r *Repository) Get(ctx context.Context, org, id string) (Customer, error) {
	return scan(r.DB.QueryRow(ctx, `SELECT `+columns+` FROM customers c JOIN users u ON u.organization_id=c.organization_id AND u.id=c.owner_id WHERE c.organization_id=$1 AND c.id=$2`, org, id))
}
func (r *Repository) List(ctx context.Context, org, q, status string, page, size int) ([]Customer, int, error) {
	tx, err := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	where := ` WHERE c.organization_id=$1 AND ($2='' OR strpos(lower(c.name||' '||c.email||' '||c.phone),lower($2))>0) AND ($3='' OR c.status=$3)`
	total := 0
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM customers c`+where, org, q, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM customers c JOIN users u ON u.organization_id=c.organization_id AND u.id=c.owner_id`+where+` ORDER BY c.created_at DESC,c.id LIMIT $4 OFFSET $5`, org, q, status, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	result := []Customer{}
	for rows.Next() {
		c, e := scan(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		result = append(result, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, tx.Commit(ctx)
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, action, id, rid string, before, after any) error {
	var old any
	if before != nil {
		b, _ := json.Marshal(before)
		old = b
	}
	next, _ := json.Marshal(after)
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7)`, a.Organization.ID, a.User.ID, action, id, rid, old, next)
	return err
}
func (r *Repository) Save(ctx context.Context, a user.Context, c Customer, before *Customer, rid string) (Customer, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT name FROM users WHERE organization_id=$1 AND id=$2 AND active FOR SHARE`, a.Organization.ID, c.OwnerID).Scan(&c.OwnerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, user.ErrInvalid
	}
	if err != nil {
		return c, err
	}
	action := "customer.created"
	if before == nil {
		err = tx.QueryRow(ctx, `INSERT INTO customers(organization_id,name,email,phone,website,industry,country,owner_id,status,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,version,created_at,updated_at`, a.Organization.ID, c.Name, c.Email, c.Phone, c.Website, c.Industry, c.Country, c.OwnerID, c.Status, c.Notes).Scan(&c.ID, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	} else {
		action = "customer.updated"
		err = tx.QueryRow(ctx, `UPDATE customers SET name=$3,email=$4,phone=$5,website=$6,industry=$7,country=$8,owner_id=$9,status=$10,notes=$11,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2 AND version=$12 RETURNING version,updated_at`, a.Organization.ID, c.ID, c.Name, c.Email, c.Phone, c.Website, c.Industry, c.Country, c.OwnerID, c.Status, c.Notes, before.Version).Scan(&c.Version, &c.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return c, ErrConflict
		}
	}
	if err != nil {
		return c, err
	}
	var old any
	if before != nil {
		old = before
	}
	if err = audit(ctx, tx, a, action, c.ID, rid, old, c); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (r *Repository) Owners(ctx context.Context, org string, page, size int) ([]Owner, int, error) {
	rows, err := r.DB.Query(ctx, `SELECT id,name,count(*) OVER() FROM users WHERE organization_id=$1 AND active ORDER BY name,id LIMIT $2 OFFSET $3`, org, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Owner{}
	total := 0
	for rows.Next() {
		var o Owner
		if err = rows.Scan(&o.ID, &o.Name, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	if len(out) == 0 {
		err = r.DB.QueryRow(ctx, `SELECT count(*) FROM users WHERE organization_id=$1 AND active`, org).Scan(&total)
	}
	if err != nil {
		return nil, 0, err
	}
	return out, total, rows.Err()
}
func (r *Repository) Contacts(ctx context.Context, org, id string, page, size int) ([]Contact, int, error) {
	rows, err := r.DB.Query(ctx, `SELECT id,customer_id,name,email,phone,title,created_at,count(*) OVER() FROM contacts WHERE organization_id=$1 AND customer_id=$2 ORDER BY created_at,id LIMIT $3 OFFSET $4`, org, id, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Contact{}
	total := 0
	for rows.Next() {
		var c Contact
		if err = rows.Scan(&c.ID, &c.CustomerID, &c.Name, &c.Email, &c.Phone, &c.Title, &c.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		err = r.DB.QueryRow(ctx, `SELECT count(*) FROM contacts WHERE organization_id=$1 AND customer_id=$2`, org, id).Scan(&total)
	}
	if err != nil {
		return nil, 0, err
	}
	return out, total, rows.Err()
}
func (r *Repository) AddContact(ctx context.Context, a user.Context, id string, in ContactInput, rid string) (Contact, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return Contact{}, err
	}
	defer tx.Rollback(ctx)
	var c Contact
	err = tx.QueryRow(ctx, `INSERT INTO contacts(organization_id,customer_id,name,email,phone,title) SELECT organization_id,id,$3,$4,$5,$6 FROM customers WHERE organization_id=$1 AND id=$2 RETURNING id,customer_id,name,email,phone,title,created_at`, a.Organization.ID, id, in.Name, in.Email, in.Phone, in.Title).Scan(&c.ID, &c.CustomerID, &c.Name, &c.Email, &c.Phone, &c.Title, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if err = audit(ctx, tx, a, "customer.contact_created", id, rid, nil, c); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (r *Repository) Activity(ctx context.Context, org, id string, page, size int) ([]user.Audit, int, error) {
	rows, err := r.DB.Query(ctx, `SELECT id,action,actor_id,resource_id,created_at::text,request_id,count(*) OVER() FROM audit_logs WHERE organization_id=$1 AND resource_id=$2 ORDER BY created_at DESC,id LIMIT $3 OFFSET $4`, org, id, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []user.Audit{}
	total := 0
	for rows.Next() {
		var a user.Audit
		if err = rows.Scan(&a.ID, &a.Action, &a.ActorID, &a.ResourceID, &a.CreatedAt, &a.RequestID, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		err = r.DB.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id=$1 AND resource_id=$2`, org, id).Scan(&total)
	}
	if err != nil {
		return nil, 0, err
	}
	return out, total, rows.Err()
}
