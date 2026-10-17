package dashboard

import (
	"ai-business-api/internal/document"
	"ai-business-api/internal/user"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Currency struct {
	Currency      string `json:"currency"`
	OpenCount     int    `json:"openCount"`
	WonCount      int    `json:"wonCount"`
	LostCount     int    `json:"lostCount"`
	OpenValue     string `json:"openValue"`
	WeightedValue string `json:"weightedValue"`
	WonValue      string `json:"wonValue"`
}
type Stage struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type Sales struct {
	Currencies []Currency `json:"currencies"`
	Stages     []Stage    `json:"stages"`
}
type Task struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	Title       string `json:"title"`
	DueDate     string `json:"dueDate"`
	Priority    string `json:"priority"`
}
type Tasks struct {
	DueToday int    `json:"dueToday"`
	Overdue  int    `json:"overdue"`
	Items    []Task `json:"items"`
}
type Activity struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Title     string    `json:"title"`
	Href      string    `json:"href"`
	ActorName string    `json:"actorName"`
	CreatedAt time.Time `json:"createdAt"`
}
type Overview struct {
	AsOf     time.Time  `json:"asOf"`
	Today    string     `json:"today"`
	Sales    *Sales     `json:"sales"`
	Tasks    *Tasks     `json:"tasks"`
	Activity []Activity `json:"activity"`
}
type Service struct{ DB *pgxpool.Pool }

func (s *Service) Get(ctx context.Context, a user.Context) (Overview, error) {
	now := time.Now().UTC()
	v := Overview{AsOf: now, Today: now.Format("2006-01-02"), Activity: []Activity{}}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	if a.Allows("opportunity.read") {
		v.Sales = &Sales{Currencies: []Currency{}, Stages: []Stage{}}
		rows, e := tx.Query(ctx, `SELECT o.currency,count(*) FILTER(WHERE NOT p.is_closed),count(*) FILTER(WHERE p.is_won),count(*) FILTER(WHERE p.is_closed AND NOT p.is_won),COALESCE(sum(o.expected_value) FILTER(WHERE NOT p.is_closed),0)::numeric(30,2)::text,COALESCE(sum(o.expected_value*o.probability/100) FILTER(WHERE NOT p.is_closed),0)::numeric(30,2)::text,COALESCE(sum(o.expected_value) FILTER(WHERE p.is_won),0)::numeric(30,2)::text FROM opportunities o JOIN pipeline_stages p ON p.organization_id=o.organization_id AND p.id=o.stage_id WHERE o.organization_id=$1 GROUP BY o.currency ORDER BY o.currency`, a.Organization.ID)
		if e != nil {
			return v, e
		}
		for rows.Next() {
			var c Currency
			if e = rows.Scan(&c.Currency, &c.OpenCount, &c.WonCount, &c.LostCount, &c.OpenValue, &c.WeightedValue, &c.WonValue); e != nil {
				rows.Close()
				return v, e
			}
			v.Sales.Currencies = append(v.Sales.Currencies, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return v, e
		}
		rows, e = tx.Query(ctx, `SELECT p.id,p.name,count(o.id) FROM pipeline_stages p LEFT JOIN opportunities o ON o.organization_id=p.organization_id AND o.stage_id=p.id WHERE p.organization_id=$1 AND NOT p.is_closed GROUP BY p.id ORDER BY p.position,p.id`, a.Organization.ID)
		if e != nil {
			return v, e
		}
		for rows.Next() {
			var st Stage
			if e = rows.Scan(&st.ID, &st.Name, &st.Count); e != nil {
				rows.Close()
				return v, e
			}
			v.Sales.Stages = append(v.Sales.Stages, st)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return v, e
		}
	}
	if a.Allows("project.read") {
		v.Tasks = &Tasks{Items: []Task{}}
		const taskWhere = ` FROM tasks t JOIN projects p ON p.organization_id=t.organization_id AND p.id=t.project_id WHERE t.organization_id=$1 AND t.assignee_id=$2 AND t.status<>'done' AND p.status<>'completed' AND t.due_date<=$3::date`
		e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE t.due_date=$3::date),count(*) FILTER(WHERE t.due_date<$3::date)`+taskWhere, a.Organization.ID, a.User.ID, v.Today).Scan(&v.Tasks.DueToday, &v.Tasks.Overdue)
		if e != nil {
			return v, e
		}
		rows, e := tx.Query(ctx, `SELECT t.id,t.project_id,p.name,t.title,t.due_date::text,t.priority`+taskWhere+` ORDER BY t.due_date,CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END,t.id LIMIT 8`, a.Organization.ID, a.User.ID, v.Today)
		if e != nil {
			return v, e
		}
		for rows.Next() {
			var t Task
			if e = rows.Scan(&t.ID, &t.ProjectID, &t.ProjectName, &t.Title, &t.DueDate, &t.Priority); e != nil {
				rows.Close()
				return v, e
			}
			v.Tasks.Items = append(v.Tasks.Items, t)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return v, e
		}
	}
	// Join only visible business records; never expose raw audit payloads or admin events.
	sources := activitySources(a)
	if len(sources) > 0 {
		args := []any{a.Organization.ID}
		if a.Allows("document.read") {
			args = append(args, document.Workspaces(a))
		}
		rows, e := tx.Query(ctx, `WITH records AS (`+strings.Join(sources, " UNION ALL ")+`) SELECT l.id,l.action,r.title,r.href,u.name,l.created_at FROM audit_logs l JOIN records r ON r.id=l.resource_id AND r.kind=split_part(l.action,'.',1) JOIN users u ON u.organization_id=l.organization_id AND u.id=l.actor_id WHERE l.organization_id=$1 AND l.action<>'document.downloaded' ORDER BY l.created_at DESC,l.id DESC LIMIT 12`, args...)
		if e != nil {
			return v, e
		}
		for rows.Next() {
			var item Activity
			if e = rows.Scan(&item.ID, &item.Action, &item.Title, &item.Href, &item.ActorName, &item.CreatedAt); e != nil {
				rows.Close()
				return v, e
			}
			v.Activity = append(v.Activity, item)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return v, e
		}
	}
	return v, tx.Commit(ctx)
}
func activitySources(a user.Context) []string {
	out := []string{}
	for _, r := range []struct{ kind, table, title, path, permission string }{
		{"customer", "customers", "name", "customers", "customer.read"},
		{"opportunity", "opportunities", "title", "opportunities", "opportunity.read"},
		{"quote", "quotes", "number", "quotes", "quote.read"},
		{"project", "projects", "name", "projects", "project.read"},
		{"invoice", "invoices", "number", "finance", "invoice.read"},
		{"scope", "service_scopes", "title", "scopes", "scope.read"},
	} {
		if a.Allows(r.permission) {
			out = append(out, "SELECT '"+r.kind+"' AS kind,id,"+r.title+" AS title,'/"+r.path+"/'||id::text AS href FROM "+r.table+" WHERE organization_id=$1")
		}
	}
	if a.Allows("document.read") {
		out = append(out, `SELECT 'document' AS kind,id,title,'/documents/'||id::text AS href FROM documents WHERE organization_id=$1 AND workspace=ANY($2::text[])`)
	}
	return out
}
