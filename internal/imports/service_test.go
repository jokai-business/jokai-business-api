package imports

import (
	"ai-business-api/internal/user"
	"ai-business-api/migrations"
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func workbook(kind string, rows [][]string, edit func(map[string]string)) []byte {
	b, _ := Template(kind)
	z, _ := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		v, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = string(v)
	}
	extra := ""
	for i, row := range rows {
		extra += fmt.Sprintf(`<row r="%d">`, i+2)
		for c, value := range row {
			var text bytes.Buffer
			xml.EscapeText(&text, []byte(value))
			extra += fmt.Sprintf(`<c r="%c%d" t="inlineStr"><is><t>%s</t></is></c>`, 'A'+c, i+2, text.String())
		}
		extra += `</row>`
	}
	files["xl/worksheets/sheet1.xml"] = strings.Replace(files["xl/worksheets/sheet1.xml"], "</sheetData>", extra+"</sheetData>", 1)
	if edit != nil {
		edit(files)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for n, v := range files {
		f, _ := w.Create(n)
		f.Write([]byte(v))
	}
	w.Close()
	return out.Bytes()
}
func TestWorkbook(t *testing.T) {
	external, e := os.ReadFile("testdata/openpyxl-customer.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	externalRows, e := Parse(external, "customer")
	if e != nil || externalRows[0][0] != "ក្រុមហ៊ុន Excel" || externalRows[0][2] != "00123" {
		t.Fatal("independent XLSX writer", externalRows, e)
	}

	rows := [][]string{{"ក្រុមហ៊ុន", "a@example.test", "00123", "https://example.test", "Industry"}}
	b := workbook("customer", rows, nil)
	v, e := Parse(b, "customer")
	if e != nil || v[0][0] != rows[0][0] || v[0][2] != "00123" {
		t.Fatal(v, e)
	}
	for _, edit := range []func(map[string]string){
		func(f map[string]string) {
			f["xl/worksheets/sheet1.xml"] = strings.Replace(f["xl/worksheets/sheet1.xml"], `<c r="A2"`, `<c r="Z2"`, 1)
		},
		func(f map[string]string) {
			f["xl/worksheets/sheet1.xml"] = strings.Replace(f["xl/worksheets/sheet1.xml"], `<is><t>ក្រុមហ៊ុន</t></is>`, `<f>1+1</f><v>2</v>`, 1)
		},
		func(f map[string]string) {
			f["xl/worksheets/sheet1.xml"] = strings.Replace(f["xl/worksheets/sheet1.xml"], `<row r="2">`, `<row r="2" hidden="1">`, 1)
		},
		func(f map[string]string) {
			f["xl/worksheets/sheet1.xml"] = strings.Replace(f["xl/worksheets/sheet1.xml"], `</worksheet>`, `<mergeCells><mergeCell ref="A2:B2"/></mergeCells></worksheet>`, 1)
		},
		func(f map[string]string) {
			f["xl/workbook.xml"] = strings.Replace(f["xl/workbook.xml"], `</sheets>`, `<sheet name="Extra" sheetId="2" r:id="rId2"/></sheets>`, 1)
		},
		func(f map[string]string) { f["xl/vbaProject.bin"] = "macro" },
		func(f map[string]string) {
			f["xl/worksheets/sheet1.xml"] = strings.Replace(f["xl/worksheets/sheet1.xml"], `<t>name</t>`, `<t>wrong</t>`, 1)
		},
		func(f map[string]string) {
			f["xl/_rels/workbook.xml.rels"] = strings.Replace(f["xl/_rels/workbook.xml.rels"], `Target="worksheets/sheet1.xml"`, `Target="https://example.test" TargetMode="External"`, 1)
		},
		func(f map[string]string) { f["padding.xml"] = strings.Repeat("x", 8<<20) },
	} {
		if _, e = Parse(workbook("customer", rows, edit), "customer"); e != ErrWorkbook {
			t.Fatal("accepted unsupported workbook", e)
		}
	}
	// Excel shared-string and numeric cells are read without evaluating formulas.
	b = workbook("customer", rows, func(f map[string]string) {
		f["xl/sharedStrings.xml"] = `<sst><si><t>Shared customer</t></si></sst>`
		f["xl/worksheets/sheet1.xml"] = strings.Replace(f["xl/worksheets/sheet1.xml"], `<c r="A2" t="inlineStr"><is><t>ក្រុមហ៊ុន</t></is></c>`, `<c r="A2" t="s"><v>0</v></c>`, 1)
	})
	if v, e = Parse(b, "customer"); e != nil || v[0][0] != "Shared customer" {
		t.Fatal("shared strings", e)
	}
	many := make([][]string, 501)
	for i := range many {
		many[i] = rows[0]
	}
	if _, e = Parse(workbook("customer", many, nil), "customer"); e != ErrWorkbook {
		t.Fatal("row limit")
	}
}
func TestImportIntegration(t *testing.T) {
	dsn := os.Getenv("IMPORT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable IMPORT_TEST_DATABASE_URL")
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
	exec(`INSERT INTO organizations(id,name)VALUES($1,'Import'),($2,'Other')`, org, foreign)
	exec(`INSERT INTO users(id,organization_id,subject,email,name)VALUES($1,$2,'import-one','one@example.test','One'),($3,$2,'import-two','two@example.test','Two')`, uid, org, other)
	exec(`INSERT INTO user_roles VALUES($1,$2,'organization_admin'),($1,$3,'organization_admin')`, org, uid, other)
	a := user.Context{User: user.User{ID: uid}, Organization: user.Organization{ID: org}}
	s := &Service{DB: db}
	preview := func(kind string, rows [][]string) Report {
		t.Helper()
		v, e := s.Preview(ctx, a, kind, workbook(kind, rows, nil))
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	rows := [][]string{{"Import One", "one@customer.test", "00123", "https://example.test", "Tech"}, {"ក្រុមហ៊ុន", "two@customer.test", "", "", ""}}
	originalUpload := workbook("customer", rows, nil)
	v, e := s.Preview(ctx, a, "customer", originalUpload)
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Rows) != 2 || len(v.Rows[0].Errors) > 0 {
		t.Fatal(v)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM customers`).Scan(&count); e != nil || count != 0 {
		t.Fatal("preview wrote records")
	}
	if _, e = s.Confirm(ctx, a, v.ID, false, "test"); e != user.ErrInvalid {
		t.Fatal("consent", e)
	}
	stranger := a
	stranger.User.ID = other
	if _, e = s.Get(ctx, stranger, v.ID); e != ErrNotFound {
		t.Fatal("private preview", e)
	}
	stranger.Organization.ID = foreign
	if _, e = s.Confirm(ctx, stranger, v.ID, true, "test"); e != ErrNotFound {
		t.Fatal("tenant isolation", e)
	}
	var wg sync.WaitGroup
	results := make(chan Report, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { r, e := s.Confirm(ctx, a, v.ID, true, "test"); results <- r; errs <- e })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	one, two := <-results, <-results
	if one.Rows[0].RecordID != two.Rows[0].RecordID {
		t.Fatal("duplicate confirmation")
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM customers`).Scan(&count); e != nil || count != 2 {
		t.Fatal("duplicate rows")
	}
	if _, e = s.Preview(ctx, a, "customer", originalUpload); e != ErrChanged {
		t.Fatal("reuploaded batch", e)
	}
	bad := preview("customer", [][]string{{"New valid", "valid@example.test", "", "", ""}, {"Bad", "not email", "", "", ""}})
	if len(bad.Rows[1].Errors) == 0 {
		t.Fatal("missing row error")
	}
	if _, e = s.Confirm(ctx, a, bad.ID, true, "test"); e != ErrChanged {
		t.Fatal("invalid batch", e)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM customers WHERE name='New valid'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial import")
	}
	dup := preview("customer", [][]string{{"Same", "", "", "", ""}, {"same", "", "", "", ""}, {"Import One", "", "", "", ""}})
	if len(dup.Rows[1].Errors) == 0 || len(dup.Rows[2].Errors) == 0 {
		t.Fatal("duplicates")
	}
	var stageID, stage string
	if e = db.QueryRow(ctx, `SELECT id,name FROM pipeline_stages WHERE organization_id=$1 ORDER BY position LIMIT 1`, org).Scan(&stageID, &stage); e != nil {
		t.Fatal(e)
	}
	opp := preview("opportunity", [][]string{{"Import One", "Deal", "100.25", "USD", stage, "one@example.test"}})
	if len(opp.Rows[0].Errors) > 0 {
		t.Fatal(opp.Rows)
	}
	saved, e := s.Confirm(ctx, a, opp.ID, true, "test")
	if e != nil {
		t.Fatal(e)
	}
	var source, value string
	if e = db.QueryRow(ctx, `SELECT source,expected_value::text FROM opportunities WHERE id=$1`, saved.Rows[0].RecordID).Scan(&source, &value); e != nil || source != "Excel" || value != "100.25" {
		t.Fatal("opportunity defaults", e)
	}
	invalid := preview("opportunity", [][]string{{"Missing", "Bad deal", "1", "USD", stage, ""}})
	if len(invalid.Rows[0].Errors) == 0 {
		t.Fatal("missing reference")
	}
	stale := preview("opportunity", [][]string{{"Import One", "Stale deal", "2", "USD", stage, "two@example.test"}})
	exec(`UPDATE users SET active=false WHERE id=$1`, other)
	if _, e = s.Confirm(ctx, a, stale.ID, true, "test"); e != ErrChanged {
		t.Fatal("inactive owner", e)
	}
	revoke := preview("customer", [][]string{{"Revoked", "", "", "", ""}})
	exec(`DELETE FROM user_roles WHERE organization_id=$1 AND user_id=$2`, org, uid)
	if _, e = s.Confirm(ctx, a, revoke.ID, true, "test"); e != user.ErrForbidden {
		t.Fatal("revocation", e)
	}
	exec(`INSERT INTO user_roles VALUES($1,$2,'organization_admin')`, org, uid)
	exec(`UPDATE excel_imports SET created_at=now()-interval '25 hours' WHERE id=$1`, revoke.ID)
	if _, e = s.Get(ctx, a, revoke.ID); e != ErrNotFound {
		t.Fatal("expired", e)
	}
	// A competing ordinary insert between preview and confirmation invalidates all rows.
	concurrent := preview("customer", [][]string{{"Concurrent", "", "", "", ""}})
	exec(`INSERT INTO customers(organization_id,name,owner_id)VALUES($1,'Concurrent',$2)`, org, uid)
	if _, e = s.Confirm(ctx, a, concurrent.ID, true, "test"); e != ErrChanged {
		t.Fatal("stale duplicate", e)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='opportunity.created' AND resource_id=$1`, saved.Rows[0].RecordID).Scan(&count); e != nil || count != 1 {
		t.Fatal("audit", e)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM opportunity_activities WHERE opportunity_id=$1`, saved.Rows[0].RecordID).Scan(&count); e != nil || count != 1 {
		t.Fatal("activity", e)
	}
}
