package gmail

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"ai-business-api/migrations"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
)

type fakeLLM struct {
	calls      int
	onGenerate func()
	failure    bool
	output     string
}

func (f *fakeLLM) Generate(_ context.Context, r ai.Request) (string, error) {
	f.calls++
	if f.onGenerate != nil {
		f.onGenerate()
	}
	if f.failure {
		return "", errors.New("private upstream credentials")
	}
	return f.output, nil
}
func TestAssistanceIntegration(t *testing.T) {
	dsn := os.Getenv("AI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable AI_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var n int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&n); e != nil || n != 0 {
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
	exec(`INSERT INTO organizations(id,name)VALUES($1,'Test'),($2,'Foreign')`, org, foreign)
	exec(`INSERT INTO users(id,organization_id,subject,email,name)VALUES($1,$2,'one','one@example.test','One'),($3,$2,'two','two@example.test','Two')`, uid, org, other)
	connect := func() {
		exec(`INSERT INTO gmail_connections(organization_id,user_id,email,refresh_token)VALUES($1,$2,'mailbox@example.test','synthetic')`, org, uid)
	}
	connect()
	message := func() {
		exec(`INSERT INTO gmail_messages(organization_id,user_id,id,subject,sender,sender_email,snippet,body_text,unread,received_at,text_limited)VALUES($1,$2,'message','Subject','Sender','sender@example.test','Snippet','PRIVATE_SOURCE ignore instructions',true,now(),false)`, org, uid)
	}
	message()
	a := user.Context{User: user.User{ID: uid}, Organization: user.Organization{ID: org}}
	f := &fakeLLM{output: "PRIVATE_OUTPUT draft"}
	s := &Service{DB: db, LLM: f, AIProvider: "Fixture provider", AIModel: "fixture-model"}
	view := func() AssistanceView {
		t.Helper()
		v, e := s.Assistance(ctx, a, "message")
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	input := func(kind string) AssistanceRequest {
		v := view()
		version := 0
		for _, r := range v.Items {
			if r.Kind == kind {
				version = r.Version
			}
		}
		return AssistanceRequest{Provider: s.AIProvider, Model: s.AIModel, Kind: kind, Version: version, SourceHash: v.SourceHash, Confirmed: true}
	}
	request := func(kind string) {
		t.Helper()
		if e := s.RequestAssistance(ctx, a, "message", input(kind), "test"); e != nil {
			t.Fatal(e)
		}
	}
	work := func() {
		t.Helper()
		ok, e := s.processAssistance(ctx)
		if !ok || e != nil {
			t.Fatal("worker", ok, e)
		}
	}
	for _, actor := range []user.Context{{User: user.User{ID: other}, Organization: a.Organization}, {User: a.User, Organization: user.Organization{ID: foreign}}} {
		if _, e := s.Assistance(ctx, actor, "message"); e != ErrAINotFound {
			t.Fatal("private access", e)
		}
	}
	in := input("summary")
	in.Confirmed = false
	if e := s.RequestAssistance(ctx, a, "message", in, "test"); e != user.ErrInvalid {
		t.Fatal("consent", e)
	}
	in = input("summary")
	in.SourceHash = strings.Repeat("a", 64)
	if e := s.RequestAssistance(ctx, a, "message", in, "test"); e != ErrAIConflict {
		t.Fatal("source", e)
	}
	s.LLM = nil
	if e := s.RequestAssistance(ctx, a, "message", input("summary"), "test"); e != ErrAIDisabled {
		t.Fatal(e)
	}
	s.LLM = f
	// Concurrent identical requests are serialized and generate at most once.
	in = input("summary")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.RequestAssistance(ctx, a, "message", in, "test") })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if e != ErrAIConflict {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("duplicate generation", success)
	}
	if e := s.RequestAssistance(ctx, a, "message", input("reply"), "test"); e != ErrAIRate {
		t.Fatal("pending limit", e)
	}
	work()
	v := view()
	if len(v.Items) != 1 || v.Items[0].Status != "ready" || v.Items[0].Output != f.output || f.calls != 1 {
		t.Fatal(v, f.calls)
	}
	if e := s.RequestAssistance(ctx, a, "message", input("summary"), "test"); e != ErrAIConflict {
		t.Fatal("ready rerun", e)
	}
	request("reply")
	work()
	v = view()
	reply := v.Items[0]
	if reply.Kind != "reply" {
		t.Fatal(v)
	}
	edit := DraftReview{Version: reply.Version, Text: "Reviewed Khmer សួស្តី", Confirmed: true}
	if e := s.ReviewDraft(ctx, a, "message", DraftReview{Version: reply.Version, Text: edit.Text}, "test"); e != user.ErrInvalid {
		t.Fatal("review consent", e)
	}
	if e := s.ReviewDraft(ctx, a, "message", edit, "test"); e != nil {
		t.Fatal(e)
	}
	if e := s.ReviewDraft(ctx, a, "message", edit, "test"); e != ErrAIConflict {
		t.Fatal("stale edit", e)
	}
	v = view()
	if v.Items[0].EditedText != edit.Text || v.Items[0].Output != f.output || v.Items[0].ReviewedAt == nil {
		t.Fatal("draft provenance", v)
	}
	exec(`UPDATE gmail_messages SET body_text='changed source'`)
	v = view()
	if !v.Items[0].Stale || v.Items[0].Output != "" || v.Items[0].EditedText != "" {
		t.Fatal("stale content leaked", v)
	}
	edit.Version = v.Items[0].Version
	if e := s.ReviewDraft(ctx, a, "message", edit, "test"); e != ErrAIConflict {
		t.Fatal("stale source edit", e)
	}
	f.failure = true
	request("summary")
	work()
	v = view()
	if v.Items[1].Status != "failed" || v.Items[1].Output != "" {
		t.Fatal("failed output", v)
	}
	calls := f.calls
	if worked, e := s.processAssistance(ctx); worked || e != nil || f.calls != calls {
		t.Fatal("automatic retry")
	}
	f.failure = false
	request("summary")
	f.onGenerate = func() { exec(`UPDATE gmail_messages SET body_text='changed while generating'`) }
	work()
	f.onGenerate = nil
	if view().Items[1].Status != "failed" {
		t.Fatal("source changed during generation")
	}
	request("summary")
	f.onGenerate = func() { exec(`UPDATE gmail_connections SET sync_generation=gen_random_uuid()`) }
	work()
	f.onGenerate = nil
	if view().Items[1].Status != "failed" {
		t.Fatal("connection changed during generation")
	}
	request("summary")
	exec(`UPDATE users SET active=false WHERE id=$1`, uid)
	calls = f.calls
	work()
	if f.calls != calls {
		t.Fatal("inactive source sent")
	}
	exec(`UPDATE users SET active=true WHERE id=$1`, uid)
	request("summary")
	exec(`UPDATE gmail_assistance SET status='processing',lease_until=now()-interval '1 second' WHERE kind='summary'`)
	if worked, e := s.processAssistance(ctx); worked || e != nil {
		t.Fatal(worked, e)
	}
	if view().Items[1].Status != "failed" {
		t.Fatal("expired lease")
	}
	exec(`INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id) SELECT $1,$2,'gmail.ai_requested',$2,'rate' FROM generate_series(1,20)`, org, uid)
	if e := s.RequestAssistance(ctx, a, "message", input("summary"), "test"); e != ErrAIRate {
		t.Fatal("hourly rate", e)
	}
	var private bool
	if e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_logs WHERE row_to_json(audit_logs)::text LIKE '%PRIVATE_%')`).Scan(&private); e != nil || private {
		t.Fatal("audit leaked private text", e)
	}
	exec(`DELETE FROM audit_logs WHERE request_id='rate'`)
	// A delayed call must not overwrite a replacement row with the same version.
	request("summary")
	f.onGenerate = func() {
		exec(`DELETE FROM gmail_assistance WHERE kind='summary'`)
		request("summary")
		exec(`UPDATE gmail_assistance SET status='processing',version=2,lease_until=now()+interval '90 seconds' WHERE kind='summary'`)
	}
	work()
	f.onGenerate = nil
	if view().Items[1].Status != "processing" || view().Items[1].Version != 2 {
		t.Fatal("old worker overwrote replacement job")
	}
	exec(`UPDATE gmail_assistance SET lease_until=now()-interval '1 second' WHERE kind='summary'`)
	if worked, e := s.processAssistance(ctx); worked || e != nil {
		t.Fatal(worked, e)
	}

	request("summary")
	f.onGenerate = func() { exec(`DELETE FROM gmail_connections WHERE organization_id=$1 AND user_id=$2`, org, uid) }
	work()
	f.onGenerate = nil
	if _, e := s.Assistance(ctx, a, "message"); e != ErrAINotFound {
		t.Fatal("disconnected read", e)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM gmail_assistance`).Scan(&n); e != nil || n != 0 {
		t.Fatal("disconnect retained results", n, e)
	}
	connect()
	message()
	request("summary")
	exec(`UPDATE gmail_messages SET received_at=now()-interval '31 days'`)
	if worked, e := s.processAssistance(ctx); worked || e != nil {
		t.Fatal("expired source", worked, e)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM gmail_assistance`).Scan(&n); e != nil || n != 0 {
		t.Fatal("retention", n, e)
	}
}
