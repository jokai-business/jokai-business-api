package businessai

import (
	"ai-business-api/internal/user"
	"ai-business-api/migrations"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestQuestionIntegration(t *testing.T) {
	dsn := os.Getenv("QUESTION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable QUESTION_TEST_DATABASE_URL")
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
	view := func() QuestionView {
		t.Helper()
		v, e := s.GetQuestion(ctx, a, "customer", cid)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	input := func() QuestionInput {
		v := view()
		version := 0
		if v.Result != nil {
			version = v.Result.Version
		}
		return QuestionInput{Input: Input{Version: version, SourceHash: v.SourceHash, Provider: s.Provider, Model: s.Model, Confirmed: true}, Question: "PRIVATE_QUESTION: តើខ្ញុំគួរតាមដានអ្វី?"}
	}
	request := func() {
		t.Helper()
		if e := s.RequestQuestion(ctx, a, "customer", cid, input(), "test"); e != nil {
			t.Fatal(e)
		}
	}
	work := func() {
		t.Helper()
		worked, e := s.processQuestion(ctx)
		if e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	bad := input()
	for _, q := range []string{"", strings.Repeat("ក", 501), "bad\x00text"} {
		bad.Question = q
		if e := s.RequestQuestion(ctx, a, "customer", cid, bad, "test"); e != user.ErrInvalid {
			t.Fatal("question validation", e)
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
	if _, e := s.GetQuestion(ctx, a, "opportunity", oid); e != user.ErrForbidden {
		t.Fatal("opportunity permission", e)
	}
	foreignActor := a
	foreignActor.Organization.ID = foreign
	if _, e := s.GetQuestion(ctx, foreignActor, "customer", cid); e != user.ErrForbidden {
		t.Fatal("cross tenant", e)
	}
	if _, e := s.GetQuestion(ctx, a, "customer", qid); e != ErrNotFound {
		t.Fatal("unknown source", e)
	}
	in := input()
	in.Confirmed = false
	if e := s.RequestQuestion(ctx, a, "customer", cid, in, "test"); e != user.ErrInvalid {
		t.Fatal("consent", e)
	}
	in = input()
	in.Provider = "different"
	if e := s.RequestQuestion(ctx, a, "customer", cid, in, "test"); e != ErrChanged {
		t.Fatal("provider consent", e)
	}
	in = input()
	exec(`UPDATE customers SET notes='changed' WHERE id=$1`, cid)
	if e := s.RequestQuestion(ctx, a, "customer", cid, in, "test"); e != ErrChanged {
		t.Fatal("stale preview", e)
	}
	in = input()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.RequestQuestion(ctx, a, "customer", cid, in, "test") })
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
	if !strings.Contains(f.input, "PRIVATE_QUESTION") {
		t.Fatal("question missing from provider request")
	}
	if strings.Contains(f.input, "OTHER_MAILBOX_SECRET") || strings.Contains(f.input, "SECRET_INVOICE") {
		t.Fatal("provider data leak")
	}

	// A new independent question replaces the result, but replaying the prior
	// version never starts another provider call.
	next := input()
	next.Question = "PRIVATE_QUESTION: What should happen next?"
	if e := s.RequestQuestion(ctx, a, "customer", cid, next, "test"); e != nil {
		t.Fatal(e)
	}
	if e := s.RequestQuestion(ctx, a, "customer", cid, next, "test"); e != ErrChanged {
		t.Fatal("duplicate next question", e)
	}
	work()
	if view().Result.Question != next.Question || f.calls != 2 {
		t.Fatal("replacement question")
	}
	otherActor := a
	otherActor.User.ID = other
	ov, e := s.GetQuestion(ctx, otherActor, "customer", cid)
	if e != nil || ov.Result != nil {
		t.Fatal("shared result", e)
	}
	s.LLM = nil
	if !strings.Contains(view().Result.Output, "PRIVATE_RESULT") {
		t.Fatal("disabled hides saved result")
	}
	if e := s.RequestQuestion(ctx, a, "customer", cid, input(), "test"); e != ErrDisabled {
		t.Fatal(e)
	}
	s.LLM = f
	// Adding permissions changes context and invalidates previous output.
	exec(`INSERT INTO role_permissions VALUES('summary_reader','opportunity.read'),('summary_reader','quote.read'),('summary_reader','invoice.read'),('summary_reader','project.read')`)
	v = view()
	if !v.Result.Stale || v.Result.Output != "" || v.Result.Question != "" {
		t.Fatal("stale disclosure")
	}
	b, _ = json.Marshal(v.Context)
	if !strings.Contains(string(b), "SECRET_INVOICE") || !strings.Contains(string(b), "SECRET_QUOTE") {
		t.Fatal("permitted context missing")
	}
	exec(`DELETE FROM role_permissions WHERE role_key='summary_reader' AND permission_key='customer.read'`)
	if _, e := s.GetQuestion(ctx, a, "customer", cid); e != user.ErrForbidden {
		t.Fatal("revoked root access", e)
	}
	exec(`INSERT INTO role_permissions VALUES('summary_reader','customer.read')`)
	op, e := s.GetQuestion(ctx, a, "opportunity", oid)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = json.Marshal(op.Context)
	if strings.Contains(string(b), "PRIVATE_EMAIL") {
		t.Fatal("unrelated email in opportunity")
	}
	if e := s.RequestQuestion(ctx, a, "opportunity", oid, QuestionInput{Input: Input{SourceHash: op.SourceHash, Provider: s.Provider, Model: s.Model, Confirmed: true}, Question: "What is next?"}, "test"); e != nil {
		t.Fatal(e)
	}
	if e := s.RequestQuestion(ctx, a, "customer", cid, input(), "test"); e != ErrLimit {
		t.Fatal("pending across kinds", e)
	}
	work()
	op, e = s.GetQuestion(ctx, a, "opportunity", oid)
	if e != nil || op.Result == nil || op.Result.Status != "ready" {
		t.Fatal("opportunity generation", op, e)
	}
	exec(`UPDATE opportunities SET next_action='Changed follow up' WHERE id=$1`, oid)
	op, e = s.GetQuestion(ctx, a, "opportunity", oid)
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
	if worked, e := s.processQuestion(ctx); worked || e != nil || f.calls != calls {
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
	exec(`UPDATE business_questions SET status='processing',lease_until=now()-interval '1 second' WHERE kind='customer'`)
	if worked, e := s.processQuestion(ctx); worked || e != nil {
		t.Fatal(worked, e)
	}
	if view().Result.Status != "failed" {
		t.Fatal("expired lease")
	}
	request()
	f.callback = func() {
		exec(`DELETE FROM business_questions`)
		request()
		exec(`UPDATE business_questions SET status='processing',lease_until=now()+interval '90 seconds'`)
	}
	work()
	f.callback = nil
	if view().Result.Status != "processing" {
		t.Fatal("old worker replaced new job")
	}
	exec(`UPDATE business_questions SET status='failed',lease_until=NULL`)
	exec(`INSERT INTO audit_logs(organization_id,actor_id,resource_id,action,request_id)SELECT $1,$2,$3,'ai.question_requested','rate' FROM generate_series(1,20)`, org, uid, cid)
	if e := s.RequestQuestion(ctx, a, "customer", cid, input(), "test"); e != ErrLimit {
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
	exec(`UPDATE business_questions SET requested_at=now()-interval '31 days',conversation_started_at=now()-interval '31 days'`)
	if worked, e := s.processQuestion(ctx); worked || e != nil {
		t.Fatal(worked, e)
	}
	if view().Result != nil {
		t.Fatal("retention")
	}
	t.Run("organization questions", func(t *testing.T) {
		exec(`DELETE FROM audit_logs WHERE action='ai.question_requested'`)
		exec(`DELETE FROM role_permissions WHERE role_key='summary_reader'`)
		if _, err := s.GetQuestion(ctx, a, "organization", org); err != user.ErrForbidden {
			t.Fatal("missing read permission", err)
		}
		exec(`INSERT INTO role_permissions VALUES('summary_reader','customer.read')`)
		exec(`INSERT INTO customers(organization_id,name,owner_id,notes)SELECT $1,'Org customer '||n,$2,'EXCLUDED_NOTE' FROM generate_series(1,12)n`, org, uid)
		v, err := s.GetQuestion(ctx, a, "organization", org)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Context.Sections) != 1 || v.Context.Sections[0].Total != 13 || len(v.Context.Sections[0].Records) != 10 {
			t.Fatal("bounds", v.Context)
		}
		raw, _ := json.Marshal(v.Context)
		if strings.Contains(string(raw), "EXCLUDED_NOTE") || strings.Contains(string(raw), "SECRET_INVOICE") || strings.Contains(string(raw), "PRIVATE_EMAIL") {
			t.Fatal("restricted fields")
		}
		if _, err := s.GetQuestion(ctx, a, "organization", foreign); err != ErrNotFound {
			t.Fatal("foreign source", err)
		}
		if _, err := s.Get(ctx, a, "organization", org); err != user.ErrInvalid {
			t.Fatal("summary route expanded", err)
		}
		exec(`INSERT INTO role_permissions VALUES('summary_reader','opportunity.read'),('summary_reader','project.read'),('summary_reader','invoice.read'),('summary_reader','payment.read')`)
		exec(`INSERT INTO opportunities(organization_id,customer_id,title,stage_id,expected_value,currency,probability,owner_id,source)SELECT $1,$2,'KHR Deal',id,400000,'KHR',50,$3,'Manual' FROM pipeline_stages WHERE organization_id=$1 ORDER BY position LIMIT 1`, org, cid, uid)
		v, err = s.GetQuestion(ctx, a, "organization", org)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Context.Sections) != 6 || v.Context.Sections[1].Total != 2 {
			t.Fatal("currency groups", v.Context)
		}
		ask := func() {
			t.Helper()
			v, e := s.GetQuestion(ctx, a, "organization", org)
			if e != nil {
				t.Fatal(e)
			}
			version := 0
			if v.Result != nil {
				version = v.Result.Version
			}
			in := QuestionInput{Input: Input{Version: version, SourceHash: v.SourceHash, Provider: s.Provider, Model: s.Model, Confirmed: true}, Question: "What needs follow-up across our organization?"}
			if e = s.RequestQuestion(ctx, a, "organization", org, in, "org-test"); e != nil {
				t.Fatal(e)
			}
		}
		ask()
		work()
		v, err = s.GetQuestion(ctx, a, "organization", org)
		if err != nil || v.Result == nil || v.Result.Status != "ready" || v.Result.PromptVersion != "organization_question_v1" {
			t.Fatal("org worker", v, err)
		}
		if strings.Contains(f.input, "EXCLUDED_NOTE") || !strings.Contains(f.input, "KHR") {
			t.Fatal("provider context", f.input)
		}
		second := a
		second.User.ID = other
		private, err := s.GetQuestion(ctx, second, "organization", org)
		if err != nil || private.Result != nil {
			t.Fatal("private answer", err)
		}
		exec(`UPDATE customers SET name=name||' changed' WHERE organization_id=$1`, org)
		v, err = s.GetQuestion(ctx, a, "organization", org)
		if err != nil || !v.Result.Stale || v.Result.Output != "" {
			t.Fatal("stale organization result", err)
		}
		ask()
		f.callback = func() {
			exec(`DELETE FROM role_permissions WHERE role_key='summary_reader' AND permission_key='payment.read'`)
		}
		work()
		f.callback = nil
		v, err = s.GetQuestion(ctx, a, "organization", org)
		if err != nil || v.Result.Status != "failed" {
			t.Fatal("permission changed during org generation", err)
		}
	})

	for _, scope := range []struct{ kind, id string }{{"customer", cid}, {"opportunity", oid}, {"organization", org}} {
		t.Run("conversation "+scope.kind, func(t *testing.T) {
			exec(`DELETE FROM business_questions`)
			exec(`DELETE FROM audit_logs WHERE action='ai.question_requested'`)
			preview := func() QuestionView {
				t.Helper()
				v, e := s.GetQuestion(ctx, a, scope.kind, scope.id)
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			makeInput := func(follow bool) QuestionInput {
				v := preview()
				version := 0
				if v.Result != nil {
					version = v.Result.Version
				}
				return QuestionInput{Input: Input{Version: version, SourceHash: v.SourceHash, Provider: s.Provider, Model: s.Model, Confirmed: true}, Question: "CONVERSATION_QUESTION", FollowUp: follow}
			}
			ask := func(follow bool) error {
				return s.RequestQuestion(ctx, a, scope.kind, scope.id, makeInput(follow), "conversation-test")
			}
			if e := ask(true); e != ErrChanged {
				t.Fatal("follow-up without result", e)
			}
			if e := ask(false); e != nil {
				t.Fatal(e)
			}
			work()
			start := preview().Result.ConversationStartedAt
			for n := 1; n <= 4; n++ {
				if e := ask(true); e != nil {
					t.Fatal(e)
				}
				if e := ask(true); e != ErrChanged {
					t.Fatal("pending follow-up", e)
				}
				work()
				v := preview()
				if len(v.Result.History) != n || v.Result.Status != "ready" || v.Result.PromptVersion != "business_conversation_v1" || !v.Result.ConversationStartedAt.Equal(start) {
					t.Fatal("conversation lost", v)
				}
				if !strings.Contains(f.input, `"history"`) || !strings.Contains(f.input, "PRIVATE_RESULT") {
					t.Fatal("history missing at provider")
				}
			}
			if e := ask(true); e != ErrChanged {
				t.Fatal("turn cap", e)
			}
			if e := ask(false); e != nil {
				t.Fatal(e)
			}
			work()
			if len(preview().Result.History) != 0 || strings.Contains(f.input, `"history"`) {
				t.Fatal("independent request retained history")
			}
			previousModel := s.Model
			s.Model = "changed-model"
			if e := ask(true); e != ErrChanged {
				t.Fatal("provider change", e)
			}
			s.Model = previousModel
			// Oversized escaped conversation is rejected before queueing or losing the prior result.
			huge, _ := json.Marshal([]map[string]string{{"question": "q", "answer": strings.Repeat("\"", 8000)}, {"question": "q", "answer": strings.Repeat("\"", 8000)}})
			exec(`UPDATE business_questions SET history=$1,output=$2`, huge, strings.Repeat("\"", 8000))
			if e := ask(true); e != ErrContext {
				t.Fatal("input size limit", e)
			}
			if preview().Result.Status != "ready" {
				t.Fatal("oversize replaced result")
			}
			if e := ask(false); e != nil {
				t.Fatal(e)
			}
			work()
			if e := ask(true); e != nil {
				t.Fatal(e)
			}
			work()
			exec(`UPDATE customers SET name=name||' newer' WHERE id=$1`, cid)
			// Organization lists can omit this older customer, so change all names for that scope.
			if scope.kind == "organization" {
				exec(`UPDATE customers SET name=name||' newer' WHERE organization_id=$1`, org)
			}
			v := preview()
			if !v.Result.Stale || len(v.Result.History) != 0 || v.Result.Question != "" || v.Result.Output != "" {
				t.Fatal("stale history leak", v)
			}
			if e := ask(true); e != ErrChanged {
				t.Fatal("stale follow-up accepted", e)
			}
			exec(`UPDATE business_questions SET conversation_started_at=now()-interval '31 days',requested_at=now()`)
			if preview().Result != nil {
				t.Fatal("follow-ups extended retention")
			}
			if worked, e := s.processQuestion(ctx); worked || e != nil {
				t.Fatal("expired conversation cleanup", worked, e)
			}
		})
	}

}
