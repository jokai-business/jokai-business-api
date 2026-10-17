package gmail

import (
	"ai-business-api/internal/user"
	"ai-business-api/platform/googlemail"
	"context"
	"errors"
	"net/url"
	"testing"
	"time"
)

func (f *fakeProvider) Recent(context.Context, string, string, time.Time) (googlemail.Snapshot, error) {
	f.syncCalls++
	if f.onRecent != nil {
		f.onRecent()
	}
	if f.syncFail {
		return googlemail.Snapshot{}, errors.New("private provider detail")
	}
	return f.snapshot, nil
}
func testSyncLifecycle(t *testing.T, s *Service, a user.Context, f *fakeProvider, other, foreign string) {
	t.Helper()
	ctx := context.Background()
	db := s.DB
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	due := func() {
		exec(`UPDATE gmail_connections SET next_sync_at=now()-interval '1 second' WHERE organization_id=$1 AND user_id=$2`, a.Organization.ID, a.User.ID)
	}
	work := func() {
		t.Helper()
		ok, e := s.processSync(ctx)
		if e != nil || !ok {
			t.Fatal("sync work", ok, e)
		}
	}
	if ok, e := s.processSync(ctx); e != nil || ok {
		t.Fatal("synced without opt-in", ok, e)
	}
	customerID := "00000000-0000-0000-0000-000000000010"
	exec(`INSERT INTO customers(id,organization_id,name,email,owner_id) VALUES($1,$2,'Matched customer',' Sender@Example.Test ',$3)`, customerID, a.Organization.ID, a.User.ID)
	exec(`INSERT INTO contacts(organization_id,customer_id,name,email) VALUES($1,$2,'Duplicate same customer','sender@example.test')`, a.Organization.ID, customerID)
	f.snapshot = googlemail.Snapshot{Messages: []googlemail.Message{{ID: "one", Subject: "Hello", SenderEmail: "sender@example.test", Text: "private text", ReceivedAt: time.Now(), Unread: true}, {ID: "two", Subject: "Unmatched", SenderEmail: "other@example.test", ReceivedAt: time.Now()}}}
	if _, e := s.SetSync(ctx, a, true, "test"); e != nil {
		t.Fatal(e)
	}
	work()
	reader := a
	reader.Permissions = []string{"customer.read"}
	items, total, e := s.Messages(ctx, reader, "all", "", 1, 25)
	if e != nil || total != 2 || len(items) != 2 {
		t.Fatal(items, total, e)
	}
	linked, total, e := s.Messages(ctx, reader, "linked", "", 1, 25)
	if e != nil || total != 1 || linked[0].CustomerID == nil || *linked[0].CustomerID != customerID {
		t.Fatal(linked, total, e)
	}
	if _, n, e := s.Messages(ctx, reader, "unread", "", 1, 25); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if _, n, e := s.Messages(ctx, reader, "all", customerID, 1, 25); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	for _, actor := range []user.Context{{User: user.User{ID: other}, Organization: a.Organization}, {User: a.User, Organization: user.Organization{ID: foreign}}} {
		if rows, n, e := s.Messages(ctx, actor, "all", "", 1, 25); e != nil || n != 0 || len(rows) != 0 {
			t.Fatal("mailbox leaked", n, e)
		}
	}
	private, _, e := s.Messages(ctx, a, "all", "", 1, 25)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range private {
		if m.CustomerID != nil || m.CustomerName != nil {
			t.Fatal("customer permission leak")
		}
	}
	if _, _, e = s.Messages(ctx, a, "linked", "", 1, 25); e != user.ErrForbidden {
		t.Fatal("linked filter permission", e)
	}
	exec(`INSERT INTO customers(organization_id,name,email,owner_id) VALUES($1,'Ambiguous','sender@example.test',$2)`, a.Organization.ID, a.User.ID)
	if _, n, e := s.Messages(ctx, reader, "linked", "", 1, 25); e != nil || n != 0 {
		t.Fatal("guessed ambiguous match", n, e)
	}
	exec(`DELETE FROM customers WHERE name='Ambiguous'`)
	if _, e = s.SetSync(ctx, a, true, "test"); e != nil {
		t.Fatal(e)
	}
	if ok, e := s.processSync(ctx); ok || e != nil {
		t.Fatal("enable bypassed interval", ok, e)
	}
	f.syncFail = true
	due()
	work()
	st, e := s.SyncStatus(ctx, a)
	if e != nil || st.Status != "failed" || st.LastSyncedAt == nil {
		t.Fatal(st, e)
	}
	if _, n, e := s.Messages(ctx, reader, "all", "", 1, 25); e != nil || n != 2 {
		t.Fatal("failed fetch replaced snapshot", n, e)
	}
	f.syncFail = false
	// A pause during the external fetch invalidates the worker result.
	f.snapshot.Messages = nil
	f.onRecent = func() {
		if _, e := s.SetSync(ctx, a, false, "test"); e != nil {
			t.Fatal(e)
		}
	}
	due()
	work()
	f.onRecent = nil
	if _, n, e := s.Messages(ctx, reader, "all", "", 1, 25); e != nil || n != 2 {
		t.Fatal("paused worker published", n, e)
	}
	// Empty successful snapshots remove stale messages; an expired lease is recoverable.
	if _, e = s.SetSync(ctx, a, true, "test"); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE gmail_connections SET sync_status='syncing',sync_lease_until=now()-interval '1 second',next_sync_at=now()-interval '1 second'`)
	work()
	if _, n, e := s.Messages(ctx, reader, "all", "", 1, 25); e != nil || n != 0 {
		t.Fatal("stale snapshot retained", n, e)
	}
	// Disconnect/reconnect while a fetch is running cannot restore the old mailbox snapshot.
	f.snapshot.Messages = []googlemail.Message{{ID: "stale", ReceivedAt: time.Now(), Text: "stale mailbox data"}}
	f.onRecent = func() {
		if _, e := s.Disconnect(ctx, a, "test"); e != nil {
			t.Fatal(e)
		}
		start, e := s.Begin(ctx, a, "test")
		if e != nil {
			t.Fatal(e)
		}
		u, _ := url.Parse(start.AuthorizationURL)
		if e = s.Complete(ctx, u.Query().Get("state"), start.BrowserSecret, "code", false, "test"); e != nil {
			t.Fatal(e)
		}
	}
	due()
	work()
	f.onRecent = nil
	if _, n, e := s.Messages(ctx, reader, "all", "", 1, 25); e != nil || n != 0 {
		t.Fatal("old worker restored disconnected mailbox", n, e)
	}
	if status, e := s.SyncStatus(ctx, a); e != nil || status.Enabled {
		t.Fatal("reconnect enabled sync without consent", status, e)
	}

	if _, e = s.SetSync(ctx, a, false, "test"); e != nil {
		t.Fatal(e)
	}
}
