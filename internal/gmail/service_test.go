package gmail

import (
	"ai-business-api/internal/user"
	"ai-business-api/migrations"
	"ai-business-api/platform/googlemail"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

type fakeProvider struct {
	snapshot         googlemail.Snapshot
	syncFail         bool
	onRecent         func()
	syncCalls        int
	fail, revokeFail bool
	calls            int
	mu               sync.Mutex
}

func (f *fakeProvider) AuthorizationURL(state, verifier string) string {
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state
}
func (f *fakeProvider) Exchange(context.Context, string, string) (googlemail.Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return googlemail.Grant{}, errors.New("private-error")
	}
	return googlemail.Grant{Email: "mailbox@example.test", RefreshToken: "secret-token"}, nil
}
func (f *fakeProvider) Revoke(context.Context, string) error {
	if f.revokeFail {
		return errors.New("private-error")
	}
	return nil
}
func TestDisabled(t *testing.T) {
	s := &Service{}
	if _, e := s.Begin(context.Background(), user.Context{}, "r"); e != ErrDisabled {
		t.Fatal(e)
	}
}
func TestConnectionIntegration(t *testing.T) {
	dsn := os.Getenv("GMAIL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable GMAIL_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("requires empty database", e)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	files, _ := migrations.Files.ReadDir(".")
	for _, f := range files {
		sql, _ := migrations.Files.ReadFile(f.Name())
		exec(string(sql))
	}
	org := "00000000-0000-0000-0000-000000000001"
	uid := "00000000-0000-0000-0000-000000000002"
	other := "00000000-0000-0000-0000-000000000003"
	foreign := "00000000-0000-0000-0000-000000000004"
	exec(`INSERT INTO organizations(id,name) VALUES($1,'Test'),($2,'Foreign')`, org, foreign)
	exec(`INSERT INTO users(id,organization_id,subject,email,name) VALUES($1,$2,'one','one@example.test','One'),($3,$2,'two','two@example.test','Two')`, uid, org, other)
	a := user.Context{User: user.User{ID: uid}, Organization: user.Organization{ID: org}}
	vault, _ := googlemail.NewVault(make([]byte, 32))
	provider := &fakeProvider{}
	s := &Service{DB: db, Provider: provider, Vault: vault}
	begin := func() (Start, string) {
		t.Helper()
		v, e := s.Begin(ctx, a, "r")
		if e != nil {
			t.Fatal(e)
		}
		u, _ := url.Parse(v.AuthorizationURL)
		return v, u.Query().Get("state")
	}
	v, state := begin()
	if e = s.Complete(ctx, state, strings.Repeat("x", 43), "code", false, "r"); e != ErrState {
		t.Fatal("wrong browser", e)
	}
	exec(`UPDATE gmail_oauth_states SET expires_at=now()-interval '1 second'`)
	if e = s.Complete(ctx, state, v.BrowserSecret, "code", false, "r"); e != ErrState {
		t.Fatal("expired", e)
	}
	old, oldState := begin()
	v, state = begin()
	if e = s.Complete(ctx, oldState, old.BrowserSecret, "code", false, "r"); e != ErrState {
		t.Fatal("old tab", e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.Complete(ctx, state, v.BrowserSecret, "code", false, "r") })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if e != ErrState {
			t.Fatal(e)
		}
	}
	if success != 1 || provider.calls != 1 {
		t.Fatal("replayed callback", success, provider.calls)
	}
	status, e := s.Status(ctx, a)
	if e != nil || !status.Connected || status.Email != "mailbox@example.test" {
		t.Fatal(status, e)
	}
	testSyncLifecycle(t, s, a, provider, other, foreign)
	for _, actor := range []user.Context{{User: user.User{ID: other}, Organization: a.Organization}, {User: a.User, Organization: user.Organization{ID: foreign}}} {
		v, e := s.Status(ctx, actor)
		if e != nil || v.Connected || v.Email != "" {
			t.Fatal("leaked another mailbox", v, e)
		}
	}
	otherActor := user.Context{User: user.User{ID: other}, Organization: a.Organization}
	if _, e = s.Disconnect(ctx, otherActor, "r"); e != nil {
		t.Fatal(e)
	}
	if current, e := s.Status(ctx, a); e != nil || !current.Connected {
		t.Fatal("another member disconnected owner", e)
	}
	var stored []byte
	db.QueryRow(ctx, `SELECT refresh_token FROM gmail_connections`).Scan(&stored)
	if strings.Contains(string(stored), "secret-token") {
		t.Fatal("plaintext token")
	}
	if plain, e := vault.Open(stored, owner(org, uid, "token")); e != nil || plain != "secret-token" {
		t.Fatal("encrypted credential", e)
	}
	// Failed reconnect retains the old valid connection and consumes its state.
	provider.fail = true
	v, state = begin()
	if e = s.Complete(ctx, state, v.BrowserSecret, "code", false, "r"); e != googlemail.ErrProvider {
		t.Fatal(e)
	}
	if e = s.Complete(ctx, state, v.BrowserSecret, "code", false, "r"); e != ErrState {
		t.Fatal("failed state replay", e)
	}
	provider.fail = false
	v, state = begin()
	provider.revokeFail = true
	r, e := s.Disconnect(ctx, a, "r")
	if e != nil || r.Revoked {
		t.Fatal("revocation failure hidden", r, e)
	}
	if e = s.Complete(ctx, state, v.BrowserSecret, "code", false, "r"); e != ErrState {
		t.Fatal("disconnect left active callback", e)
	}
	status, e = s.Status(ctx, a)
	if e != nil || status.Connected {
		t.Fatal("disconnect did not remove credentials", status, e)
	}
	v, state = begin()
	exec(`UPDATE users SET active=false WHERE id=$1`, uid)
	if e = s.Complete(ctx, state, v.BrowserSecret, "code", false, "r"); e != user.ErrForbidden {
		t.Fatal("inactive user", e)
	}
	exec(`UPDATE users SET active=true WHERE id=$1`, uid)
	v, state = begin()
	if e = s.Complete(ctx, state, v.BrowserSecret, "", true, "r"); e != ErrState {
		t.Fatal("denied consent", e)
	}
	if e = s.Complete(ctx, state, v.BrowserSecret, "code", false, "r"); e != ErrState {
		t.Fatal("denied state replay", e)
	}
	provider.revokeFail = false
	v, state = begin()
	if e = s.Complete(ctx, state, v.BrowserSecret, "code", false, "r"); e != nil {
		t.Fatal(e)
	}
	r, e = s.Disconnect(ctx, a, "r")
	if e != nil || !r.Revoked {
		t.Fatal("successful revocation", r, e)
	}
	r, e = s.Disconnect(ctx, a, "r")
	if e != nil || !r.Revoked {
		t.Fatal("repeat disconnect", r, e)
	}
	var leaked int
	e = db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE coalesce(before_value::text,'')||coalesce(after_value::text,'') LIKE '%secret-token%' OR coalesce(after_value::text,'') LIKE '%mailbox@%'`).Scan(&leaked)
	if e != nil || leaked != 0 {
		t.Fatal("audit privacy", e)
	}
}
