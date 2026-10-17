package document

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

type fakeOCR struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (f *fakeOCR) Extract(context.Context, []byte, string) (OCRResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return OCRResult{}, errors.New("private provider detail")
	}
	return OCRResult{Text: "Invoice 12\nTotal 42.00", Pages: 1}, nil
}
func TestOCRIntegration(t *testing.T) {
	dsn := os.Getenv("DOCUMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable DOCUMENT_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("requires empty schema", err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
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
	otherOrg := "00000000-0000-0000-0000-000000000003"
	exec(`INSERT INTO organizations(id,name) VALUES($1,'OCR'),($2,'Other')`, org, otherOrg)
	exec(`INSERT INTO users(id,organization_id,subject,email,name) VALUES($1,$2,'ocr-test','test@example.test','Reviewer')`, uid, org)
	exec(`INSERT INTO user_roles VALUES($1,$2,'organization_admin')`, org, uid)
	actor, e := (&user.Repository{DB: db}).Resolve(ctx, "ocr-test")
	if e != nil {
		t.Fatal(e)
	}
	engine := &fakeOCR{}
	s := &Service{DB: db, Blobs: LocalStore{Root: t.TempDir()}, OCR: engine}
	newDoc := func() Document {
		t.Helper()
		d, e := s.Upload(ctx, actor, Input{Title: "OCR source", Workspace: "sales", Filename: "invoice.pdf"}, []byte("%PDF-1.4\n%%EOF"), "test")
		if e != nil {
			t.Fatal(e)
		}
		d, e = s.Review(ctx, actor, d.ID, Review{Version: d.Version, Status: "accepted"}, "test")
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	get := func(id string) OCR {
		t.Helper()
		v, e := s.GetOCR(ctx, actor, id)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	process := func() {
		t.Helper()
		if _, e := s.processOCR(ctx); e != nil {
			t.Fatal(e)
		}
	}

	// Language selection is persisted, routed to a separate provider and version guarded.
	khmerDoc := newDoc()
	if e := s.RequestOCRLanguage(ctx, actor, khmerDoc.ID, 0, "km+en", "test"); e != ErrOCRDisabled {
		t.Fatal("unconfigured Khmer accepted", e)
	}
	if e := s.RequestOCRLanguage(ctx, actor, khmerDoc.ID, 0, "invalid", "test"); e != user.ErrInvalid {
		t.Fatal("invalid language accepted", e)
	}
	khmerEngine := &fakeOCR{fail: true}
	s.KhmerOCR = khmerEngine
	if e := s.RequestOCRLanguage(ctx, actor, khmerDoc.ID, 0, "km+en", "test"); e != nil {
		t.Fatal(e)
	}
	process()
	khmerStatus := get(khmerDoc.ID)
	if khmerStatus.Language != "km+en" || khmerStatus.Engine != "Tesseract 5 (Khmer + English)" || khmerStatus.Status != "failed" || khmerEngine.calls != 1 || engine.calls != 0 {
		t.Fatal("wrong OCR routing", khmerStatus)
	}
	if e := s.RequestOCRLanguage(ctx, actor, khmerDoc.ID, 0, "en", "test"); e != ErrConflict {
		t.Fatal("stale language retry", e)
	}
	if e := s.RequestOCRLanguage(ctx, actor, khmerDoc.ID, khmerStatus.Version, "en", "test"); e != nil {
		t.Fatal(e)
	}
	process()
	if v := get(khmerDoc.ID); v.Language != "en" || v.Engine != "PaddleOCR" || v.Status != "ready" {
		t.Fatal("retry language not stored", v)
	}
	engine.calls = 0
	s.KhmerOCR = nil
	d := newDoc()
	v := get(d.ID)
	if v.Status != "not_requested" || !v.Available {
		t.Fatal(v)
	}
	foreign := actor
	foreign.Organization.ID = otherOrg
	if _, e = s.GetOCR(ctx, foreign, d.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("tenant leak", e)
	}
	denied := actor
	denied.Permissions = []string{"document.read", "document.sales"}
	if e = s.RequestOCR(ctx, denied, d.ID, 0, "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	hidden := actor
	hidden.Permissions = []string{"document.read", "document.review", "document.pmo"}
	if _, e = s.GetOCR(ctx, hidden, d.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("team leak", e)
	}
	s.OCR = nil
	if e = s.RequestOCR(ctx, actor, d.ID, 0, "test"); !errors.Is(e, ErrOCRDisabled) {
		t.Fatal(e)
	}
	s.OCR = engine
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.RequestOCR(ctx, actor, d.ID, 0, "test") }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("duplicate requests", success)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.processOCR(ctx); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	v = get(d.ID)
	if v.Status != "ready" || v.RawText == "" || v.SourceSHA256 != d.SHA256 || engine.calls != 1 {
		t.Fatal(v, engine.calls)
	}
	review := OCRReview{Version: v.Version, Status: "approved", Text: "Invoice 12\nTotal 43.00", Confirmed: true}
	noConfirm := review
	noConfirm.Confirmed = false
	if e = s.ReviewOCR(ctx, actor, d.ID, noConfirm, "test"); !errors.Is(e, user.ErrInvalid) {
		t.Fatal(e)
	}
	if e = s.ReviewOCR(ctx, denied, d.ID, review, "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if e = s.ReviewOCR(ctx, actor, d.ID, review, "test"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReviewOCR(ctx, actor, d.ID, review, "test"); !errors.Is(e, ErrConflict) {
		t.Fatal("stale review", e)
	}
	v = get(d.ID)
	if v.Status != "approved" || v.RawText == v.ReviewedText || v.ReviewerName != "Reviewer" {
		t.Fatal(v)
	}
	if e = s.RequestOCR(ctx, actor, d.ID, v.Version, "test"); !errors.Is(e, ErrConflict) {
		t.Fatal("overwrote approved text", e)
	}
	failed := newDoc()
	engine.fail = true
	for i := 0; i < 3; i++ {
		v = get(failed.ID)
		if e = s.RequestOCR(ctx, actor, failed.ID, v.Version, "test"); e != nil {
			t.Fatal(e)
		}
		process()
		v = get(failed.ID)
		if v.Status != "failed" || v.Attempts != i+1 || v.RawText != "" {
			t.Fatal(v)
		}
	}
	if e = s.RequestOCR(ctx, actor, failed.ID, v.Version, "test"); !errors.Is(e, ErrConflict) {
		t.Fatal("unbounded retry", e)
	}
	engine.fail = false
	revoked := newDoc()
	if e = s.RequestOCR(ctx, actor, revoked.ID, 0, "test"); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE users SET active=false WHERE id=$1`, uid)
	calls := engine.calls
	process()
	if engine.calls != calls || get(revoked.ID).Status != "failed" {
		t.Fatal("inactive requester processed")
	}
	exec(`UPDATE users SET active=true WHERE id=$1`, uid)
	interrupted := newDoc()
	if e = s.RequestOCR(ctx, actor, interrupted.ID, 0, "test"); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE document_ocr SET status='processing',attempts=1,lease_until=now()-interval '1 second' WHERE document_id=$1`, interrupted.ID)
	process()
	v = get(interrupted.ID)
	if v.Status != "failed" {
		t.Fatal("lease not recovered", v)
	}
	if e = s.RequestOCR(ctx, actor, interrupted.ID, v.Version, "test"); e != nil {
		t.Fatal(e)
	}
	process()
	v = get(interrupted.ID)
	rejection := OCRReview{Version: v.Version, Status: "rejected", Confirmed: true}
	if e = s.ReviewOCR(ctx, actor, interrupted.ID, rejection, "test"); !errors.Is(e, user.ErrInvalid) {
		t.Fatal("missing note", e)
	}
	rejection.Note = "Unreadable amounts"
	if e = s.ReviewOCR(ctx, actor, interrupted.ID, rejection, "test"); e != nil {
		t.Fatal(e)
	}
	corrupt := newDoc()
	if e = s.RequestOCR(ctx, actor, corrupt.ID, 0, "test"); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE documents SET sha256='wrong' WHERE id=$1`, corrupt.ID)
	calls = engine.calls
	process()
	if engine.calls != calls || get(corrupt.ID).Status != "failed" {
		t.Fatal("unverified source sent")
	}
	var leaked int
	exec(`SELECT 1`)
	if e = db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE after_value::text LIKE '%Invoice 12%'`).Scan(&leaked); e != nil || leaked != 0 {
		t.Fatal("private text in audit", e)
	}
	testInvoiceCapture(t, s, actor, d.ID, newDoc().ID)
}
