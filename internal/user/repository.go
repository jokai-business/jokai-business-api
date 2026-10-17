package user

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ DB *pgxpool.Pool }

// Identity bootstrap is the only lookup without a tenant predicate: subject is unique
// and obtained exclusively from a cryptographically verified token, never the browser.
func (r *Repository) Resolve(ctx context.Context, subject string) (Context, error) {
	var c Context
	err := r.DB.QueryRow(ctx, `SELECT u.id,u.name,u.email,o.id,o.name,o.default_currency,o.timezone,
 ARRAY(SELECT role_key FROM user_roles WHERE organization_id=u.organization_id AND user_id=u.id ORDER BY role_key),
 ARRAY(SELECT DISTINCT rp.permission_key FROM user_roles ur JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE ur.organization_id=u.organization_id AND ur.user_id=u.id ORDER BY rp.permission_key)
 FROM users u JOIN organizations o ON o.id=u.organization_id WHERE u.subject=$1 AND u.active`, subject).Scan(&c.User.ID, &c.User.Name, &c.User.Email, &c.Organization.ID, &c.Organization.Name, &c.Organization.DefaultCurrency, &c.Organization.Timezone, &c.User.Roles, &c.Permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrForbidden
	}
	return c, err
}
func (r *Repository) List(ctx context.Context, org string, page, size int) ([]User, int, error) {
	// A single statement provides a consistent total and page under concurrent inserts.
	rows, err := r.DB.Query(ctx, `WITH page AS (SELECT id,name,email,created_at FROM users WHERE organization_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3)
 SELECT p.id,p.name,p.email,ARRAY(SELECT role_key FROM user_roles WHERE organization_id=$1 AND user_id=p.id ORDER BY role_key),
 (SELECT count(*) FROM users WHERE organization_id=$1) FROM page p ORDER BY p.created_at,p.id`, org, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []User{}
	total := 0
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Name, &u.Email, &u.Roles, &total); err != nil {
			return nil, 0, err
		}
		result = append(result, u)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(result) == 0 {
		err = r.DB.QueryRow(ctx, "SELECT count(*) FROM users WHERE organization_id=$1", org).Scan(&total)
	}
	return result, total, err
}
func (r *Repository) Create(ctx context.Context, actor Context, req CreateRequest, requestID string) (User, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	u := User{Name: req.Name, Email: req.Email, Roles: []string{req.Role}}
	err = tx.QueryRow(ctx, `INSERT INTO users(organization_id,subject,email,name) VALUES($1,$2,$3,$4) RETURNING id`, actor.Organization.ID, req.Subject, req.Email, req.Name).Scan(&u.ID)
	if err != nil {
		var e *pgconn.PgError
		if errors.As(err, &e) && e.Code == "23505" {
			return u, ErrConflict
		}
		return u, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_roles(organization_id,user_id,role_key) VALUES($1,$2,$3)`, actor.Organization.ID, u.ID, req.Role)
	if err != nil {
		return u, err
	}
	after, _ := json.Marshal(u)
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,after_value) VALUES($1,$2,'user.created',$3,$4,$5)`, actor.Organization.ID, actor.User.ID, u.ID, requestID, after)
	if err != nil {
		return u, err
	}
	return u, tx.Commit(ctx)
}
func (r *Repository) Login(ctx context.Context, c Context, session, requestID string) error {
	_, err := r.DB.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,session_id) VALUES($1,$2,'login',$2,$3,$4) ON CONFLICT DO NOTHING`, c.Organization.ID, c.User.ID, requestID, session)
	return err
}
func (r *Repository) Audit(ctx context.Context, org string, page, size int) ([]Audit, int, error) {
	rows, err := r.DB.Query(ctx, `SELECT id,action,actor_id,resource_id,created_at::text,request_id,count(*) OVER() FROM audit_logs WHERE organization_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, org, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []Audit{}
	total := 0
	for rows.Next() {
		var a Audit
		if err = rows.Scan(&a.ID, &a.Action, &a.ActorID, &a.ResourceID, &a.CreatedAt, &a.RequestID, &total); err != nil {
			return nil, 0, err
		}
		result = append(result, a)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(result) == 0 {
		err = r.DB.QueryRow(ctx, "SELECT count(*) FROM audit_logs WHERE organization_id=$1", org).Scan(&total)
	}
	return result, total, err
}
