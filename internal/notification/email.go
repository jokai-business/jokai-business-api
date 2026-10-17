package notification

import (
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"time"
)

type EmailSender interface {
	Send(context.Context, string, string) error
	SendTest(context.Context, string, string) error
}
type Preferences struct {
	EmailEnabled   bool   `json:"emailEnabled"`
	EmailAvailable bool   `json:"emailAvailable"`
	Email          string `json:"email"`
}

func (s *Service) Preferences(ctx context.Context, a user.Context) (Preferences, error) {
	p := Preferences{EmailAvailable: s.Mailer != nil}
	e := s.DB.QueryRow(ctx, `SELECT COALESCE(p.email_enabled,false),u.email FROM users u LEFT JOIN notification_preferences p ON p.organization_id=u.organization_id AND p.user_id=u.id WHERE u.organization_id=$1 AND u.id=$2 AND u.active`, a.Organization.ID, a.User.ID).Scan(&p.EmailEnabled, &p.Email)
	return p, e
}
func (s *Service) SetPreferences(ctx context.Context, a user.Context, enabled bool, rid string) (Preferences, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Preferences{}, e
	}
	defer tx.Rollback(ctx)
	// Same user lock as delivery: opt-out waits for any already-running send.
	var email string
	e = tx.QueryRow(ctx, `SELECT email FROM users WHERE organization_id=$1 AND id=$2 AND active FOR UPDATE`, a.Organization.ID, a.User.ID).Scan(&email)
	if e != nil {
		return Preferences{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO notification_preferences(organization_id,user_id,email_enabled) VALUES($1,$2,$3) ON CONFLICT(organization_id,user_id) DO UPDATE SET email_enabled=$3,enabled_at=CASE WHEN $3 AND NOT notification_preferences.email_enabled THEN now() ELSE notification_preferences.enabled_at END`, a.Organization.ID, a.User.ID, enabled)
	if e != nil {
		return Preferences{}, e
	}
	if !enabled {
		_, e = tx.Exec(ctx, `UPDATE notification_email_deliveries SET status='cancelled' WHERE organization_id=$1 AND user_id=$2 AND status='pending'`, a.Organization.ID, a.User.ID)
		if e != nil {
			return Preferences{}, e
		}
	}
	action := "notification.email_disabled"
	if enabled {
		action = "notification.email_enabled"
	}
	if e = audit(ctx, tx, a, action, a.User.ID, rid); e != nil {
		return Preferences{}, e
	}
	return Preferences{EmailEnabled: enabled, EmailAvailable: s.Mailer != nil, Email: email}, tx.Commit(ctx)
}

// Enqueue only new alerts since opt-in; existing read/resolved alerts are excluded.
func (s *Service) enqueueEmails(ctx context.Context) error {
	_, e := s.DB.Exec(ctx, `INSERT INTO notification_email_deliveries(organization_id,notification_id,user_id)
 SELECT n.organization_id,n.id,n.user_id FROM notifications n JOIN notification_preferences p ON p.organization_id=n.organization_id AND p.user_id=n.user_id
 JOIN users u ON u.organization_id=n.organization_id AND u.id=n.user_id
 WHERE p.email_enabled AND u.active AND n.created_at>=p.enabled_at AND n.read_at IS NULL AND n.resolved_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM notification_email_deliveries d WHERE d.organization_id=n.organization_id AND d.notification_id=n.id)
 ORDER BY n.created_at,n.id LIMIT 500 ON CONFLICT DO NOTHING`)
	return e
}
func (s *Service) deliverEmail(ctx context.Context) (bool, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	// Lock the user first, consistently with preference writes. Skip users another
	// replica is processing; the transaction spans a bounded SMTP attempt.
	var org, uid string
	e = tx.QueryRow(ctx, `SELECT u.organization_id,u.id FROM users u WHERE EXISTS(SELECT 1 FROM notification_email_deliveries d WHERE d.organization_id=u.organization_id AND d.user_id=u.id AND d.status='pending' AND d.next_attempt_at<=now()) ORDER BY u.organization_id,u.id LIMIT 1 FOR UPDATE OF u SKIP LOCKED`).Scan(&org, &uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var id, email string
	var attempts int
	var eligible bool
	e = tx.QueryRow(ctx, `SELECT d.notification_id,u.email,d.attempts,
 `+emailEligible+`
 FROM notification_email_deliveries d JOIN notifications n ON n.organization_id=d.organization_id AND n.id=d.notification_id
 JOIN users u ON u.organization_id=d.organization_id AND u.id=d.user_id
 LEFT JOIN notification_preferences p ON p.organization_id=u.organization_id AND p.user_id=u.id
 WHERE d.organization_id=$1 AND d.user_id=$2 AND d.status='pending' AND d.next_attempt_at<=now() ORDER BY d.next_attempt_at,d.notification_id LIMIT 1 FOR UPDATE OF d,n`, org, uid).Scan(&id, &email, &attempts, &eligible)
	if e != nil {
		return false, e
	}
	status := "cancelled"
	var sent *time.Time
	if eligible {
		attempts++
		job, cancel := context.WithTimeout(ctx, 15*time.Second)
		sendErr := s.Mailer.Send(job, email, id)
		cancel()
		if sendErr == nil {
			status = "sent"
			now := time.Now()
			sent = &now
		} else if attempts >= 5 {
			status = "failed"
		} else {
			status = "pending"
		}
	}
	// Never persist SMTP error text: providers may include recipient or credentials.
	_, e = tx.Exec(ctx, `UPDATE notification_email_deliveries SET status=$3,attempts=$4,next_attempt_at=now()+make_interval(secs => $5),sent_at=$6 WHERE organization_id=$1 AND notification_id=$2`, org, id, status, attempts, retryDelay(attempts).Seconds(), sent)
	if e != nil {
		return false, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id) VALUES($1,$2,$3,$4,'notification-email-worker')`, org, uid, "notification.email_"+status, id)
	if e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Minute * time.Duration(1<<uint(attempt-1))
}
func (s *Service) sweepEmails(ctx context.Context) error {
	if s.Mailer == nil {
		return nil
	}
	for i := 0; i < 10; i++ {
		ok, e := s.deliverTestEmail(ctx)
		if e != nil {
			return e
		}
		if !ok {
			break
		}
	}
	if e := s.enqueueEmails(ctx); e != nil {
		return e
	}
	for i := 0; i < 50; i++ {
		ok, e := s.deliverEmail(ctx)
		if e != nil {
			return e
		}
		if !ok {
			return nil
		}
	}
	return nil
}

// RunEmail is independent so a slow SMTP provider cannot delay in-app reminders.
func (s *Service) RunEmail(ctx context.Context, logger *slog.Logger) {
	if s.Mailer == nil {
		return
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if e := s.sweepEmails(ctx); e != nil && ctx.Err() == nil {
			logger.Error("notification email sweep failed; will retry")
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
