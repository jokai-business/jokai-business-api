package opportunity

import (
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ DB *pgxpool.Pool }

const columns = `o.id,o.customer_id,c.name,o.title,o.requirement,o.stage_id,s.name,s.is_closed,s.is_won,o.expected_value::text,o.currency,o.probability,COALESCE(o.expected_close_date::text,''),o.owner_id,u.name,o.source,o.next_action,o.version,o.created_at,o.updated_at`
const joins = ` FROM opportunities o JOIN customers c ON c.organization_id=o.organization_id AND c.id=o.customer_id JOIN users u ON u.organization_id=o.organization_id AND u.id=o.owner_id JOIN pipeline_stages s ON s.organization_id=o.organization_id AND s.id=o.stage_id`

func scan(row pgx.Row) (Opportunity, error) {
	var v Opportunity
	e := row.Scan(&v.ID, &v.CustomerID, &v.CustomerName, &v.Title, &v.Requirement, &v.StageID, &v.StageName, &v.IsClosed, &v.IsWon, &v.ExpectedValue, &v.Currency, &v.Probability, &v.ExpectedCloseDate, &v.OwnerID, &v.OwnerName, &v.Source, &v.NextAction, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return v, e
}
func (r *Repository) Get(ctx context.Context, org, id string) (Opportunity, error) {
	return scan(r.DB.QueryRow(ctx, `SELECT `+columns+joins+` WHERE o.organization_id=$1 AND o.id=$2`, org, id))
}
func (r *Repository) List(ctx context.Context, org, q, stage, customer string, p, z int) ([]Opportunity, int, error) {
	tx, e := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := ` WHERE o.organization_id=$1 AND ($2='' OR strpos(lower(o.title||' '||c.name),lower($2))>0) AND ($3='' OR o.stage_id=NULLIF($3,'')::uuid) AND ($4='' OR o.customer_id=NULLIF($4,'')::uuid)`
	total := 0
	if e = tx.QueryRow(ctx, `SELECT count(*)`+joins+where, org, q, stage, customer).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+columns+joins+where+` ORDER BY o.created_at DESC,o.id LIMIT $5 OFFSET $6`, org, q, stage, customer, z, (p-1)*z)
	if e != nil {
		return nil, 0, e
	}
	out := []Opportunity{}
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
func activity(ctx context.Context, tx pgx.Tx, a user.Context, id, kind, body string) (Activity, error) {
	v := Activity{Kind: kind, Body: body, ActorName: a.User.Name}
	e := tx.QueryRow(ctx, `INSERT INTO opportunity_activities(organization_id,opportunity_id,actor_id,kind,body) VALUES($1,$2,$3,$4,$5) RETURNING id,created_at`, a.Organization.ID, id, a.User.ID, kind, body).Scan(&v.ID, &v.CreatedAt)
	return v, e
}
func (r *Repository) Save(ctx context.Context, a user.Context, v Opportunity, before *Opportunity, rid string) (Opportunity, error) {
	tx, e := r.DB.Begin(ctx)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	e = tx.QueryRow(ctx, `SELECT c.name,u.name,s.name,s.is_closed,s.is_won FROM customers c JOIN users u ON u.organization_id=c.organization_id AND u.id=$3 AND u.active JOIN pipeline_stages s ON s.organization_id=c.organization_id AND s.id=$4 WHERE c.organization_id=$1 AND c.id=$2 FOR SHARE OF c,u,s`, a.Organization.ID, v.CustomerID, v.OwnerID, v.StageID).Scan(&v.CustomerName, &v.OwnerName, &v.StageName, &v.IsClosed, &v.IsWon)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, user.ErrInvalid
	}
	if e != nil {
		return v, e
	}
	kind, body := "created", "Opportunity created"
	if before == nil {
		e = tx.QueryRow(ctx, `INSERT INTO opportunities(organization_id,customer_id,title,requirement,stage_id,expected_value,currency,probability,expected_close_date,owner_id,source,next_action) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::date,$10,$11,$12) RETURNING id,version,created_at,updated_at,expected_value::text`, a.Organization.ID, v.CustomerID, v.Title, v.Requirement, v.StageID, v.ExpectedValue, v.Currency, v.Probability, v.ExpectedCloseDate, v.OwnerID, v.Source, v.NextAction).Scan(&v.ID, &v.Version, &v.CreatedAt, &v.UpdatedAt, &v.ExpectedValue)
	} else {
		kind, body = "updated", "Opportunity updated"
		if before.StageID != v.StageID {
			kind, body = "stage_changed", "Stage changed from "+before.StageName+" to "+v.StageName
		}
		e = tx.QueryRow(ctx, `UPDATE opportunities SET customer_id=$3,title=$4,requirement=$5,stage_id=$6,expected_value=$7,currency=$8,probability=$9,expected_close_date=NULLIF($10,'')::date,owner_id=$11,source=$12,next_action=$13,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2 AND version=$14 RETURNING version,updated_at,expected_value::text`, a.Organization.ID, v.ID, v.CustomerID, v.Title, v.Requirement, v.StageID, v.ExpectedValue, v.Currency, v.Probability, v.ExpectedCloseDate, v.OwnerID, v.Source, v.NextAction, before.Version).Scan(&v.Version, &v.UpdatedAt, &v.ExpectedValue)
		if errors.Is(e, pgx.ErrNoRows) {
			return v, ErrConflict
		}
	}
	if e != nil {
		return v, e
	}
	var old any
	if before != nil {
		old = before
	}
	if e = audit(ctx, tx, a, "opportunity."+kind, v.ID, rid, old, v); e != nil {
		return v, e
	}
	if _, e = activity(ctx, tx, a, v.ID, kind, body); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func scanStage(row pgx.Row) (Stage, error) {
	var s Stage
	e := row.Scan(&s.ID, &s.Name, &s.Position, &s.IsClosed, &s.IsWon, &s.Version)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return s, e
}
func (r *Repository) Stages(ctx context.Context, org string) ([]Stage, error) {
	rows, e := r.DB.Query(ctx, `SELECT id,name,position,is_closed,is_won,version FROM pipeline_stages WHERE organization_id=$1 ORDER BY position,id`, org)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Stage{}
	for rows.Next() {
		v, err := scanStage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) SaveStage(ctx context.Context, a user.Context, id string, in StageInput, rid string) (Stage, error) {
	tx, e := r.DB.Begin(ctx)
	if e != nil {
		return Stage{}, e
	}
	defer tx.Rollback(ctx)
	// Lock the organization's stage configuration while validating its size and uniqueness.
	var locked string
	if e = tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id=$1 FOR NO KEY UPDATE`, a.Organization.ID).Scan(&locked); e != nil {
		return Stage{}, e
	}
	var before any
	if id != "" {
		v, err := scanStage(tx.QueryRow(ctx, `SELECT id,name,position,is_closed,is_won,version FROM pipeline_stages WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id))
		if err != nil {
			return Stage{}, err
		}
		if v.Version != in.Version {
			return Stage{}, ErrConflict
		}
		before = v
	}
	var count, duplicate int
	e = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE lower(name)=lower($2) AND id<>COALESCE(NULLIF($3,'')::uuid,'00000000-0000-0000-0000-000000000000'::uuid)) FROM pipeline_stages WHERE organization_id=$1`, a.Organization.ID, in.Name, id).Scan(&count, &duplicate)
	if e != nil {
		return Stage{}, e
	}
	if duplicate > 0 || (id == "" && count >= 30) {
		return Stage{}, user.ErrInvalid
	}
	var v Stage
	action := "pipeline.stage_created"
	if id == "" {
		v, e = scanStage(tx.QueryRow(ctx, `INSERT INTO pipeline_stages(organization_id,name,position,is_closed,is_won) VALUES($1,$2,$3,$4,$5) RETURNING id,name,position,is_closed,is_won,version`, a.Organization.ID, in.Name, in.Position, in.IsClosed, in.IsWon))
	} else {
		action = "pipeline.stage_updated"
		v, e = scanStage(tx.QueryRow(ctx, `UPDATE pipeline_stages SET name=$3,position=$4,is_closed=$5,is_won=$6,version=version+1 WHERE organization_id=$1 AND id=$2 RETURNING id,name,position,is_closed,is_won,version`, a.Organization.ID, id, in.Name, in.Position, in.IsClosed, in.IsWon))
	}
	if e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, action, v.ID, rid, before, v); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Activities(ctx context.Context, org, id string, p, z int) ([]Activity, int, error) {
	tx, e := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	total := 0
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM opportunity_activities WHERE organization_id=$1 AND opportunity_id=$2`, org, id).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT a.id,a.kind,a.body,u.name,a.created_at FROM opportunity_activities a JOIN users u ON u.organization_id=a.organization_id AND u.id=a.actor_id WHERE a.organization_id=$1 AND a.opportunity_id=$2 ORDER BY a.created_at DESC,a.id LIMIT $3 OFFSET $4`, org, id, z, (p-1)*z)
	if e != nil {
		return nil, 0, e
	}
	out := []Activity{}
	for rows.Next() {
		var v Activity
		if e = rows.Scan(&v.ID, &v.Kind, &v.Body, &v.ActorName, &v.CreatedAt); e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func (r *Repository) AddActivity(ctx context.Context, a user.Context, id, body, rid string) (Activity, error) {
	tx, e := r.DB.Begin(ctx)
	if e != nil {
		return Activity{}, e
	}
	defer tx.Rollback(ctx)
	v, e := activity(ctx, tx, a, id, "note", body)
	if e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, "opportunity.note_added", id, rid, nil, v); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
