package notification

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

var ErrEmailDisabled = errors.New("email delivery not configured")
var ErrEmailAction = errors.New("email action unavailable")
var ErrEmailRate = errors.New("email test rate limit reached")

type EmailStatus struct {
	Available     bool   `json:"available"`
	Sender        string `json:"sender"`
	TestRecipient string `json:"testRecipient"`
	Transport     string `json:"transport"`
}
type EmailDelivery struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	Recipient     string     `json:"recipient"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	ManualRetries int        `json:"manualRetries"`
	CreatedAt     time.Time  `json:"createdAt"`
	NextAttemptAt *time.Time `json:"nextAttemptAt"`
	SentAt        *time.Time `json:"sentAt"`
	CanRetry      bool       `json:"canRetry"`
}

// Shared eligibility for automatic sends, admin retry and retry affordances.
const emailEligible = `u.active AND COALESCE(p.email_enabled,false) AND n.created_at>=p.enabled_at AND n.read_at IS NULL AND n.resolved_at IS NULL
 AND (n.required_permission='' OR EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE ur.organization_id=u.organization_id AND ur.user_id=u.id AND rp.permission_key=n.required_permission))
 AND (n.kind='reminder' OR EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE ur.organization_id=u.organization_id AND ur.user_id=u.id AND rp.permission_key=CASE WHEN n.kind LIKE 'task_%' THEN 'project.read' WHEN n.kind LIKE 'invoice_%' THEN 'invoice.read' WHEN n.kind='quote_review' THEN 'quote.read' WHEN n.kind='scope_review' THEN 'scope.read' ELSE '' END))`

func (s *Service) EmailStatus(ctx context.Context, a user.Context) (EmailStatus, error) {
	if !a.Allows("settings.manage") {
		return EmailStatus{}, user.ErrForbidden
	}
	v := EmailStatus{Available: s.Mailer != nil, Transport: "STARTTLS"}
	if v.Available {
		v.Sender = s.EmailFrom
	}
	e := s.DB.QueryRow(ctx, `SELECT email FROM users WHERE organization_id=$1 AND id=$2 AND active`, a.Organization.ID, a.User.ID).Scan(&v.TestRecipient)
	return v, e
}

const history = `WITH history AS (
 SELECT d.notification_id AS id,'notification'::text AS kind,u.email AS recipient,d.status,d.attempts,d.manual_retries,d.created_at,
 CASE WHEN d.status='pending' THEN d.next_attempt_at END AS next_attempt_at,d.sent_at,
 COALESCE(d.status='failed' AND d.attempts=5 AND d.manual_retries=0 AND (` + emailEligible + `),false) AS can_retry
 FROM notification_email_deliveries d JOIN users u ON u.organization_id=d.organization_id AND u.id=d.user_id
 JOIN notifications n ON n.organization_id=d.organization_id AND n.id=d.notification_id
 LEFT JOIN notification_preferences p ON p.organization_id=d.organization_id AND p.user_id=d.user_id WHERE d.organization_id=$1
 UNION ALL SELECT id,'test',recipient,status,attempts,0,created_at,NULL::timestamptz,sent_at,false FROM notification_email_tests WHERE organization_id=$1
) `

func (s *Service) EmailHistory(ctx context.Context, a user.Context, status string, page, size int) ([]EmailDelivery, int, error) {
	if !a.Allows("settings.manage") {
		return nil, 0, user.ErrForbidden
	}
	if !pageOK(page, size) || (status != "all" && status != "pending" && status != "sent" && status != "failed" && status != "cancelled") {
		return nil, 0, user.ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	var total int
	e = tx.QueryRow(ctx, history+`SELECT count(*) FROM history WHERE $2='all' OR status=$2`, a.Organization.ID, status).Scan(&total)
	if e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, history+`SELECT id,kind,recipient,status,attempts,manual_retries,created_at,next_attempt_at,sent_at,can_retry FROM history WHERE $2='all' OR status=$2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, a.Organization.ID, status, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []EmailDelivery{}
	for rows.Next() {
		var v EmailDelivery
		if e = rows.Scan(&v.ID, &v.Kind, &v.Recipient, &v.Status, &v.Attempts, &v.ManualRetries, &v.CreatedAt, &v.NextAttemptAt, &v.SentAt, &v.CanRetry); e != nil {
			rows.Close()
			return nil, 0, e
		}
		v.CanRetry = v.CanRetry && s.Mailer != nil
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	return out, total, tx.Commit(ctx)
}
func (s *Service) RetryEmail(ctx context.Context, a user.Context, id, rid string) error {
	if !a.Allows("settings.manage") {
		return user.ErrForbidden
	}
	if !customer.UUID(id) {
		return user.ErrInvalid
	}
	if s.Mailer == nil {
		return ErrEmailDisabled
	}
	// Reconcile completed/reassigned business sources before evaluating retry.
	if e := s.refresh(ctx, a.Organization.ID, true); e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var uid string
	e = tx.QueryRow(ctx, `SELECT user_id FROM notification_email_deliveries WHERE organization_id=$1 AND notification_id=$2`, a.Organization.ID, id).Scan(&uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	// Same lock order as worker and preference writes: user, then delivery/notice.
	if _, e = tx.Exec(ctx, `SELECT id FROM users WHERE organization_id=$1 AND id=$2 FOR UPDATE`, a.Organization.ID, uid); e != nil {
		return e
	}
	var eligible bool
	var status string
	var attempts, retries int
	e = tx.QueryRow(ctx, `SELECT d.status,d.attempts,d.manual_retries,COALESCE((`+emailEligible+`),false) FROM notification_email_deliveries d
 JOIN notifications n ON n.organization_id=d.organization_id AND n.id=d.notification_id
 JOIN users u ON u.organization_id=d.organization_id AND u.id=d.user_id
 LEFT JOIN notification_preferences p ON p.organization_id=d.organization_id AND p.user_id=d.user_id
 WHERE d.organization_id=$1 AND d.notification_id=$2 FOR UPDATE OF d,n`, a.Organization.ID, id).Scan(&status, &attempts, &retries, &eligible)
	if e != nil {
		return e
	}
	if status != "failed" || attempts != 5 || retries != 0 || !eligible {
		return ErrEmailAction
	}
	_, e = tx.Exec(ctx, `UPDATE notification_email_deliveries SET status='pending',manual_retries=1,next_attempt_at=now() WHERE organization_id=$1 AND notification_id=$2`, a.Organization.ID, id)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, a, "notification.email_retry_requested", id, rid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

type TestEmailInput struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Confirmed      bool   `json:"confirmed"`
}

func (s *Service) QueueTestEmail(ctx context.Context, a user.Context, in TestEmailInput, rid string) (string, error) {
	if !a.Allows("settings.manage") {
		return "", user.ErrForbidden
	}
	if !in.Confirmed || !customer.UUID(in.IdempotencyKey) {
		return "", user.ErrInvalid
	}
	if s.Mailer == nil {
		return "", ErrEmailDisabled
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	// Serialize rate checks across all admins/replicas in this organization.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('email-test:'||$1,0))`, a.Organization.ID); e != nil {
		return "", e
	}
	var id, owner string
	e = tx.QueryRow(ctx, `SELECT id,user_id FROM notification_email_tests WHERE organization_id=$1 AND idempotency_key=$2`, a.Organization.ID, in.IdempotencyKey).Scan(&id, &owner)
	if e == nil {
		if owner != a.User.ID {
			return "", ErrEmailAction
		}
		return id, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return "", e
	}
	var limited bool
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE created_at>now()-interval '1 minute')>0 OR count(*)>=10 FROM notification_email_tests WHERE organization_id=$1 AND created_at>now()-interval '1 hour'`, a.Organization.ID).Scan(&limited)
	if e != nil {
		return "", e
	}
	if limited {
		return "", ErrEmailRate
	}
	var email string
	e = tx.QueryRow(ctx, `SELECT email FROM users WHERE organization_id=$1 AND id=$2 AND active FOR UPDATE`, a.Organization.ID, a.User.ID).Scan(&email)
	if e != nil {
		return "", e
	}
	e = tx.QueryRow(ctx, `INSERT INTO notification_email_tests(organization_id,user_id,recipient,idempotency_key) VALUES($1,$2,$3,$4) RETURNING id`, a.Organization.ID, a.User.ID, email, in.IdempotencyKey).Scan(&id)
	if e != nil {
		return "", e
	}
	if e = audit(ctx, tx, a, "notification.email_test_requested", id, rid); e != nil {
		return "", e
	}
	return id, tx.Commit(ctx)
}
func (s *Service) deliverTestEmail(ctx context.Context) (bool, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	var org, uid string
	e = tx.QueryRow(ctx, `SELECT u.organization_id,u.id FROM users u WHERE EXISTS(SELECT 1 FROM notification_email_tests t WHERE t.organization_id=u.organization_id AND t.user_id=u.id AND t.status='pending') ORDER BY u.organization_id,u.id LIMIT 1 FOR UPDATE OF u SKIP LOCKED`).Scan(&org, &uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var id, email string
	var eligible bool
	e = tx.QueryRow(ctx, `SELECT t.id,t.recipient,u.active AND u.email=t.recipient AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key=ur.role_key WHERE ur.organization_id=u.organization_id AND ur.user_id=u.id AND rp.permission_key='settings.manage') FROM notification_email_tests t JOIN users u ON u.organization_id=t.organization_id AND u.id=t.user_id WHERE t.organization_id=$1 AND t.user_id=$2 AND t.status='pending' ORDER BY t.created_at,t.id LIMIT 1 FOR UPDATE OF t`, org, uid).Scan(&id, &email, &eligible)
	if e != nil {
		return false, e
	}
	status := "cancelled"
	attempts := 0
	var sent *time.Time
	if eligible {
		attempts = 1
		job, cancel := context.WithTimeout(ctx, 15*time.Second)
		sendErr := s.Mailer.SendTest(job, email, id)
		cancel()
		if sendErr == nil {
			status = "sent"
			now := time.Now()
			sent = &now
		} else {
			status = "failed"
		}
	}
	_, e = tx.Exec(ctx, `UPDATE notification_email_tests SET status=$3,attempts=$4,sent_at=$5 WHERE organization_id=$1 AND id=$2`, org, id, status, attempts, sent)
	if e != nil {
		return false, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id) VALUES($1,$2,$3,$4,'notification-email-worker')`, org, uid, "notification.email_test_"+status, id)
	if e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}
