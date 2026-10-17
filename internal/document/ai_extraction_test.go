package document

import (
	"ai-business-api/internal/ai"
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

func extractionFixture() string {
	f := SuggestInvoice(labelledInvoice)
	b, _ := json.Marshal(ExtractionSuggestion{Fields: f, Evidence: f})
	return string(b)
}
func TestExtractionValidation(t *testing.T) {
	valid := extractionFixture()
	if _, e := parseExtraction(valid, labelledInvoice); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{valid + " {}", "```json\n" + valid + "\n```", strings.Replace(valid, `"fields":`, `"extra":{},"fields":`, 1), strings.Replace(valid, `"vendor":"Example Supplies"`, `"vendor":null`, 1), strings.Replace(valid, `"vendor":"Example Supplies"`, `"vendor":"A","vendor":"Example Supplies"`, 1), strings.Replace(valid, `"total":"110.00"`, `"total":"111.00"`, 1), strings.Replace(valid, `"currency":"USD"`, `"currency":"XYZ"`, 1), strings.Replace(valid, `"dueDate":"2026-11-15"`, `"dueDate":"2026-01-01"`, 1), strings.ReplaceAll(valid, "Example Supplies", "Invented Evidence"), strings.Repeat("x", 8001)} {
		if _, e := parseExtraction(raw, labelledInvoice); e == nil {
			t.Fatal("invalid provider response accepted", raw)
		}
	}
	partial := ExtractionSuggestion{Fields: InvoiceFields{Vendor: "ក្រុមហ៊ុន"}, Evidence: InvoiceFields{Vendor: "ក្រុមហ៊ុន"}}
	b, _ := json.Marshal(partial)
	if _, e := parseExtraction(string(b), "Vendor: ក្រុមហ៊ុន"); e != nil {
		t.Fatal("Khmer partial", e)
	}
}

type extractionLLM struct {
	calls         int
	output, input string
	callback      func()
}

func (f *extractionLLM) Generate(_ context.Context, r ai.Request) (string, error) {
	f.calls++
	f.input = r.Input
	if f.callback != nil {
		f.callback()
	}
	return f.output, nil
}
func TestExtractionIntegration(t *testing.T) {
	dsn := os.Getenv("EXTRACTION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable EXTRACTION_TEST_DATABASE_URL")
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
	exec(`INSERT INTO organizations(id,name)VALUES($1,'Extraction'),($2,'Foreign')`, org, foreign)
	exec(`INSERT INTO users(id,organization_id,subject,email,name)VALUES($1,$2,'extract-one','one@example.test','One'),($3,$2,'extract-two','two@example.test','Two')`, uid, org, other)
	exec(`INSERT INTO roles(key,name)VALUES('extract_reviewer','Extraction reviewer')`)
	for _, perm := range []string{"document.read", "document.review", "document.upload", "document.sales"} {
		exec(`INSERT INTO role_permissions VALUES('extract_reviewer',$1)`, perm)
	}
	exec(`INSERT INTO user_roles VALUES($1,$2,'extract_reviewer'),($1,$3,'extract_reviewer')`, org, uid, other)
	a, e := (&user.Repository{DB: db}).Resolve(ctx, "extract-one")
	if e != nil {
		t.Fatal(e)
	}
	llm := &extractionLLM{output: extractionFixture()}
	s := &Service{DB: db, Blobs: LocalStore{Root: t.TempDir()}, LLM: llm, AIProvider: "Fixture", AIModel: "fixture-model"}
	newDoc := func(approved bool) string {
		t.Helper()
		d, e := s.Upload(ctx, a, Input{Title: "Private source", Workspace: "sales", Filename: "invoice.pdf"}, []byte("%PDF-1.4\n%%EOF"), "test")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Review(ctx, a, d.ID, Review{Version: d.Version, Status: "accepted"}, "test"); e != nil {
			t.Fatal(e)
		}
		if approved {
			exec(`INSERT INTO document_ocr(organization_id,document_id,status,requested_by,source_sha256,reviewed_text,reviewed_by,reviewed_at)VALUES($1,$2,'approved',$3,$4,$5,$3,now())`, org, d.ID, uid, d.SHA256, labelledInvoice)
		}
		return d.ID
	}
	id := newDoc(true)
	pending := newDoc(false)
	get := func() ExtractionView {
		t.Helper()
		v, e := s.GetExtraction(ctx, a, id)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	input := func() ExtractionInput {
		v := get()
		n := 0
		if v.Result != nil {
			n = v.Result.Version
		}
		return ExtractionInput{Version: n, SourceHash: v.SourceHash, Provider: s.AIProvider, Model: s.AIModel, Confirmed: true}
	}
	request := func() {
		t.Helper()
		if e := s.RequestExtraction(ctx, a, id, input(), "test"); e != nil {
			t.Fatal(e)
		}
	}
	work := func() {
		t.Helper()
		worked, e := s.processExtraction(ctx)
		if !worked || e != nil {
			t.Fatal(worked, e)
		}
	}
	if _, e = s.GetExtraction(ctx, a, pending); e != ErrExtractionChanged {
		t.Fatal("unapproved source", e)
	}
	stranger := a
	stranger.Organization.ID = foreign
	if _, e = s.GetExtraction(ctx, stranger, id); e != user.ErrForbidden {
		t.Fatal("tenant", e)
	}
	in := input()
	in.Confirmed = false
	if e = s.RequestExtraction(ctx, a, id, in, "test"); e != user.ErrInvalid {
		t.Fatal("consent", e)
	}
	in = input()
	in.Provider = "Changed"
	if e = s.RequestExtraction(ctx, a, id, in, "test"); e != ErrExtractionChanged {
		t.Fatal("provider", e)
	}
	in = input()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.RequestExtraction(ctx, a, id, in, "test") })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if e != ErrExtractionChanged {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("duplicate", success)
	}
	work()
	v := get()
	if v.Result.Status != "ready" || llm.calls != 1 || !strings.Contains(llm.input, "Example Supplies") {
		t.Fatal("generation", v)
	}
	stranger = a
	stranger.User.ID = other
	if v, e := s.GetExtraction(ctx, stranger, id); e != nil || v.Result != nil {
		t.Fatal("private results", e)
	}
	cap, e := s.GetInvoiceCapture(ctx, a, id)
	if e != nil || cap.Status != "suggested" {
		t.Fatal("automatic draft", e)
	}
	fields := SuggestInvoice(labelledInvoice)
	fields.Vendor = "Reviewed Vendor"
	save := CaptureInput{SourceOCRVersion: cap.SourceOCRVersion, Fields: fields, AIVersion: v.Result.Version}
	bad := save
	bad.AIVersion++
	if e = s.SaveInvoiceCapture(ctx, a, id, bad, "test"); e != ErrExtractionChanged {
		t.Fatal("stale apply", e)
	}
	if e = s.SaveInvoiceCapture(ctx, a, id, save, "test"); e != nil {
		t.Fatal(e)
	}
	cap, e = s.GetInvoiceCapture(ctx, a, id)
	if e != nil || cap.AIProvenance == nil || cap.Suggestions.Vendor != "Example Supplies" || cap.Fields.Vendor != "Reviewed Vendor" {
		t.Fatal("provenance lost", cap, e)
	}
	save.Version = cap.Version
	save.AIVersion = 0
	save.Fields.Vendor = "Manual correction"
	if e = s.SaveInvoiceCapture(ctx, a, id, save, "test"); e != nil {
		t.Fatal(e)
	}
	cap, _ = s.GetInvoiceCapture(ctx, a, id)
	if cap.AIProvenance == nil || cap.Suggestions.Vendor != "Example Supplies" {
		t.Fatal("manual edit lost originals")
	}

	// A reviewer may explicitly apply AI to an already saved manual draft.
	save.Version = cap.Version
	save.AIVersion = v.Result.Version
	if e = s.SaveInvoiceCapture(ctx, a, id, save, "test"); e != nil {
		t.Fatal("apply to existing draft", e)
	}
	cap, _ = s.GetInvoiceCapture(ctx, a, id)
	if cap.AIProvenance == nil || cap.Suggestions.Vendor != "Example Supplies" || cap.Fields.Vendor != "Manual correction" {
		t.Fatal("updated draft provenance")
	}
	if e = s.RecordInvoiceCapture(ctx, a, id, cap.Version, true, "test"); e != nil {
		t.Fatal(e)
	}
	save.Version = cap.Version + 1
	save.AIVersion = v.Result.Version
	if e = s.SaveInvoiceCapture(ctx, a, id, save, "test"); e != ErrCaptureConflict {
		t.Fatal("record overwritten", e)
	}
	exec(`DELETE FROM role_permissions WHERE role_key='extract_reviewer' AND permission_key='document.sales'`)
	if _, e = s.GetExtraction(ctx, a, id); e != ErrNotFound {
		t.Fatal("team permission reload", e)
	}
	exec(`INSERT INTO role_permissions VALUES('extract_reviewer','document.sales')`)
	exec(`UPDATE document_ocr SET reviewed_text=reviewed_text||' changed',version=version+1 WHERE document_id=$1`, id)
	if v = get(); !v.Result.Stale || v.Result.Output != "" {
		t.Fatal("stale output")
	}
	request()
	exec(`UPDATE users SET active=false WHERE id=$1`, uid)
	work()
	if llm.calls != 1 {
		t.Fatal("inactive request dispatched")
	}
	exec(`UPDATE users SET active=true WHERE id=$1`, uid)
	request()
	llm.callback = func() { exec(`UPDATE document_ocr SET version=version+1 WHERE document_id=$1`, id) }
	work()
	llm.callback = nil
	if get().Result.Status != "failed" {
		t.Fatal("source mutation published")
	}
	request()
	llm.output = `{"fields":{}}`
	work()
	if get().Result.Status != "failed" {
		t.Fatal("invalid response published")
	}
	calls := llm.calls
	if worked, e := s.processExtraction(ctx); e != nil || worked || llm.calls != calls {
		t.Fatal("auto retry", e)
	}
	request()
	exec(`UPDATE document_ai_extractions SET status='processing',lease_until=now()-interval '1 second'`)
	if worked, e := s.processExtraction(ctx); e != nil || worked {
		t.Fatal(e)
	}
	if get().Result.Status != "failed" {
		t.Fatal("lease")
	}
	llm.output = extractionFixture()
	request()
	llm.callback = func() {
		exec(`DELETE FROM document_ai_extractions`)
		request()
		exec(`UPDATE document_ai_extractions SET status='processing',lease_until=now()+interval '90 seconds'`)
	}
	work()
	llm.callback = nil
	if get().Result.Status != "processing" {
		t.Fatal("delayed job replaced current job")
	}
	exec(`UPDATE document_ai_extractions SET status='failed'`)
	exec(`INSERT INTO audit_logs(organization_id,actor_id,resource_id,action,request_id)SELECT $1,$2,$3,'document.ai_requested','rate' FROM generate_series(1,20)`, org, uid, id)
	if e = s.RequestExtraction(ctx, a, id, input(), "test"); e != ErrExtractionLimit {
		t.Fatal("rate", e)
	}
	var leaked bool
	if e = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_logs WHERE row_to_json(audit_logs)::text LIKE '%Example Supplies%' OR row_to_json(audit_logs)::text LIKE '%TEST-2042%')`).Scan(&leaked); e != nil || leaked {
		t.Fatal("audit leak", e)
	}
	exec(`DELETE FROM audit_logs WHERE request_id='rate'`)
	request()
	work()
	s.LLM = nil
	if get().Result.Output == "" {
		t.Fatal("disabled hides saved output")
	}
	if e = s.RequestExtraction(ctx, a, id, input(), "test"); e != ErrExtractionDisabled {
		t.Fatal("disabled", e)
	}
	exec(`UPDATE document_ai_extractions SET requested_at=now()-interval '31 days'`)
	if _, e = s.processExtraction(ctx); e != nil {
		t.Fatal(e)
	}
	if get().Result != nil {
		t.Fatal("retention")
	}
	var invoices int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM invoices`).Scan(&invoices); e != nil || invoices != 0 {
		t.Fatal("financial side effect", e)
	}
}
