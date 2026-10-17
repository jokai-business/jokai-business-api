package notification

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Service struct {
	DB        *pgxpool.Pool
	Mailer    EmailSender
	EmailFrom string
}

const visible = `organization_id=$1 AND user_id=$2 AND (required_permission='' OR required_permission=ANY($3::text[]))`

func pageOK(p, z int) bool { return p >= 1 && p <= 1000000 && z >= 1 && z <= 100 }
func (s *Service) List(ctx context.Context, a user.Context, filter string, p, z int) ([]Notification, int, error) {
	if !pageOK(p, z) || (filter != "active" && filter != "unread" && filter != "all") {
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := visible + ` AND ($4='all' OR resolved_at IS NULL) AND ($4<>'unread' OR read_at IS NULL)`
	args := []any{a.Organization.ID, a.User.ID, a.Permissions, filter}
	var total int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE `+where, args...).Scan(&total); e != nil {
		return nil, 0, e
	}
	args = append(args, z, (p-1)*z)
	rows, e := tx.Query(ctx, `SELECT id,kind,title,body,href,created_at,read_at,resolved_at FROM notifications WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6`, args...)
	if e != nil {
		return nil, 0, e
	}
	out := []Notification{}
	for rows.Next() {
		var n Notification
		if e = rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Href, &n.CreatedAt, &n.ReadAt, &n.ResolvedAt); e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, n)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func (s *Service) Unread(ctx context.Context, a user.Context) (int, error) {
	var count int
	e := s.DB.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE `+visible+` AND read_at IS NULL AND resolved_at IS NULL`, a.Organization.ID, a.User.ID, a.Permissions).Scan(&count)
	return count, e
}
func (s *Service) Read(ctx context.Context, a user.Context, id string, all bool) error {
	if !all && !customer.UUID(id) {
		return user.ErrInvalid
	}
	q := `UPDATE notifications SET read_at=COALESCE(read_at,now()) WHERE ` + visible
	args := []any{a.Organization.ID, a.User.ID, a.Permissions}
	if !all {
		q += ` AND id=$4`
		args = append(args, id)
	} else {
		q += ` AND resolved_at IS NULL AND read_at IS NULL`
	}
	tag, e := s.DB.Exec(ctx, q, args...)
	if e == nil && !all && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return e
}
func scanReminder(row pgx.Row) (Reminder, error) {
	var r Reminder
	e := row.Scan(&r.ID, &r.Title, &r.Notes, &r.DueAt, &r.Status, &r.CreatedAt, &r.DeliveredAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return r, e
}

const reminderColumns = `id,title,notes,due_at,status,created_at,delivered_at`

func (s *Service) Create(ctx context.Context, a user.Context, in Input, rid string) (Reminder, error) {
	if e := Validate(&in, time.Now()); e != nil {
		return Reminder{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Reminder{}, e
	}
	defer tx.Rollback(ctx)
	var id string
	e = tx.QueryRow(ctx, `SELECT id FROM users WHERE organization_id=$1 AND id=$2 AND active FOR UPDATE`, a.Organization.ID, a.User.ID).Scan(&id)
	if e != nil {
		return Reminder{}, e
	}
	var count int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM reminders WHERE organization_id=$1 AND user_id=$2 AND status='scheduled'`, a.Organization.ID, a.User.ID).Scan(&count)
	if e != nil {
		return Reminder{}, e
	}
	if count >= 100 {
		return Reminder{}, ErrConflict
	}
	r, e := scanReminder(tx.QueryRow(ctx, `INSERT INTO reminders(organization_id,user_id,title,notes,due_at) VALUES($1,$2,$3,$4,$5) RETURNING `+reminderColumns, a.Organization.ID, a.User.ID, in.Title, in.Notes, in.DueAt))
	if e != nil {
		return r, e
	}
	if e = audit(ctx, tx, a, "reminder.created", r.ID, rid); e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (s *Service) Cancel(ctx context.Context, a user.Context, id, rid string) (Reminder, error) {
	if !customer.UUID(id) {
		return Reminder{}, user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Reminder{}, e
	}
	defer tx.Rollback(ctx)
	r, e := scanReminder(tx.QueryRow(ctx, `SELECT `+reminderColumns+` FROM reminders WHERE organization_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, a.Organization.ID, a.User.ID, id))
	if e != nil {
		return r, e
	}
	if r.Status != "scheduled" {
		return r, ErrConflict
	}
	r, e = scanReminder(tx.QueryRow(ctx, `UPDATE reminders SET status='cancelled' WHERE organization_id=$1 AND user_id=$2 AND id=$3 RETURNING `+reminderColumns, a.Organization.ID, a.User.ID, id))
	if e != nil {
		return r, e
	}
	if e = audit(ctx, tx, a, "reminder.cancelled", id, rid); e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (s *Service) Reminders(ctx context.Context, a user.Context, p, z int) ([]Reminder, int, error) {
	if !pageOK(p, z) {
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	var total int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM reminders WHERE organization_id=$1 AND user_id=$2`, a.Organization.ID, a.User.ID).Scan(&total)
	if e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+reminderColumns+` FROM reminders WHERE organization_id=$1 AND user_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, a.Organization.ID, a.User.ID, z, (p-1)*z)
	if e != nil {
		return nil, 0, e
	}
	out := []Reminder{}
	for rows.Next() {
		r, e := scanReminder(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, action, id, rid string) error {
	_, e := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id) VALUES($1,$2,$3,$4,$5)`, a.Organization.ID, a.User.ID, action, id, rid)
	return e
}
