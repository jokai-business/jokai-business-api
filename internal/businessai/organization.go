package businessai

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func validQuestion(kind, id string) bool {
	return valid(kind, id) || (kind == "organization" && customer.UUID(id))
}

// Organization questions use fixed, permission-filtered projections, never model-generated SQL.
func organizationSource(ctx context.Context, tx pgx.Tx, a user.Context, id string, p map[string]bool) (Context, error) {
	v := Context{Kind: "organization", Sections: []Section{}, Omitted: []string{"Emails, contacts, document contents, private notes, tasks and prior questions are not included", "Recent record lists are samples; do not treat them as complete organization history"}}
	if id != a.Organization.ID {
		return v, ErrNotFound
	}
	if !p["customer.read"] && !p["opportunity.read"] && !p["project.read"] && !(p["invoice.read"] && p["payment.read"]) {
		return v, user.ErrForbidden
	}
	add := func(name, query string) error {
		rows, e := tx.Query(ctx, query, a.Organization.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		s := Section{Name: name, Limit: 10, Records: []json.RawMessage{}}
		for rows.Next() {
			var b []byte
			var total int
			if e = rows.Scan(&b, &total); e != nil {
				return e
			}
			s.Total = total
			s.Records = append(s.Records, json.RawMessage(b))
		}
		if e = rows.Err(); e != nil {
			return e
		}
		v.Sections = append(v.Sections, s)
		return nil
	}
	specs := []struct {
		allowed     bool
		name, query string
	}{
		{p["customer.read"], "Latest customers", `SELECT jsonb_build_object('id',id,'name',name,'status',status,'industry',industry,'version',version),count(*) OVER() FROM customers WHERE organization_id=$1 ORDER BY created_at DESC,id LIMIT 10`},
		{p["opportunity.read"], "Pipeline totals by currency (all deals)", `SELECT jsonb_build_object('currency',o.currency,'openCount',count(*) FILTER(WHERE NOT s.is_closed),'openValue',coalesce(sum(o.expected_value) FILTER(WHERE NOT s.is_closed),0)::text,'wonCount',count(*) FILTER(WHERE s.is_won),'wonValue',coalesce(sum(o.expected_value) FILTER(WHERE s.is_won),0)::text,'lostCount',count(*) FILTER(WHERE s.is_closed AND NOT s.is_won),'note','Won deal value is not recognized revenue; no currency conversion'),count(*) OVER() FROM opportunities o JOIN pipeline_stages s ON s.organization_id=o.organization_id AND s.id=o.stage_id WHERE o.organization_id=$1 GROUP BY o.currency ORDER BY o.currency LIMIT 10`},
		{p["opportunity.read"], "Latest open opportunities", `SELECT jsonb_build_object('id',o.id,'title',o.title,'stage',s.name,'expectedValue',o.expected_value::text,'currency',o.currency,'probability',o.probability,'expectedCloseDate',o.expected_close_date,'version',o.version),count(*) OVER() FROM opportunities o JOIN pipeline_stages s ON s.organization_id=o.organization_id AND s.id=o.stage_id WHERE o.organization_id=$1 AND NOT s.is_closed ORDER BY o.created_at DESC,o.id LIMIT 10`},
		{p["project.read"], "Latest active projects", `SELECT jsonb_build_object('id',id,'name',name,'status',status,'targetDate',target_date,'version',version),count(*) OVER() FROM projects WHERE organization_id=$1 AND status<>'completed' ORDER BY created_at DESC,id LIMIT 10`},
		{p["invoice.read"] && p["payment.read"], "Invoice totals by currency (excluding drafts)", `SELECT jsonb_build_object('currency',currency,'invoiceCount',count(*),'invoiced',sum(total)::text,'collected',sum(paid)::text,'outstanding',sum(total-paid)::text,'openCount',count(*) FILTER(WHERE paid<total)),count(*) OVER() FROM invoices WHERE organization_id=$1 AND state<>'draft' GROUP BY currency ORDER BY currency LIMIT 10`},
		{p["invoice.read"] && p["payment.read"], "Outstanding invoices by earliest due date", `SELECT jsonb_build_object('id',id,'number',number,'currency',currency,'outstanding',(total-paid)::text,'dueDate',due_date,'version',version),count(*) OVER() FROM invoices WHERE organization_id=$1 AND state<>'draft' AND paid<total ORDER BY due_date,id LIMIT 10`},
	}
	for _, spec := range specs {
		if !spec.allowed {
			v.Omitted = append(v.Omitted, spec.name+" (no access)")
			continue
		}
		if e := add(spec.name, spec.query); e != nil {
			return v, e
		}
	}
	return v, nil
}
