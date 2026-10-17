package businessai

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("summary source not found")
var ErrChanged = errors.New("summary source or job changed")
var ErrDisabled = errors.New("AI unavailable")
var ErrLimit = errors.New("summary request limit")
var ErrContext = errors.New("summary context too large")

type Section struct {
	Name    string            `json:"name"`
	Total   int               `json:"total"`
	Limit   int               `json:"limit"`
	Records []json.RawMessage `json:"records"`
}
type Context struct {
	Epoch    string    `json:"-"`
	Kind     string    `json:"kind"`
	Sections []Section `json:"sections"`
	Omitted  []string  `json:"omitted"`
}

func fingerprint(v Context) (string, string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", "", e
	}
	if len(b) > 36000 {
		return "", "", ErrContext
	}
	h := sha256.Sum256(append(b, []byte(v.Epoch)...))
	return string(b), hex.EncodeToString(h[:]), nil
}
func valid(kind, id string) bool {
	return (kind == "customer" || kind == "opportunity") && customer.UUID(id)
}

// Resolve current membership/permissions inside every source transaction, including workers.
func permissions(ctx context.Context, tx pgx.Tx, a user.Context) (map[string]bool, error) {
	var keys []string
	e := tx.QueryRow(ctx, `SELECT ARRAY(SELECT DISTINCT rp.permission_key FROM user_roles ur JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE ur.organization_id=u.organization_id AND ur.user_id=u.id ORDER BY rp.permission_key) FROM users u WHERE u.organization_id=$1 AND u.id=$2 AND u.active`, a.Organization.ID, a.User.ID).Scan(&keys)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, user.ErrForbidden
	}
	if e != nil {
		return nil, e
	}
	out := map[string]bool{}
	for _, key := range keys {
		out[key] = true
	}
	return out, nil
}
func source(ctx context.Context, tx pgx.Tx, a user.Context, kind, id string) (Context, error) {
	v := Context{Kind: kind, Sections: []Section{}, Omitted: []string{}}
	if !validQuestion(kind, id) {
		return v, user.ErrInvalid
	}
	p, e := permissions(ctx, tx, a)
	if e != nil {
		return v, e
	}
	if kind == "organization" {
		return organizationSource(ctx, tx, a, id, p)
	}
	if !p[kind+".read"] {
		return v, user.ErrForbidden
	}
	org := a.Organization.ID
	customerID := id
	// Each row is a small server-selected projection. Totals explicitly describe partial lists.
	add := func(name, sql string, args ...any) error {
		rows, e := tx.Query(ctx, sql, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		s := Section{Name: name, Limit: 5, Records: []json.RawMessage{}}
		for rows.Next() {
			var b []byte
			var n int
			if e = rows.Scan(&b, &n); e != nil {
				return e
			}
			s.Total = n
			s.Records = append(s.Records, json.RawMessage(b))
		}
		if e = rows.Err(); e != nil {
			return e
		}
		v.Sections = append(v.Sections, s)
		return nil
	}
	if kind == "opportunity" {
		e = tx.QueryRow(ctx, `SELECT customer_id FROM opportunities WHERE organization_id=$1 AND id=$2`, org, id).Scan(&customerID)
		if errors.Is(e, pgx.ErrNoRows) {
			return v, ErrNotFound
		}
		if e != nil {
			return v, e
		}
		e = add("Opportunity", `SELECT jsonb_build_object('id',o.id,'title',o.title,'customerName',c.name,'stage',s.name,'closed',s.is_closed,'won',s.is_won,'expectedValue',o.expected_value::text,'currency',o.currency,'probability',o.probability,'expectedCloseDate',o.expected_close_date,'requirement',left(o.requirement,2000),'requirementTruncated',length(o.requirement)>2000,'nextAction',left(o.next_action,1000),'nextActionTruncated',length(o.next_action)>1000,'owner',u.name,'version',o.version),1 FROM opportunities o JOIN customers c ON c.organization_id=o.organization_id AND c.id=o.customer_id JOIN pipeline_stages s ON s.organization_id=o.organization_id AND s.id=o.stage_id JOIN users u ON u.organization_id=o.organization_id AND u.id=o.owner_id WHERE o.organization_id=$1 AND o.id=$2`, org, id)
		if e != nil {
			return v, e
		}
		e = add("Latest opportunity activity", `SELECT jsonb_build_object('id',id,'kind',kind,'body',left(body,500),'truncated',length(body)>500,'createdAt',created_at),count(*) OVER() FROM opportunity_activities WHERE organization_id=$1 AND opportunity_id=$2 ORDER BY created_at DESC,id LIMIT 5`, org, id)
		if e != nil {
			return v, e
		}
	}
	if p["customer.read"] {
		e = add("Customer", `SELECT jsonb_build_object('id',c.id,'name',c.name,'email',c.email,'phone',c.phone,'industry',c.industry,'country',c.country,'status',c.status,'notes',left(c.notes,2000),'notesTruncated',length(c.notes)>2000,'owner',u.name,'version',c.version),1 FROM customers c JOIN users u ON u.organization_id=c.organization_id AND u.id=c.owner_id WHERE c.organization_id=$1 AND c.id=$2`, org, customerID)
		if e != nil {
			return v, e
		}
		if len(v.Sections[len(v.Sections)-1].Records) == 0 {
			return v, ErrNotFound
		}
		e = add("Latest contacts", `SELECT jsonb_build_object('id',id,'name',name,'title',title,'email',email,'phone',phone),count(*) OVER() FROM contacts WHERE organization_id=$1 AND customer_id=$2 ORDER BY created_at DESC,id LIMIT 5`, org, customerID)
		if e != nil {
			return v, e
		}
	} else {
		v.Omitted = append(v.Omitted, "Customer details and contacts (no access)")
	}
	if kind == "customer" && p["opportunity.read"] {
		e = add("Latest opportunities", `SELECT jsonb_build_object('id',o.id,'title',o.title,'stage',s.name,'closed',s.is_closed,'won',s.is_won,'expectedValue',o.expected_value::text,'currency',o.currency,'probability',o.probability,'expectedCloseDate',o.expected_close_date,'nextAction',left(o.next_action,500),'truncated',length(o.next_action)>500,'version',o.version),count(*) OVER() FROM opportunities o JOIN pipeline_stages s ON s.organization_id=o.organization_id AND s.id=o.stage_id WHERE o.organization_id=$1 AND o.customer_id=$2 ORDER BY o.created_at DESC,o.id LIMIT 5`, org, id)
		if e != nil {
			return v, e
		}
	} else if kind == "customer" {
		v.Omitted = append(v.Omitted, "Opportunities (no access)")
	}
	column := "customer_id"
	if kind == "opportunity" {
		column = "opportunity_id"
	}
	if p["quote.read"] {
		e = add("Latest quotes", `SELECT jsonb_build_object('id',id,'number',number,'status',status,'currency',currency,'total',total::text,'expiryDate',expiry_date,'version',version),count(*) OVER() FROM quotes WHERE organization_id=$1 AND `+column+`=$2 ORDER BY created_at DESC,id LIMIT 5`, org, id)
		if e != nil {
			return v, e
		}
	} else {
		v.Omitted = append(v.Omitted, "Quotes (no access)")
	}
	if p["project.read"] {
		e = add("Active projects", `SELECT jsonb_build_object('id',id,'name',name,'status',status,'targetDate',target_date,'notes',left(notes,500),'truncated',length(notes)>500,'version',version),count(*) OVER() FROM projects WHERE organization_id=$1 AND `+column+`=$2 AND status<>'completed' ORDER BY created_at DESC,id LIMIT 5`, org, id)
		if e != nil {
			return v, e
		}
	} else {
		v.Omitted = append(v.Omitted, "Delivery projects (no access)")
	}
	if p["invoice.read"] {
		condition := "i.customer_id=$2"
		if kind == "opportunity" {
			condition = "EXISTS(SELECT 1 FROM quotes q WHERE q.organization_id=i.organization_id AND q.id=i.quote_id AND q.opportunity_id=$2)"
		}
		e = add("Outstanding invoices", `SELECT jsonb_build_object('id',i.id,'number',i.number,'state',i.state,'currency',i.currency,'total',i.total::text,'paid',i.paid::text,'outstanding',(i.total-i.paid)::text,'dueDate',i.due_date,'version',i.version),count(*) OVER() FROM invoices i WHERE i.organization_id=$1 AND `+condition+` AND i.state<>'draft' AND i.paid<i.total ORDER BY i.due_date,i.id LIMIT 5`, org, id)
		if e != nil {
			return v, e
		}
	} else {
		v.Omitted = append(v.Omitted, "Invoices (no access)")
	}
	// Email context is only for customer summaries; exact unique sender matching mirrors Gmail.
	if kind == "customer" {
		e = tx.QueryRow(ctx, `SELECT sync_generation::text FROM gmail_connections WHERE organization_id=$1 AND user_id=$2`, org, a.User.ID).Scan(&v.Epoch)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return v, e
		}
		e = add("Your latest matched emails", `SELECT jsonb_build_object('id',m.id,'subject',m.subject,'sender',m.sender,'body',left(m.body_text,1000),'incomplete',m.text_limited OR length(m.body_text)>1000,'receivedAt',m.received_at),count(*) OVER() FROM gmail_messages m JOIN gmail_connections g USING(organization_id,user_id) WHERE m.organization_id=$1 AND m.user_id=$2 AND m.received_at>=now()-interval '30 days' AND m.sender_email<>'' AND (SELECT array_agg(c.id) FROM customers c WHERE c.organization_id=m.organization_id AND (lower(btrim(c.email))=m.sender_email OR EXISTS(SELECT 1 FROM contacts ct WHERE ct.organization_id=c.organization_id AND ct.customer_id=c.id AND lower(btrim(ct.email))=m.sender_email)))=ARRAY[$3::uuid] ORDER BY m.received_at DESC,m.id LIMIT 5`, org, a.User.ID, id)
		if e != nil {
			return v, e
		}
	} else {
		v.Omitted = append(v.Omitted, "Email (not linked to a specific opportunity)")
	}
	return v, nil
}
