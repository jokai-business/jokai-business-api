package scope

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

type Service struct{ DB *pgxpool.Pool }

const columns = `s.id,s.opportunity_id,s.customer_id,s.title,s.objectives,s.customer_name,s.opportunity_title,s.creator_id,s.edited_by,u.name,COALESCE(s.submitted_by::text,''),s.status,s.revision,s.version,s.created_at,s.updated_at,ARRAY(SELECT p.user_id::text FROM scope_preparers p WHERE p.organization_id=s.organization_id AND p.scope_id=s.id AND p.revision=s.revision ORDER BY p.user_id),(SELECT count(*) FROM scope_contributions c WHERE c.organization_id=s.organization_id AND c.scope_id=s.id)`
const joined = ` FROM service_scopes s JOIN users u ON u.organization_id=s.organization_id AND u.id=s.creator_id`

func scan(row pgx.Row) (Scope, error) {
	v := Scope{Contributions: []Contribution{}}
	e := row.Scan(&v.ID, &v.OpportunityID, &v.CustomerID, &v.Title, &v.Objectives, &v.CustomerName, &v.OpportunityTitle, &v.CreatorID, &v.EditedBy, &v.CreatorName, &v.SubmittedBy, &v.Status, &v.Revision, &v.Version, &v.CreatedAt, &v.UpdatedAt, &v.PreparedBy, &v.ContributionCount)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return v, e
}
func load(ctx context.Context, tx pgx.Tx, org, id string, lock bool) (Scope, error) {
	if lock {
		var found string
		e := tx.QueryRow(ctx, `SELECT id FROM service_scopes WHERE organization_id=$1 AND id=$2 FOR UPDATE`, org, id).Scan(&found)
		if errors.Is(e, pgx.ErrNoRows) {
			return Scope{}, ErrNotFound
		}
		if e != nil {
			return Scope{}, e
		}
	}
	v, e := scan(tx.QueryRow(ctx, `SELECT `+columns+joined+` WHERE s.organization_id=$1 AND s.id=$2`, org, id))
	if e != nil {
		return v, e
	}
	rows, e := tx.Query(ctx, `SELECT c.id,c.team,c.service,c.deliverables,c.assumptions,c.exclusions,c.timeline,c.updated_by,u.name,c.updated_at FROM scope_contributions c JOIN users u ON u.organization_id=c.organization_id AND u.id=c.updated_by WHERE c.organization_id=$1 AND c.scope_id=$2 ORDER BY c.created_at,c.id`, org, id)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var c Contribution
		if e = rows.Scan(&c.ID, &c.Team, &c.Service, &c.Deliverables, &c.Assumptions, &c.Exclusions, &c.Timeline, &c.UpdatedBy, &c.UpdatedByName, &c.UpdatedAt); e != nil {
			return v, e
		}
		c.Version = v.Version
		v.Contributions = append(v.Contributions, c)
	}
	return v, rows.Err()
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, action, id, rid string, before, after any) error {
	switch action {
	case "scope.created", "scope.updated", "scope.contribution_added", "scope.contribution_updated", "scope.contribution_removed", "scope.submit", "scope.revise":
		_, e := tx.Exec(ctx, `INSERT INTO scope_preparers(organization_id,scope_id,revision,user_id) SELECT organization_id,id,revision,$3 FROM service_scopes WHERE organization_id=$1 AND id=$2 ON CONFLICT DO NOTHING`, a.Organization.ID, id, a.User.ID)
		if e != nil {
			return e
		}
	}

	var b any
	if before != nil {
		x, _ := json.Marshal(before)
		b = x
	}
	n, _ := json.Marshal(after)
	_, e := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7)`, a.Organization.ID, a.User.ID, action, id, rid, b, n)
	return e
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Scope, error) {
	if !a.Allows("scope.read") {
		return Scope{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Scope{}, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Scope{}, e
	}
	defer tx.Rollback(ctx)
	v, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) List(ctx context.Context, a user.Context, q, status, opp, cust string, p, z int) ([]Scope, int, error) {
	if !a.Allows("scope.read") {
		return nil, 0, user.ErrForbidden
	}
	if !pageValid(p, z) || !product.Text(q, 200) || (opp != "" && !customer.UUID(opp)) || (cust != "" && !customer.UUID(cust)) {
		return nil, 0, user.ErrInvalid
	}
	switch status {
	case "", "draft", "in_review", "changes_requested", "approved":
	default:
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	where := ` WHERE s.organization_id=$1 AND ($2='' OR strpos(lower(s.title||' '||s.customer_name||' '||s.opportunity_title),lower($2))>0) AND ($3='' OR s.status=$3) AND ($4='' OR s.opportunity_id=NULLIF($4,'')::uuid) AND ($5='' OR s.customer_id=NULLIF($5,'')::uuid)`
	n := 0
	if e = tx.QueryRow(ctx, `SELECT count(*)`+joined+where, a.Organization.ID, q, status, opp, cust).Scan(&n); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+columns+joined+where+` ORDER BY s.created_at DESC,s.id LIMIT $6 OFFSET $7`, a.Organization.ID, q, status, opp, cust, z, (p-1)*z)
	if e != nil {
		return nil, 0, e
	}
	out := []Scope{}
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
func (s *Service) Create(ctx context.Context, a user.Context, in Input, rid string) (Scope, error) {
	if !a.Allows("scope.create") {
		return Scope{}, user.ErrForbidden
	}
	if e := Validate(&in); e != nil {
		return Scope{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Scope{}, e
	}
	defer tx.Rollback(ctx)
	var cust, name, title string
	e = tx.QueryRow(ctx, `SELECT o.customer_id,c.name,o.title FROM opportunities o JOIN customers c ON c.organization_id=o.organization_id AND c.id=o.customer_id WHERE o.organization_id=$1 AND o.id=$2 FOR UPDATE OF o`, a.Organization.ID, in.OpportunityID).Scan(&cust, &name, &title)
	if errors.Is(e, pgx.ErrNoRows) {
		return Scope{}, user.ErrInvalid
	}
	if e != nil {
		return Scope{}, e
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_scopes WHERE organization_id=$1 AND opportunity_id=$2)`, a.Organization.ID, in.OpportunityID).Scan(&exists); e != nil {
		return Scope{}, e
	}
	if exists {
		return Scope{}, ErrExists
	}
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO service_scopes(organization_id,opportunity_id,customer_id,creator_id,edited_by,title,objectives,customer_name,opportunity_title) VALUES($1,$2,$3,$4,$4,$5,$6,$7,$8) RETURNING id`, a.Organization.ID, in.OpportunityID, cust, a.User.ID, in.Title, in.Objectives, name, title).Scan(&id)
	if e != nil {
		return Scope{}, e
	}
	v, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, "scope.created", id, rid, nil, v); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Edit(ctx context.Context, a user.Context, id string, in Edit, rid string) (Scope, error) {
	if !a.Allows("scope.update") {
		return Scope{}, user.ErrForbidden
	}
	if !customer.UUID(id) || in.Version < 1 {
		return Scope{}, user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Scope{}, e
	}
	defer tx.Rollback(ctx)
	old, e := load(ctx, tx, a.Organization.ID, id, true)
	if e != nil {
		return old, e
	}
	if old.Version != in.Version || !Editable(old.Status) {
		return old, ErrConflict
	}
	next := Input{OpportunityID: old.OpportunityID, Title: in.Title, Objectives: in.Objectives}
	if e = Validate(&next); e != nil {
		return old, e
	}
	_, e = tx.Exec(ctx, `UPDATE service_scopes SET title=$3,objectives=$4,edited_by=$5,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id, next.Title, next.Objectives, a.User.ID)
	if e != nil {
		return old, e
	}
	v, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, "scope.updated", id, rid, old, v); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Contribution(ctx context.Context, a user.Context, id, cid string, in ContributionInput, remove bool, rid string) (Scope, error) {
	if !a.Allows("scope.update") {
		return Scope{}, user.ErrForbidden
	}
	if !customer.UUID(id) || (cid != "" && !customer.UUID(cid)) || in.Version < 1 || (remove && cid == "") {
		return Scope{}, user.ErrInvalid
	}
	if !remove {
		if e := ValidateContribution(&in); e != nil {
			return Scope{}, e
		}
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Scope{}, e
	}
	defer tx.Rollback(ctx)
	old, e := load(ctx, tx, a.Organization.ID, id, true)
	if e != nil {
		return old, e
	}
	if old.Version != in.Version || !Editable(old.Status) {
		return old, ErrConflict
	}
	if cid != "" {
		found := false
		for _, c := range old.Contributions {
			if c.ID == cid {
				found = true
			}
		}
		if !found {
			return old, ErrNotFound
		}
	} else if len(old.Contributions) >= 20 {
		return old, user.ErrInvalid
	}
	action := "scope.contribution_added"
	if remove {
		_, e = tx.Exec(ctx, `DELETE FROM scope_contributions WHERE organization_id=$1 AND scope_id=$2 AND id=$3`, a.Organization.ID, id, cid)
		action = "scope.contribution_removed"
	} else if cid == "" {
		_, e = tx.Exec(ctx, `INSERT INTO scope_contributions(organization_id,scope_id,team,service,deliverables,assumptions,exclusions,timeline,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.Organization.ID, id, in.Team, in.Service, in.Deliverables, in.Assumptions, in.Exclusions, in.Timeline, a.User.ID)
	} else {
		_, e = tx.Exec(ctx, `UPDATE scope_contributions SET team=$4,service=$5,deliverables=$6,assumptions=$7,exclusions=$8,timeline=$9,updated_by=$10,updated_at=now() WHERE organization_id=$1 AND scope_id=$2 AND id=$3`, a.Organization.ID, id, cid, in.Team, in.Service, in.Deliverables, in.Assumptions, in.Exclusions, in.Timeline, a.User.ID)
		action = "scope.contribution_updated"
	}
	if e != nil {
		return old, e
	}
	_, e = tx.Exec(ctx, `UPDATE service_scopes SET version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id)
	if e != nil {
		return old, e
	}
	v, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, action, id, rid, old, v); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Act(ctx context.Context, a user.Context, id, action string, in Action, rid string) (Scope, error) {
	permission := "scope.update"
	if action == "approve" || action == "request-changes" {
		permission = "scope.approve"
	}
	if !a.Allows(permission) {
		return Scope{}, user.ErrForbidden
	}
	in.Note = strings.TrimSpace(in.Note)
	if !customer.UUID(id) || in.Version < 1 || !product.Text(in.Note, 2000) {
		return Scope{}, user.ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Scope{}, e
	}
	defer tx.Rollback(ctx)
	old, e := load(ctx, tx, a.Organization.ID, id, true)
	if e != nil {
		return old, e
	}
	if old.Version != in.Version {
		return old, ErrConflict
	}
	status, e := Transition(old, a, action, in.Note)
	if e != nil {
		return old, e
	}
	revision := old.Revision
	submitted := old.SubmittedBy
	if action == "submit" {
		submitted = a.User.ID
	}
	if action == "revise" {
		revision++
		submitted = ""
	}
	_, e = tx.Exec(ctx, `UPDATE service_scopes SET status=$3,revision=$4,submitted_by=NULLIF($5,'')::uuid,version=version+1,updated_at=now() WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id, status, revision, submitted)
	if e != nil {
		return old, e
	}
	v, e := load(ctx, tx, a.Organization.ID, id, false)
	if e != nil {
		return v, e
	}
	if action == "approve" {
		payload, e := json.Marshal(v)
		if e != nil {
			return v, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO scope_revisions(organization_id,scope_id,revision,snapshot,approved_by) VALUES($1,$2,$3,$4,$5)`, a.Organization.ID, id, revision, payload, a.User.ID)
		if e != nil {
			return v, e
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO scope_reviews(organization_id,scope_id,revision,action,note,actor_id) VALUES($1,$2,$3,$4,$5,$6)`, a.Organization.ID, id, revision, action, in.Note, a.User.ID)
	if e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, "scope."+action, id, rid, old, v); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Snapshot(ctx context.Context, a user.Context, id string, revision int) (Scope, error) {
	if !a.Allows("scope.read") {
		return Scope{}, user.ErrForbidden
	}
	if !customer.UUID(id) || revision < 1 {
		return Scope{}, user.ErrInvalid
	}
	var data []byte
	e := s.DB.QueryRow(ctx, `SELECT snapshot FROM scope_revisions WHERE organization_id=$1 AND scope_id=$2 AND revision=$3`, a.Organization.ID, id, revision).Scan(&data)
	if errors.Is(e, pgx.ErrNoRows) {
		return Scope{}, ErrNotFound
	}
	if e != nil {
		return Scope{}, e
	}
	var v Scope
	e = json.Unmarshal(data, &v)
	if v.PreparedBy == nil {
		v.PreparedBy = []string{}
	}
	return v, e
}
func (s *Service) History(ctx context.Context, a user.Context, id string, p, z int) ([]Review, int, error) {
	if !a.Allows("scope.read") {
		return nil, 0, user.ErrForbidden
	}
	if !customer.UUID(id) || !pageValid(p, z) {
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	if _, e = load(ctx, tx, a.Organization.ID, id, false); e != nil {
		return nil, 0, e
	}
	n := 0
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM scope_reviews WHERE organization_id=$1 AND scope_id=$2`, a.Organization.ID, id).Scan(&n); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT r.id,r.revision,r.action,r.note,u.name,r.created_at FROM scope_reviews r JOIN users u ON u.organization_id=r.organization_id AND u.id=r.actor_id WHERE r.organization_id=$1 AND r.scope_id=$2 ORDER BY r.created_at DESC,r.id LIMIT $3 OFFSET $4`, a.Organization.ID, id, z, (p-1)*z)
	if e != nil {
		return nil, 0, e
	}
	out := []Review{}
	for rows.Next() {
		var v Review
		if e = rows.Scan(&v.ID, &v.Revision, &v.Action, &v.Note, &v.ActorName, &v.CreatedAt); e != nil {
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
