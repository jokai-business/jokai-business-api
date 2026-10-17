package businessai

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"ai-business-api/migrations"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
)

type fakeLLM struct {
	calls    int
	input    string
	callback func()
	fail     bool
}

func (f *fakeLLM) Generate(_ context.Context, r ai.Request) (string, error) {
	f.calls++
	f.input = r.Input
	if f.callback != nil {
		f.callback()
	}
	if f.fail {
		return "", errors.New("private provider error")
	}
	return "PRIVATE_RESULT: customer overview", nil
}
func TestSummaryIntegration(t *testing.T) {
	dsn := os.Getenv("SUMMARY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable SUMMARY_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("requires empty DB", e)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	files, _ := migrations.Files.ReadDir(".")
	for _, f := range files {
		b, _ := migrations.Files.ReadFile(f.Name())
		exec(string(b))
	}
	org := "00000000-0000-0000-0000-000000000001"
	uid := "00000000-0000-0000-0000-000000000002"
	other := "00000000-0000-0000-0000-000000000003"
	foreign := "00000000-0000-0000-0000-000000000004"
	cid := "00000000-0000-0000-0000-000000000005"
	oid := "00000000-0000-0000-0000-000000000006"
	qid := "00000000-0000-0000-0000-000000000007"
	exec(`INSERT INTO organizations(id,name)VALUES($1,'Test'),($2,'Foreign')`, org, foreign)
	exec(`INSERT INTO users(id,organization_id,subject,email,name)VALUES($1,$2,'summary-one','one@example.test','One'),($3,$2,'summary-two','two@example.test','Two')`, uid, org, other)
	exec(`INSERT INTO roles(key,name)VALUES('summary_reader','Test reader')`)
	exec(`INSERT INTO role_permissions VALUES('summary_reader','customer.read')`)
	exec(`INSERT INTO user_roles VALUES($1,$2,'summary_reader'),($1,$3,'summary_reader')`, org, uid, other)
	exec(`INSERT INTO customers(id,organization_id,name,email,owner_id,notes)VALUES($1,$2,'Customer','sender@example.test',$3,'PRIVATE_CUSTOMER')`, cid, org, uid)
	exec(`INSERT INTO contacts(organization_id,customer_id,name) SELECT $1,$2,'Contact '||n FROM generate_series(1,6)n`, org, cid)
	exec(`INSERT INTO opportunities(id,organization_id,customer_id,title,stage_id,expected_value,currency,probability,owner_id,source)SELECT $1,$2,$3,'Deal',id,100,'USD',50,$4,'Manual' FROM pipeline_stages WHERE organization_id=$2 ORDER BY position LIMIT 1`, oid, org, cid, uid)
	exec(`INSERT INTO quotes(id,organization_id,customer_id,opportunity_id,creator_id,number,company_name,customer_name,customer_email,opportunity_title,currency,status,expiry_date,terms,subtotal,discount_total,total)VALUES($1,$2,$3,$4,$5,'SECRET_QUOTE','Test','Customer','','Deal','USD','accepted',current_date+30,'',100,0,100)`, qid, org, cid, oid, uid)
	exec(`INSERT INTO invoices(organization_id,quote_id,customer_id,creator_id,number,quote_number,company_name,customer_name,customer_email,currency,subtotal,tax,total,issue_date,due_date,state)VALUES($1,$2,$3,$4,'SECRET_INVOICE','Q','Test','Customer','','USD',100,0,100,current_date,current_date+30,'issued')`, org, qid, cid, uid)
	exec(`INSERT INTO gmail_connections(organization_id,user_id,email,refresh_token)VALUES($1,$2,'one@example.test','synthetic'),($1,$3,'two@example.test','synthetic')`, org, uid, other)
	exec(`INSERT INTO gmail_messages(organization_id,user_id,id,subject,sender,sender_email,snippet,body_text,unread,received_at,text_limited)VALUES($1,$2,'one','Subject','Sender','sender@example.test','','PRIVATE_EMAIL',false,now(),false),($1,$3,'two','Subject','Sender','sender@example.test','','OTHER_MAILBOX_SECRET',false,now(),false)`, org, uid, other)
	a := user.Context{User: user.User{ID: uid}, Organization: user.Organization{ID: org}}
	f := &fakeLLM{}
	s := &Service{DB: db, LLM: f, Provider: "Fixture", Model: "fixture-model"}
	view := func() View {
		t.Helper()
		v, e := s.Get(ctx, a, "customer", cid)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	input := func() Input {
		v := view()
		version := 0
		if v.Result != nil {
			version = v.Result.Version
		}
		return Input{Version: version, SourceHash: v.SourceHash, Provider: s.Provider, Model: s.Model, Confirmed: true}
	}
	request := func() {
		t.Helper()
		if e := s.Request(ctx, a, "customer", cid, input(), "test"); e != nil {
			t.Fatal(e)
		}
	}
	work := func() {
		t.Helper()
		worked, e := s.process(ctx)
		if e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	v := view()
	b, _ := json.Marshal(v.Context)
	if strings.Contains(string(b), "SECRET_QUOTE") || strings.Contains(string(b), "SECRET_INVOICE") || strings.Contains(string(b), "OTHER_MAILBOX_SECRET") || !strings.Contains(string(b), "PRIVATE_EMAIL") {
		t.Fatal("context privacy", string(b))
	}
	if v.Context.Sections[1].Total != 6 || len(v.Context.Sections[1].Records) != 5 {
		t.Fatal("unbounded contacts", v.Context)
	}
	if _, e := s.Get(ctx, a, "opportunity", oid); e != user.ErrForbidden {
		t.Fatal("opportunity permission", e)
	}
	foreignActor := a
	foreignActor.Organization.ID = foreign
	if _, e := s.Get(ctx, foreignActor, "customer", cid); e != user.ErrForbidden {
		t.Fatal("cross tenant", e)
	}
	if _, e := s.Get(ctx, a, "customer", qid); e != ErrNotFound {
		t.Fatal("unknown source", e)
	}
	in := input()
	in.Confirmed = false
	if e := s.Request(ctx, a, "customer", cid, in, "test"); e != user.ErrInvalid {
		t.Fatal("consent", e)
	}
	in = input()
	in.Provider = "different"
	if e := s.Request(ctx, a, "customer", cid, in, "test"); e != ErrChanged {
		t.Fatal("provider consent", e)
	}
	in = input()
	exec(`UPDATE customers SET notes='changed' WHERE id=$1`, cid)
	if e := s.Request(ctx, a, "customer", cid, in, "test"); e != ErrChanged {
		t.Fatal("stale preview", e)
	}
	in = input()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.Request(ctx, a, "customer", cid, in, "test") })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if e != ErrChanged {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("duplicate request", success)
	}
	work()
	if view().Result.Status != "ready" || f.calls != 1 {
		t.Fatal("generation")
	}
	if strings.Contains(f.input, "OTHER_MAILBOX_SECRET") || strings.Contains(f.input, "SECRET_INVOICE") {
		t.Fatal("provider data leak")
	}
	otherActor := a
	otherActor.User.ID = other
	ov, e := s.Get(ctx, otherActor, "customer", cid)
	if e != nil || ov.Result != nil {
		t.Fatal("shared result", e)
	}
	s.LLM = nil
	if !strings.Contains(view().Result.Output, "PRIVATE_RESULT") {
		t.Fatal("disabled hides saved result")
	}
	if e := s.Request(ctx, a, "customer", cid, input(), "test"); e != ErrDisabled {
		t.Fatal(e)
	}
	s.LLM = f
	// Adding permissions changes context and invalidates previous output.
	exec(`INSERT INTO role_permissions VALUES('summary_reader','opportunity.read'),('summary_reader','quote.read'),('summary_reader','invoice.read'),('summary_reader','project.read')`)
	v = view()
	if !v.Result.Stale || v.Result.Output != "" {
		t.Fatal("stale disclosure")
	}
	b, _ = json.Marshal(v.Context)
	if !strings.Contains(string(b), "SECRET_INVOICE") || !strings.Contains(string(b), "SECRET_QUOTE") {
		t.Fatal("permitted context missing")
	}
	exec(`DELETE FROM role_permissions WHERE role_key='summary_reader' AND permission_key='customer.read'`)
	if _, e := s.Get(ctx, a, "customer", cid); e != user.ErrForbidden {
		t.Fatal("revoked root access", e)
	}
	exec(`INSERT INTO role_permissions VALUES('summary_reader','customer.read')`)
	op, e := s.Get(ctx, a, "opportunity", oid)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = json.Marshal(op.Context)
	if strings.Contains(string(b), "PRIVATE_EMAIL") {
		t.Fatal("unrelated email in opportunity")
	}
	if e := s.Request(ctx, a, "opportunity", oid, Input{SourceHash: op.SourceHash, Provider: s.Provider, Model: s.Model, Confirmed: true}, "test"); e != nil {
		t.Fatal(e)
	}
	if e := s.Request(ctx, a, "customer", cid, input(), "test"); e != ErrLimit {
		t.Fatal("pending across kinds", e)
	}
	work()
	op, e = s.Get(ctx, a, "opportunity", oid)
	if e != nil || op.Result == nil || op.Result.Status != "ready" {
		t.Fatal("opportunity generation", op, e)
	}
	exec(`UPDATE opportunities SET next_action='Changed follow up' WHERE id=$1`, oid)
	op, e = s.Get(ctx, a, "opportunity", oid)
	if e != nil || !op.Result.Stale || op.Result.Output != "" {
		t.Fatal("stale opportunity", e)
	}
	request()
	f.callback = func() {
		exec(`DELETE FROM role_permissions WHERE role_key='summary_reader' AND permission_key='invoice.read'`)
	}
	work()
	f.callback = nil
	if view().Result.Status != "failed" {
		t.Fatal("permissions changed during provider call")
	}
	request()
	f.callback = func() { exec(`UPDATE customers SET notes='changed during generation' WHERE id=$1`, cid) }
	work()
	f.callback = nil
	if view().Result.Status != "failed" {
		t.Fatal("source changed during generation")
	}
	request()
	f.fail = true
	work()
	f.fail = false
	calls := f.calls
	if worked, e := s.process(ctx); worked || e != nil || f.calls != calls {
		t.Fatal("automatic retry")
	}
	request()
	exec(`UPDATE users SET active=false WHERE id=$1`, uid)
	calls = f.calls
	work()
	if f.calls != calls {
		t.Fatal("inactive data sent")
	}
	exec(`UPDATE users SET active=true WHERE id=$1`, uid)
	request()
	exec(`UPDATE business_summaries SET status='processing',lease_until=now()-interval '1 second' WHERE kind='customer'`)
	if worked, e := s.process(ctx); worked || e != nil {
		t.Fatal(worked, e)
	}
	if view().Result.Status != "failed" {
		t.Fatal("expired lease")
	}
	request()
	f.callback = func() {
		exec(`DELETE FROM business_summaries`)
		request()
		exec(`UPDATE business_summaries SET status='processing',lease_until=now()+interval '90 seconds'`)
	}
	work()
	f.callback = nil
	if view().Result.Status != "processing" {
		t.Fatal("old worker replaced new job")
	}
	exec(`UPDATE business_summaries SET status='failed',lease_until=NULL`)
	exec(`INSERT INTO audit_logs(organization_id,actor_id,resource_id,action,request_id)SELECT $1,$2,$3,'ai.summary_requested','rate' FROM generate_series(1,20)`, org, uid, cid)
	if e := s.Request(ctx, a, "customer", cid, input(), "test"); e != ErrLimit {
		t.Fatal("rate", e)
	}
	var leaked bool
	if e = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_logs WHERE row_to_json(audit_logs)::text LIKE '%PRIVATE_%' OR row_to_json(audit_logs)::text LIKE '%SECRET_%')`).Scan(&leaked); e != nil || leaked {
		t.Fatal("audit leak", e)
	}
	exec(`DELETE FROM gmail_connections WHERE organization_id=$1 AND user_id=$2`, org, uid)
	if view().Result != nil {
		t.Fatal("disconnect retained derived customer summary")
	}
	exec(`DELETE FROM audit_logs WHERE request_id='rate'`)
	request()
	work()
	exec(`UPDATE business_summaries SET requested_at=now()-interval '31 days'`)
	if worked, e := s.process(ctx); worked || e != nil {
		t.Fatal(worked, e)
	}
	if view().Result != nil {
		t.Fatal("retention")
	}
}
func TestContextBounds(t *testing.T) {
	v := Context{Kind: "customer", Sections: []Section{{Name: "test", Records: []json.RawMessage{json.RawMessage(`{"text":"` + strings.Repeat("a", 37000) + `"}`)}}}}
	if _, _, e := fingerprint(v); e != ErrContext {
		t.Fatal(e)
	}
}
