package notification

import (
	"ai-business-api/internal/user"
	"ai-business-api/migrations"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
)

type fakeMailer struct {
	mu    sync.Mutex
	count int
	fail  bool
}

func (f *fakeMailer) Send(_ context.Context, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if f.fail {
		return errors.New("fake provider failure")
	}
	return nil
}

func (f *fakeMailer) SendTest(ctx context.Context, to, id string) error { return f.Send(ctx, to, id) }

// Run only against an empty disposable database; never the application database.
func TestEmailQueueIntegration(t *testing.T) {
	dsn := os.Getenv("NOTIFICATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable NOTIFICATION_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("integration test requires empty public schema", e)
	}
	files, _ := migrations.Files.ReadDir(".")
	for _, file := range files {
		sql, _ := migrations.Files.ReadFile(file.Name())
		if _, e = db.Exec(ctx, string(sql)); e != nil {
			t.Fatalf("migration %s: %v", file.Name(), e)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	org := "00000000-0000-0000-0000-000000000001"
	uid := "00000000-0000-0000-0000-000000000002"
	exec(`INSERT INTO organizations(id,name) VALUES($1,'Test')`, org)
	exec(`INSERT INTO users(id,organization_id,subject,email,name) VALUES($1,$2,'test','test@example.test','Test')`, uid, org)
	a := user.Context{Organization: user.Organization{ID: org}, User: user.User{ID: uid}}
	mail := &fakeMailer{}
	s := &Service{DB: db, Mailer: mail}
	newNotice := func(key string) string {
		t.Helper()
		var id string
		e := db.QueryRow(ctx, `INSERT INTO notifications(organization_id,user_id,kind,source_id,dedup_key,title,href) VALUES($1,$2,'reminder',$2,$3,'Private reminder','/notifications') RETURNING id`, org, uid, key).Scan(&id)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	sweep := func() {
		t.Helper()
		if e := s.sweepEmails(ctx); e != nil {
			t.Fatal(e)
		}
	}
	state := func(id, want string) {
		t.Helper()
		var got string
		if e := db.QueryRow(ctx, `SELECT status FROM notification_email_deliveries WHERE organization_id=$1 AND notification_id=$2`, org, id).Scan(&got); e != nil || got != want {
			t.Fatalf("status %s want %s: %v", got, want, e)
		}
	}
	old := newNotice("before-opt-in")
	sweep()
	if mail.count != 0 {
		t.Fatal("sent without opt-in")
	}
	if _, e = s.SetPreferences(ctx, a, true, "test"); e != nil {
		t.Fatal(e)
	}
	first := newNotice("new")
	sweep()
	sweep()
	state(first, "sent")
	if mail.count != 1 {
		t.Fatal("duplicate send")
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM notification_email_deliveries WHERE notification_id=$1`, old).Scan(&count); e != nil || count != 0 {
		t.Fatal("old notification queued")
	}
	retry := newNotice("retry")
	mail.fail = true
	sweep()
	state(retry, "pending")
	before := mail.count
	sweep()
	if mail.count != before {
		t.Fatal("ignored backoff")
	}
	for i := 0; i < 4; i++ {
		exec(`UPDATE notification_email_deliveries SET next_attempt_at=now() WHERE notification_id=$1`, retry)
		sweep()
	}
	state(retry, "failed")
	mail.fail = false
	read := newNotice("read")
	if e = s.enqueueEmails(ctx); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE notifications SET read_at=now() WHERE id=$1`, read)
	sweep()
	state(read, "cancelled")
	revoked := newNotice("revoked")
	exec(`UPDATE notifications SET required_permission='quote.approve' WHERE id=$1`, revoked)
	sweep()
	state(revoked, "cancelled")
	inactive := newNotice("inactive")
	if e = s.enqueueEmails(ctx); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE users SET active=false WHERE id=$1`, uid)
	sweep()
	state(inactive, "cancelled")
	exec(`UPDATE users SET active=true WHERE id=$1`, uid)
	cancel := newNotice("opt-out")
	if e = s.enqueueEmails(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetPreferences(ctx, a, false, "test"); e != nil {
		t.Fatal(e)
	}
	sweep()
	state(cancel, "cancelled")
	if _, e = s.SetPreferences(ctx, a, true, "test"); e != nil {
		t.Fatal(e)
	}
	concurrent := newNotice("concurrent")
	if e = s.enqueueEmails(ctx); e != nil {
		t.Fatal(e)
	}
	before = mail.count
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.deliverEmail(ctx); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	state(concurrent, "sent")
	if mail.count != before+1 {
		t.Fatal("concurrent duplicate")
	}
	other := user.Context{Organization: user.Organization{ID: "00000000-0000-0000-0000-000000000003"}, User: user.User{ID: uid}}
	if _, e = s.SetPreferences(ctx, other, true, "test"); e == nil {
		t.Fatal("cross-tenant write")
	}
	testEmailAdministration(t, db, org, uid)
}
