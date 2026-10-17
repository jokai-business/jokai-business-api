package project

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ DB *pgxpool.Pool }

const columns = `p.id,p.customer_id,c.name,p.opportunity_id,o.title,COALESCE(p.quote_id::text,''),COALESCE(q.number,''),p.name,p.owner_id,u.name,COALESCE(p.start_date::text,''),COALESCE(p.target_date::text,''),p.status,p.notes,p.version,p.created_at,p.updated_at,(SELECT count(*) FROM tasks t WHERE t.organization_id=p.organization_id AND t.project_id=p.id),(SELECT count(*) FROM tasks t WHERE t.organization_id=p.organization_id AND t.project_id=p.id AND t.status='done')`
const joins = ` FROM projects p JOIN customers c ON c.organization_id=p.organization_id AND c.id=p.customer_id JOIN opportunities o ON o.organization_id=p.organization_id AND o.id=p.opportunity_id JOIN users u ON u.organization_id=p.organization_id AND u.id=p.owner_id LEFT JOIN quotes q ON q.organization_id=p.organization_id AND q.id=p.quote_id`

func scan(row pgx.Row) (Project, error) {
	var p Project
	e := row.Scan(&p.ID, &p.CustomerID, &p.CustomerName, &p.OpportunityID, &p.OpportunityTitle, &p.QuoteID, &p.QuoteNumber, &p.Name, &p.OwnerID, &p.OwnerName, &p.StartDate, &p.TargetDate, &p.Status, &p.Notes, &p.Version, &p.CreatedAt, &p.UpdatedAt, &p.TaskCount, &p.DoneCount)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	p.Progress = Progress(p.DoneCount, p.TaskCount)
	return p, e
}
func load(ctx context.Context, tx pgx.Tx, org, id string) (Project, error) {
	return scan(tx.QueryRow(ctx, `SELECT `+columns+joins+` WHERE p.organization_id=$1 AND p.id=$2`, org, id))
}
func lock(ctx context.Context, tx pgx.Tx, org, id string) error {
	var found string
	e := tx.QueryRow(ctx, `SELECT id FROM projects WHERE organization_id=$1 AND id=$2 FOR UPDATE`, org, id).Scan(&found)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return e
}
func (r *Repository) Get(ctx context.Context, org, id string) (Project, error) {
	return scan(r.DB.QueryRow(ctx, `SELECT `+columns+joins+` WHERE p.organization_id=$1 AND p.id=$2`, org, id))
}
func (r *Repository) List(ctx context.Context, org, q, status, customerID, opportunityID string, page, size int) ([]Project, int, error) {
	tx, e := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := ` WHERE p.organization_id=$1 AND ($2='' OR strpos(lower(p.name||' '||c.name),lower($2))>0) AND ($3='' OR p.status=$3) AND ($4='' OR p.customer_id=NULLIF($4,'')::uuid) AND ($5='' OR p.opportunity_id=NULLIF($5,'')::uuid)`
	total := 0
	if e = tx.QueryRow(ctx, `SELECT count(*)`+joins+where, org, q, status, customerID, opportunityID).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+columns+joins+where+` ORDER BY p.created_at DESC,p.id LIMIT $6 OFFSET $7`, org, q, status, customerID, opportunityID, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []Project{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, p)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func (r *Repository) Sources(ctx context.Context, org, q string, page, size int) ([]Source, int, error) {
	tx, e := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	from := ` FROM opportunities o JOIN customers c ON c.organization_id=o.organization_id AND c.id=o.customer_id JOIN pipeline_stages s ON s.organization_id=o.organization_id AND s.id=o.stage_id LEFT JOIN LATERAL (SELECT id,number FROM quotes WHERE organization_id=o.organization_id AND opportunity_id=o.id AND customer_id=o.customer_id AND status='accepted' ORDER BY created_at DESC,id LIMIT 1) accepted ON true WHERE o.organization_id=$1 AND (s.is_won OR accepted.id IS NOT NULL) AND NOT EXISTS(SELECT 1 FROM projects p WHERE p.organization_id=o.organization_id AND p.opportunity_id=o.id) AND ($2='' OR strpos(lower(o.title||' '||c.name||' '||COALESCE(accepted.number,'')),lower($2))>0)`
	total := 0
	if e = tx.QueryRow(ctx, `SELECT count(*)`+from, org, q).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT o.id,o.title,c.id,c.name,COALESCE(accepted.id::text,''),COALESCE(accepted.number,''),s.is_won`+from+` ORDER BY o.updated_at DESC,o.id LIMIT $3 OFFSET $4`, org, q, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []Source{}
	for rows.Next() {
		var s Source
		if e = rows.Scan(&s.OpportunityID, &s.OpportunityTitle, &s.CustomerID, &s.CustomerName, &s.QuoteID, &s.QuoteNumber, &s.IsWon); e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, s)
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
func owner(ctx context.Context, tx pgx.Tx, org, id string) error {
	var name string
	e := tx.QueryRow(ctx, `SELECT name FROM users WHERE organization_id=$1 AND id=$2 AND active FOR SHARE`, org, id).Scan(&name)
	if errors.Is(e, pgx.ErrNoRows) {
		return user.ErrInvalid
	}
	return e
}
func (r *Repository) Create(ctx context.Context, a user.Context, in Input, rid string) (Project, error) {
	tx, e := r.DB.Begin(ctx)
	if e != nil {
		return Project{}, e
	}
	defer tx.Rollback(ctx)
	var customerID string
	var won bool
	e = tx.QueryRow(ctx, `SELECT o.customer_id,s.is_won FROM opportunities o JOIN pipeline_stages s ON s.organization_id=o.organization_id AND s.id=o.stage_id WHERE o.organization_id=$1 AND o.id=$2 FOR UPDATE OF o FOR SHARE OF s`, a.Organization.ID, in.OpportunityID).Scan(&customerID, &won)
	if errors.Is(e, pgx.ErrNoRows) {
		return Project{}, user.ErrInvalid
	}
	if e != nil {
		return Project{}, e
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE organization_id=$1 AND opportunity_id=$2)`, a.Organization.ID, in.OpportunityID).Scan(&exists); e != nil {
		return Project{}, e
	}
	if exists {
		return Project{}, ErrExists
	}
	if in.QuoteID != "" {
		var found string
		e = tx.QueryRow(ctx, `SELECT id FROM quotes WHERE organization_id=$1 AND id=$2 AND opportunity_id=$3 AND customer_id=$4 AND status='accepted' FOR SHARE`, a.Organization.ID, in.QuoteID, in.OpportunityID, customerID).Scan(&found)
		if errors.Is(e, pgx.ErrNoRows) {
			return Project{}, ErrNotEligible
		}
		if e != nil {
			return Project{}, e
		}
	} else if !won {
		return Project{}, ErrNotEligible
	}
	if e = owner(ctx, tx, a.Organization.ID, in.OwnerID); e != nil {
		return Project{}, e
	}
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO projects(organization_id,customer_id,opportunity_id,quote_id,name,owner_id,start_date,target_date,notes) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,NULLIF($7,'')::date,NULLIF($8,'')::date,$9) RETURNING id`, a.Organization.ID, customerID, in.OpportunityID, in.QuoteID, in.Name, in.OwnerID, in.StartDate, in.TargetDate, in.Notes).Scan(&id)
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" {
		return Project{}, ErrExists
	}
	if e != nil {
		return Project{}, e
	}
	p, e := load(ctx, tx, a.Organization.ID, id)
	if e != nil {
		return p, e
	}
	if e = audit(ctx, tx, a, "project.created", id, rid, nil, p); e != nil {
		return p, e
	}
	return p, tx.Commit(ctx)
}
func (r *Repository) Update(ctx context.Context, a user.Context, id string, in Patch, rid string) (Project, error) {
	tx, e := r.DB.Begin(ctx)
	if e != nil {
		return Project{}, e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, a.Organization.ID, id); e != nil {
		return Project{}, e
	}
	before, e := load(ctx, tx, a.Organization.ID, id)
	if e != nil {
		return Project{}, e
	}
	if before.Version != in.Version {
		return Project{}, ErrConflict
	}
	p, e := ApplyPatch(before, in)
	if e != nil {
		return p, e
	}
	if p.Status == "completed" && (p.TaskCount == 0 || p.DoneCount != p.TaskCount) {
		return p, ErrIncomplete
	}
	if e = owner(ctx, tx, a.Organization.ID, p.OwnerID); e != nil {
		return p, e
	}
	_, e = tx.Exec(ctx, `UPDATE projects SET name=$3,owner_id=$4,start_date=NULLIF($5,'')::date,target_date=NULLIF($6,'')::date,status=$7,notes=$8,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id, p.Name, p.OwnerID, p.StartDate, p.TargetDate, p.Status, p.Notes)
	if e != nil {
		return p, e
	}
	p, e = load(ctx, tx, a.Organization.ID, id)
	if e != nil {
		return p, e
	}
	if e = audit(ctx, tx, a, "project.updated", id, rid, before, p); e != nil {
		return p, e
	}
	return p, tx.Commit(ctx)
}

const taskColumns = `t.id,t.project_id,t.title,COALESCE(t.assignee_id::text,''),COALESCE(u.name,''),COALESCE(t.due_date::text,''),t.status,t.priority,t.notes,t.version,t.created_at,t.updated_at`
const taskJoin = ` FROM tasks t LEFT JOIN users u ON u.organization_id=t.organization_id AND u.id=t.assignee_id`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	e := row.Scan(&t.ID, &t.ProjectID, &t.Title, &t.AssigneeID, &t.AssigneeName, &t.DueDate, &t.Status, &t.Priority, &t.Notes, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return t, e
}
func (r *Repository) Tasks(ctx context.Context, org, id, status, assignee string, page, size int) ([]Task, int, error) {
	tx, e := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := ` WHERE t.organization_id=$1 AND t.project_id=$2 AND ($3='' OR t.status=$3) AND ($4='' OR t.assignee_id=NULLIF($4,'')::uuid)`
	total := 0
	if e = tx.QueryRow(ctx, `SELECT count(*)`+taskJoin+where, org, id, status, assignee).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+taskColumns+taskJoin+where+` ORDER BY CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END,t.due_date NULLS LAST,t.created_at,t.id LIMIT $5 OFFSET $6`, org, id, status, assignee, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, t)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func (r *Repository) AddTask(ctx context.Context, a user.Context, id string, in TaskInput, rid string) (Task, error) {
	return r.saveTask(ctx, a, id, "", in, nil, rid)
}
func (r *Repository) UpdateTask(ctx context.Context, a user.Context, id, taskID string, in TaskPatch, rid string) (Task, error) {
	return r.saveTask(ctx, a, id, taskID, TaskInput{}, &in, rid)
}
func (r *Repository) saveTask(ctx context.Context, a user.Context, id, taskID string, in TaskInput, patch *TaskPatch, rid string) (Task, error) {
	tx, e := r.DB.Begin(ctx)
	if e != nil {
		return Task{}, e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, a.Organization.ID, id); e != nil {
		return Task{}, e
	}
	var state string
	if e = tx.QueryRow(ctx, `SELECT status FROM projects WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id).Scan(&state); e != nil {
		return Task{}, e
	}
	var before any
	action := "project.task_created"
	if patch != nil {
		old, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+taskJoin+` WHERE t.organization_id=$1 AND t.project_id=$2 AND t.id=$3`, a.Organization.ID, id, taskID))
		if err != nil {
			return Task{}, err
		}
		if old.Version != patch.Version {
			return Task{}, ErrConflict
		}
		updated, err := ApplyTaskPatch(old, *patch)
		if err != nil {
			return Task{}, err
		}
		in = updated.TaskInput
		before = old
		action = "project.task_updated"
	}
	if state == "completed" {
		return Task{}, ErrClosed
	}
	if in.AssigneeID != "" {
		if e = owner(ctx, tx, a.Organization.ID, in.AssigneeID); e != nil {
			return Task{}, e
		}
	}
	if patch == nil {
		e = tx.QueryRow(ctx, `INSERT INTO tasks(organization_id,project_id,title,assignee_id,due_date,status,priority,notes) VALUES($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::date,$6,$7,$8) RETURNING id`, a.Organization.ID, id, in.Title, in.AssigneeID, in.DueDate, in.Status, in.Priority, in.Notes).Scan(&taskID)
	} else {
		_, e = tx.Exec(ctx, `UPDATE tasks SET title=$4,assignee_id=NULLIF($5,'')::uuid,due_date=NULLIF($6,'')::date,status=$7,priority=$8,notes=$9,version=version+1,updated_at=now() WHERE organization_id=$1 AND project_id=$2 AND id=$3`, a.Organization.ID, id, taskID, in.Title, in.AssigneeID, in.DueDate, in.Status, in.Priority, in.Notes)
	}
	if e != nil {
		return Task{}, e
	}
	t, e := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+taskJoin+` WHERE t.organization_id=$1 AND t.project_id=$2 AND t.id=$3`, a.Organization.ID, id, taskID))
	if e != nil {
		return t, e
	}
	if _, e = tx.Exec(ctx, `UPDATE projects SET version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id); e != nil {
		return t, e
	}
	if e = audit(ctx, tx, a, action, id, rid, before, t); e != nil {
		return t, e
	}
	return t, tx.Commit(ctx)
}
func (r *Repository) Activity(ctx context.Context, org, id string, p, z int) ([]user.Audit, int, error) {
	return (&customer.Repository{DB: r.DB}).Activity(ctx, org, id, p, z)
}
