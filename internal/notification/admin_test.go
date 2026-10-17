package notification

import (
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"sync"
	"testing"
)

func TestEmailAdminRequiresPermission(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	a := user.Context{}
	if _, e := s.EmailStatus(ctx, a); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, _, e := s.EmailHistory(ctx, a, "all", 1, 25); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if e := s.RetryEmail(ctx, a, "bad", "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.QueueTestEmail(ctx, a, TestEmailInput{}, "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
}
func testEmailAdministration(t *testing.T, db *pgxpool.Pool, org, uid string) {
	t.Helper()
	ctx := context.Background()
	a := user.Context{Organization: user.Organization{ID: org}, User: user.User{ID: uid}, Permissions: []string{"settings.manage"}}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	uuid := func() string {
		t.Helper()
		var id string
		if e := db.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	exec(`INSERT INTO user_roles(organization_id,user_id,role_key) VALUES($1,$2,'organization_admin') ON CONFLICT DO NOTHING`, org, uid)
	mail := &fakeMailer{}
	s := &Service{DB: db, Mailer: mail, EmailFrom: "notify@example.test"}
	status, e := s.EmailStatus(ctx, a)
	if e != nil || !status.Available || status.TestRecipient != "test@example.test" {
		t.Fatalf("status: %+v %v", status, e)
	}
	key := uuid()
	in := TestEmailInput{IdempotencyKey: key, Confirmed: true}
	if _, e = s.QueueTestEmail(ctx, a, TestEmailInput{IdempotencyKey: key}, "test"); !errors.Is(e, user.ErrInvalid) {
		t.Fatal("confirmation required", e)
	}
	s.Mailer = nil
	if _, e = s.QueueTestEmail(ctx, a, in, "test"); !errors.Is(e, ErrEmailDisabled) {
		t.Fatal(e)
	}
	s.Mailer = mail
	ids := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); id, e := s.QueueTestEmail(ctx, a, in, "test"); ids <- id; errs <- e }()
	}
	wg.Wait()
	id1, id2 := <-ids, <-ids
	if e1, e2 := <-errs, <-errs; e1 != nil || e2 != nil || id1 != id2 {
		t.Fatal("idempotency", id1, id2, e1, e2)
	}
	if _, e = s.QueueTestEmail(ctx, a, TestEmailInput{IdempotencyKey: uuid(), Confirmed: true}, "test"); !errors.Is(e, ErrEmailRate) {
		t.Fatal("rate limit", e)
	}
	if ok, e := s.deliverTestEmail(ctx); e != nil || !ok {
		t.Fatal(e)
	}
	if ok, e := s.deliverTestEmail(ctx); e != nil || ok || mail.count != 1 {
		t.Fatal("duplicate test", ok, e, mail.count)
	}
	list, total, e := s.EmailHistory(ctx, a, "sent", 1, 100)
	if e != nil || total < 1 {
		t.Fatal(e)
	}
	found := false
	for _, d := range list {
		if d.ID == id1 {
			found = d.Kind == "test" && d.Attempts == 1 && !d.CanRetry
		}
	}
	if !found {
		t.Fatal("test not in history")
	}
	foreign := a
	foreign.Organization.ID = uuid()
	list, total, e = s.EmailHistory(ctx, foreign, "all", 1, 25)
	if e != nil || total != 0 || len(list) != 0 {
		t.Fatal("cross tenant history", e)
	}
	if _, _, e = s.EmailHistory(ctx, a, "bad", 1, 25); !errors.Is(e, user.ErrInvalid) {
		t.Fatal(e)
	}
	// Create an eligible exhausted notification without any private content in history.
	failed := func() string {
		t.Helper()
		id := uuid()
		exec(`INSERT INTO notifications(id,organization_id,user_id,kind,source_id,dedup_key,title,body,href) VALUES($1,$2,$3,'reminder',$3,$4,'PRIVATE TITLE','PRIVATE BODY','/notifications')`, id, org, uid, id)
		exec(`INSERT INTO notification_email_deliveries(organization_id,notification_id,user_id,status,attempts) VALUES($1,$2,$3,'failed',5)`, org, id, uid)
		return id
	}
	notice := failed()
	if e = s.RetryEmail(ctx, foreign, notice, "test"); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross tenant retry", e)
	}
	list, _, e = s.EmailHistory(ctx, a, "failed", 1, 100)
	if e != nil {
		t.Fatal(e)
	}
	found = false
	for _, d := range list {
		if d.ID == notice {
			found = d.CanRetry
		}
	}
	if !found {
		t.Fatal("eligible retry hidden")
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("private content leaked")
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.RetryEmail(ctx, a, notice, "test") }()
	}
	wg.Wait()
	first, second := <-errs, <-errs
	if !((first == nil && errors.Is(second, ErrEmailAction)) || (second == nil && errors.Is(first, ErrEmailAction))) {
		t.Fatal("concurrent retry", first, second)
	}
	mail.fail = true
	if ok, e := s.deliverEmail(ctx); e != nil || !ok {
		t.Fatal(e)
	}
	mail.fail = false
	var attempts, retries int
	var state string
	if e = db.QueryRow(ctx, `SELECT attempts,manual_retries,status FROM notification_email_deliveries WHERE notification_id=$1`, notice).Scan(&attempts, &retries, &state); e != nil || attempts != 6 || retries != 1 || state != "failed" {
		t.Fatal(attempts, retries, state, e)
	}
	if e = s.RetryEmail(ctx, a, notice, "test"); !errors.Is(e, ErrEmailAction) {
		t.Fatal("retry limit", e)
	}
	read := failed()
	exec(`UPDATE notifications SET read_at=now() WHERE id=$1`, read)
	if e = s.RetryEmail(ctx, a, read, "test"); !errors.Is(e, ErrEmailAction) {
		t.Fatal("read alert retry", e)
	}
	resolved := failed()
	exec(`UPDATE notifications SET resolved_at=now() WHERE id=$1`, resolved)
	if e = s.RetryEmail(ctx, a, resolved, "test"); !errors.Is(e, ErrEmailAction) {
		t.Fatal("resolved alert retry", e)
	}
	revoked := failed()
	exec(`UPDATE notifications SET required_permission='nonexistent' WHERE id=$1`, revoked)
	if e = s.RetryEmail(ctx, a, revoked, "test"); !errors.Is(e, ErrEmailAction) {
		t.Fatal("revoked retry", e)
	}
	optout := failed()
	if _, e = s.SetPreferences(ctx, a, false, "test"); e != nil {
		t.Fatal(e)
	}
	if e = s.RetryEmail(ctx, a, optout, "test"); !errors.Is(e, ErrEmailAction) {
		t.Fatal("opt out retry", e)
	}
	// Explicit test does not opt the user in, and a failed test is not auto-retried.
	exec(`UPDATE notification_email_tests SET created_at=now()-interval '2 minutes' WHERE organization_id=$1`, org)
	testID, e := s.QueueTestEmail(ctx, a, TestEmailInput{IdempotencyKey: uuid(), Confirmed: true}, "test")
	if e != nil {
		t.Fatal(e)
	}
	mail.fail = true
	before := mail.count
	if _, e = s.deliverTestEmail(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.deliverTestEmail(ctx); e != nil {
		t.Fatal(e)
	}
	mail.fail = false
	if mail.count != before+1 {
		t.Fatal("automatic test replay")
	}
	if e = db.QueryRow(ctx, `SELECT status FROM notification_email_tests WHERE id=$1`, testID).Scan(&state); e != nil || state != "failed" {
		t.Fatal(e, state)
	}
	pref, e := s.Preferences(ctx, a)
	if e != nil || pref.EmailEnabled {
		t.Fatal("test changed opt-in", e)
	}
	exec(`UPDATE notification_email_tests SET created_at=now()-interval '2 minutes' WHERE organization_id=$1`, org)
	testID, e = s.QueueTestEmail(ctx, a, TestEmailInput{IdempotencyKey: uuid(), Confirmed: true}, "test")
	if e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE users SET email='changed@example.test' WHERE id=$1`, uid)
	before = mail.count
	if _, e = s.deliverTestEmail(ctx); e != nil {
		t.Fatal(e)
	}
	if mail.count != before {
		t.Fatal("test sent to changed recipient")
	}
	if e = db.QueryRow(ctx, `SELECT status FROM notification_email_tests WHERE id=$1`, testID).Scan(&state); e != nil || state != "cancelled" {
		t.Fatal(e, state)
	}
	// A queued test cannot survive loss of administrator permission.
	exec(`UPDATE users SET email='test@example.test' WHERE id=$1`, uid)
	exec(`UPDATE notification_email_tests SET created_at=now()-interval '2 minutes' WHERE organization_id=$1`, org)
	testID, e = s.QueueTestEmail(ctx, a, TestEmailInput{IdempotencyKey: uuid(), Confirmed: true}, "test")
	if e != nil {
		t.Fatal(e)
	}
	exec(`DELETE FROM user_roles WHERE organization_id=$1 AND user_id=$2 AND role_key='organization_admin'`, org, uid)
	before = mail.count
	if _, e = s.deliverTestEmail(ctx); e != nil {
		t.Fatal(e)
	}
	if mail.count != before {
		t.Fatal("test sent after admin revocation")
	}
	if e = db.QueryRow(ctx, `SELECT status FROM notification_email_tests WHERE id=$1`, testID).Scan(&state); e != nil || state != "cancelled" {
		t.Fatal(e, state)
	}
	exec(`INSERT INTO user_roles(organization_id,user_id,role_key) VALUES($1,$2,'organization_admin')`, org, uid)
	exec(`UPDATE notification_email_tests SET created_at=now()-interval '2 minutes' WHERE organization_id=$1`, org)
	exec(`INSERT INTO notification_email_tests(organization_id,user_id,recipient,idempotency_key,status,created_at) SELECT $1,$2,'test@example.test',gen_random_uuid(),'cancelled',now()-interval '2 minutes' FROM generate_series(1,10)`, org, uid)
	if _, e = s.QueueTestEmail(ctx, a, TestEmailInput{IdempotencyKey: uuid(), Confirmed: true}, "test"); !errors.Is(e, ErrEmailRate) {
		t.Fatal("hourly limit", e)
	}

}
