package notification

import (
	"context"
	"log/slog"
	"time"
)

// Refresh reconciles only one organization's current actionable alerts. Unique keys
// and a transaction lock make multiple API replicas and manual refresh safe.
func (s *Service) Refresh(ctx context.Context, org string) error { return s.refresh(ctx, org, false) }
func (s *Service) refresh(ctx context.Context, org string, wait bool) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var locked bool
	if wait {
		_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('notifications:'||$1,0))`, org)
		if e != nil {
			return e
		}
		locked = true
	} else if e = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('notifications:'||$1,0))`, org).Scan(&locked); e != nil {
		return e
	}

	if !locked {
		return nil
	}
	_, e = tx.Exec(ctx, `CREATE TEMP TABLE notification_candidates ON COMMIT DROP AS
 WITH perms AS (SELECT DISTINCT u.id,rp.permission_key FROM users u JOIN user_roles ur ON ur.organization_id=u.organization_id AND ur.user_id=u.id JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE u.organization_id=$1 AND u.active),
 today AS (SELECT (now() AT TIME ZONE 'UTC')::date AS day)
 SELECT t.assignee_id AS user_id,CASE WHEN t.due_date=d.day THEN 'task_due' ELSE 'task_overdue' END AS kind,t.id AS source_id,
 'task:'||t.id||':'||t.due_date||':'||(t.due_date<d.day)::text AS dedup_key,
 CASE WHEN t.due_date=d.day THEN 'Task due today: ' ELSE 'Task overdue: ' END||t.title AS title,
 p.name||' · Due '||t.due_date AS body,'/projects/'||p.id AS href,'project.read'::text AS required_permission
 FROM tasks t JOIN projects p ON p.organization_id=t.organization_id AND p.id=t.project_id CROSS JOIN today d
 WHERE t.organization_id=$1 AND t.status<>'done' AND p.status<>'completed' AND t.due_date<=d.day AND EXISTS(SELECT 1 FROM perms WHERE id=t.assignee_id AND permission_key='project.read')
 UNION ALL
 SELECT u.id,CASE WHEN i.due_date=d.day THEN 'invoice_due' ELSE 'invoice_overdue' END,i.id,
 'invoice:'||i.id||':'||i.due_date||':'||(i.due_date<d.day)::text,
 CASE WHEN i.due_date=d.day THEN 'Invoice due today: ' ELSE 'Invoice overdue: ' END||i.number,
 i.customer_name||' · Due '||i.due_date,'/finance/'||i.id,'invoice.update'
 FROM invoices i CROSS JOIN today d JOIN perms u ON u.permission_key='invoice.update'
 WHERE i.organization_id=$1 AND i.state<>'draft' AND i.paid<i.total AND i.due_date<=d.day AND EXISTS(SELECT 1 FROM perms WHERE id=u.id AND permission_key='invoice.read')
 UNION ALL
 SELECT u.id,'quote_review',q.id,'quote-review:'||a.id,'Quotation awaiting review: '||q.number,q.customer_name,'/quotes/'||q.id,'quote.approve'
 FROM approval_requests a JOIN quotes q ON q.organization_id=a.organization_id AND q.id=a.quote_id JOIN perms u ON u.permission_key='quote.approve'
 WHERE a.organization_id=$1 AND a.status='pending' AND q.status='submitted' AND q.expiry_date>=(now() AT TIME ZONE 'UTC')::date AND u.id<>q.creator_id AND u.id<>a.requested_by AND EXISTS(SELECT 1 FROM perms WHERE id=u.id AND permission_key='quote.read')
 UNION ALL
 SELECT u.id,'scope_review',s.id,'scope-review:'||s.id||':'||s.revision||':'||s.version,'Service scope awaiting review: '||s.title,s.customer_name,'/scopes/'||s.id,'scope.approve'
 FROM service_scopes s JOIN perms u ON u.permission_key='scope.approve'
 WHERE s.organization_id=$1 AND s.status='in_review' AND u.id<>s.creator_id AND u.id<>s.edited_by AND u.id<>s.submitted_by
 AND NOT EXISTS(SELECT 1 FROM scope_preparers p WHERE p.organization_id=s.organization_id AND p.scope_id=s.id AND p.revision=s.revision AND p.user_id=u.id)
 AND EXISTS(SELECT 1 FROM perms WHERE id=u.id AND permission_key='scope.read')`, org)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO notifications(organization_id,user_id,kind,source_id,dedup_key,title,body,href,required_permission)
 SELECT $1,user_id,kind,source_id,dedup_key,title,body,href,required_permission FROM notification_candidates
 ON CONFLICT(organization_id,user_id,dedup_key) DO UPDATE SET title=excluded.title,body=excluded.body,href=excluded.href,resolved_at=NULL`, org)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE notifications n SET resolved_at=now() WHERE n.organization_id=$1 AND n.kind<>'reminder' AND n.resolved_at IS NULL AND NOT EXISTS(SELECT 1 FROM notification_candidates c WHERE c.user_id=n.user_id AND c.dedup_key=n.dedup_key)`, org)
	if e != nil {
		return e
	}
	// Claim due reminders in the same transaction as inbox insertion. A cancellation
	// competes on the reminder row; committed cancellation can never be delivered.
	_, e = tx.Exec(ctx, `WITH due AS (
 SELECT r.id FROM reminders r JOIN users u ON u.organization_id=r.organization_id AND u.id=r.user_id WHERE r.organization_id=$1 AND r.status='scheduled' AND r.due_at<=now() AND u.active ORDER BY r.due_at,r.id LIMIT 500 FOR UPDATE OF r SKIP LOCKED
 ), delivered AS (UPDATE reminders r SET status='delivered',delivered_at=now() FROM due WHERE r.id=due.id AND r.organization_id=$1 RETURNING r.*)
 INSERT INTO notifications(organization_id,user_id,kind,source_id,dedup_key,title,body,href) SELECT organization_id,user_id,'reminder',id,'reminder:'||id,title,notes,'/notifications' FROM delivered ON CONFLICT DO NOTHING`, org)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if e := s.sweep(ctx); e != nil && ctx.Err() == nil {
			logger.Error("notification sweep failed; will retry")
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (s *Service) sweep(ctx context.Context) error {
	// Keyset batches avoid holding a database connection while processing orgs.
	var firstErr error
	after := "00000000-0000-0000-0000-000000000000"
	for {
		rows, e := s.DB.Query(ctx, `SELECT id::text FROM organizations WHERE id>$1::uuid ORDER BY id LIMIT 100`, after)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(ids) == 0 {
			return firstErr
		}
		for _, id := range ids {
			job, cancel := context.WithTimeout(ctx, 20*time.Second)
			e = s.Refresh(job, id)
			cancel()
			if e != nil {
				firstErr = e
			}
			after = id
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
}
