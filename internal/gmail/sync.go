package gmail

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/user"
	"ai-business-api/platform/googlemail"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"time"
)

type SyncProvider interface {
	Recent(context.Context, string, string, time.Time) (googlemail.Snapshot, error)
}
type SyncStatus struct {
	Enabled      bool       `json:"enabled"`
	Status       string     `json:"status"`
	LastSyncedAt *time.Time `json:"lastSyncedAt"`
	NextSyncAt   *time.Time `json:"nextSyncAt"`
	Limited      bool       `json:"limited"`
}
type InboxMessage struct {
	googlemail.Message
	CustomerID   *string `json:"customerId"`
	CustomerName *string `json:"customerName"`
}

func (s *Service) SyncStatus(ctx context.Context, a user.Context) (SyncStatus, error) {
	v := SyncStatus{Status: "idle"}
	e := s.DB.QueryRow(ctx, `SELECT sync_enabled,sync_status,last_synced_at,CASE WHEN sync_enabled THEN next_sync_at END,sync_limited FROM gmail_connections WHERE organization_id=$1 AND user_id=$2`, a.Organization.ID, a.User.ID).Scan(&v.Enabled, &v.Status, &v.LastSyncedAt, &v.NextSyncAt, &v.Limited)
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	return v, e
}
func (s *Service) SetSync(ctx context.Context, a user.Context, enabled bool, rid string) (SyncStatus, error) {
	if enabled && !s.available() {
		return SyncStatus{}, ErrDisabled
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return SyncStatus{}, e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, a.Organization.ID, a.User.ID); e != nil {
		return SyncStatus{}, e
	}
	// Only transitions change generation; repeated enables cannot bypass the five-minute interval.
	tag, e := tx.Exec(ctx, `UPDATE gmail_connections SET sync_enabled=$3,sync_generation=CASE WHEN sync_enabled<>$3 THEN gen_random_uuid() ELSE sync_generation END,sync_status=CASE WHEN sync_enabled<>$3 THEN 'idle' ELSE sync_status END,sync_lease_until=CASE WHEN sync_enabled<>$3 THEN NULL ELSE sync_lease_until END WHERE organization_id=$1 AND user_id=$2`, a.Organization.ID, a.User.ID, enabled)
	if e != nil {
		return SyncStatus{}, e
	}
	if tag.RowsAffected() == 0 {
		return SyncStatus{}, ErrState
	}
	action := "gmail.sync_paused"
	if enabled {
		action = "gmail.sync_enabled"
	}
	if e = audit(ctx, tx, a.Organization.ID, a.User.ID, action, rid); e != nil {
		return SyncStatus{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return SyncStatus{}, e
	}
	return s.SyncStatus(ctx, a)
}

// Matching is evaluated on reads so changed addresses and permissions cannot leave stale links.
const matched = ` FROM gmail_messages m LEFT JOIN LATERAL (
 SELECT min(c.id::text) AS id,min(c.name) AS name FROM customers c
 WHERE $3 AND c.organization_id=m.organization_id AND m.sender_email<>'' AND
 (lower(btrim(c.email))=m.sender_email OR EXISTS(SELECT 1 FROM contacts ct WHERE ct.organization_id=c.organization_id AND ct.customer_id=c.id AND lower(btrim(ct.email))=m.sender_email))
 HAVING count(*)=1) customer ON true
 WHERE m.organization_id=$1 AND m.user_id=$2 AND m.received_at>=now()-interval '30 days'
 AND ($4<>'unread' OR m.unread) AND ($4<>'linked' OR customer.id IS NOT NULL)
 AND ($5='' OR customer.id=$5)`

func (s *Service) Messages(ctx context.Context, a user.Context, filter, customerID string, page, size int) ([]InboxMessage, int, error) {
	if filter != "all" && filter != "unread" && filter != "linked" {
		return nil, 0, user.ErrInvalid
	}
	if page < 1 || size < 1 || size > 100 {
		return nil, 0, user.ErrInvalid
	}
	if customerID != "" && !customer.UUID(customerID) {
		return nil, 0, user.ErrInvalid
	}
	if (filter == "linked" || customerID != "") && !a.Allows("customer.read") {
		return nil, 0, user.ErrForbidden
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	args := []any{a.Organization.ID, a.User.ID, a.Allows("customer.read"), filter, customerID}
	var total int
	if e = tx.QueryRow(ctx, `SELECT count(*)`+matched, args...).Scan(&total); e != nil {
		return nil, 0, e
	}
	args = append(args, size, (page-1)*size)
	rows, e := tx.Query(ctx, `SELECT m.id,m.subject,m.sender,m.sender_email,m.snippet,m.body_text,m.unread,m.received_at,m.text_limited,customer.id,customer.name`+matched+` ORDER BY m.received_at DESC,m.id LIMIT $6 OFFSET $7`, args...)
	if e != nil {
		return nil, 0, e
	}
	out := []InboxMessage{}
	for rows.Next() {
		var v InboxMessage
		if e = rows.Scan(&v.ID, &v.Subject, &v.Sender, &v.SenderEmail, &v.Snippet, &v.Text, &v.Unread, &v.ReceivedAt, &v.TextLimited, &v.CustomerID, &v.CustomerName); e != nil {
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
func (s *Service) RunSync(ctx context.Context, logger *slog.Logger) {
	if !s.available() {
		return
	}
	if _, ok := s.Provider.(SyncProvider); !ok {
		return
	}
	for {
		worked, e := s.processSync(ctx)
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			logger.Error("Gmail sync worker failed")
		}
		if worked && e == nil {
			continue
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

type syncJob struct {
	org, uid, email, generation string
	token                       []byte
}

func (s *Service) processSync(ctx context.Context) (bool, error) {
	provider, ok := s.Provider.(SyncProvider)
	if !ok || !s.available() {
		return false, nil
	}
	var j syncJob
	e := s.DB.QueryRow(ctx, `UPDATE gmail_connections c SET sync_status='syncing',sync_generation=gen_random_uuid(),sync_lease_until=now()+interval '3 minutes',next_sync_at=now()+interval '5 minutes'
 WHERE (organization_id,user_id) IN (SELECT g.organization_id,g.user_id FROM gmail_connections g JOIN users u ON u.organization_id=g.organization_id AND u.id=g.user_id WHERE u.active AND g.sync_enabled AND g.next_sync_at<=now() AND (g.sync_status<>'syncing' OR g.sync_lease_until<now()) ORDER BY g.next_sync_at FOR UPDATE OF g SKIP LOCKED LIMIT 1)
 RETURNING c.organization_id,c.user_id,c.email,c.refresh_token,c.sync_generation`).Scan(&j.org, &j.uid, &j.email, &j.token, &j.generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	token, e := s.Vault.Open(j.token, owner(j.org, j.uid, "token"))
	var snapshot googlemail.Snapshot
	if e == nil {
		snapshot, e = provider.Recent(jobCtx, token, j.email, time.Now().UTC().Add(-30*24*time.Hour))
	}
	if e == nil && len(snapshot.Messages) > googlemail.MaxMessages {
		e = googlemail.ErrProvider
	}
	return true, s.finishSync(ctx, j, snapshot, e)
}
func (s *Service) finishSync(ctx context.Context, j syncJob, result googlemail.Snapshot, failure error) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lock(ctx, tx, j.org, j.uid); errors.Is(e, user.ErrForbidden) {
		return nil
	} else if e != nil {
		return e
	}
	var generation string
	e = tx.QueryRow(ctx, `SELECT sync_generation FROM gmail_connections WHERE organization_id=$1 AND user_id=$2 AND sync_enabled FOR UPDATE`, j.org, j.uid).Scan(&generation)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && generation != j.generation {
		return nil
	}
	if e != nil {
		return e
	}
	if failure != nil {
		_, e = tx.Exec(ctx, `UPDATE gmail_connections SET sync_status='failed',sync_lease_until=NULL WHERE organization_id=$1 AND user_id=$2`, j.org, j.uid)
		if e != nil {
			return e
		}
		if e = audit(ctx, tx, j.org, j.uid, "gmail.sync_failed", "gmail-worker"); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	_, e = tx.Exec(ctx, `DELETE FROM gmail_messages WHERE organization_id=$1 AND user_id=$2`, j.org, j.uid)
	if e != nil {
		return e
	}
	for _, m := range result.Messages {
		_, e = tx.Exec(ctx, `INSERT INTO gmail_messages(organization_id,user_id,id,subject,sender,sender_email,snippet,body_text,unread,received_at,text_limited) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, j.org, j.uid, m.ID, m.Subject, m.Sender, m.SenderEmail, m.Snippet, m.Text, m.Unread, m.ReceivedAt, m.TextLimited)
		if e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, `UPDATE gmail_connections SET sync_status='idle',sync_lease_until=NULL,last_synced_at=now(),sync_limited=$3 WHERE organization_id=$1 AND user_id=$2`, j.org, j.uid, result.Limited)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
