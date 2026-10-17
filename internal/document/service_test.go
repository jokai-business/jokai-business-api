package document

import (
	"ai-business-api/internal/user"
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
)

func TestValidate(t *testing.T) {
	pdf := []byte("%PDF-1.4\nexample\n%%EOF")
	base := Input{Title: " Contract ", Workspace: "corporate", Filename: "Contract.pdf"}
	mime, err := Validate(&base, pdf)
	if err != nil || mime != "application/pdf" || base.Title != "Contract" {
		t.Fatal(mime, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Input)
		data   []byte
	}{
		{"path", func(i *Input) { i.Filename = "../a.pdf" }, pdf},
		{"header", func(i *Input) { i.Filename = "a\r\n.pdf" }, pdf},
		{"empty title", func(i *Input) { i.Title = " " }, pdf},
		{"unknown workspace", func(i *Input) { i.Workspace = "other" }, pdf},
		{"foreign field syntax", func(i *Input) { i.CustomerID = "not-uuid" }, pdf},
		{"mismatch", func(i *Input) { i.Filename = "a.png" }, pdf},
		{"fake pdf", func(i *Input) {}, []byte("<html>not a document</html>")},
		{"truncated pdf", func(i *Input) {}, []byte("%PDF-1.4")},
		{"empty", func(i *Input) {}, nil},
		{"oversize", func(i *Input) {}, make([]byte, MaxSize+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.change(&in)
			if _, err := Validate(&in, tc.data); err == nil {
				t.Fatal("invalid document accepted")
			}
		})
	}
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 5, 5)))
	base.Filename = "image.png"
	if mime, err = Validate(&base, b.Bytes()); err != nil || mime != "image/png" {
		t.Fatal(mime, err)
	}
}
func TestStorageImmutableAndTenantKeys(t *testing.T) {
	s := LocalStore{Root: t.TempDir()}
	ctx := context.Background()
	org := "10000000-0000-0000-0000-000000000001"
	other := "10000000-0000-0000-0000-000000000002"
	id := "20000000-0000-0000-0000-000000000001"
	if err := s.Put(ctx, org, id, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, org, id, []byte("replacement")); err == nil {
		t.Fatal("overwrite allowed")
	}
	if _, err := s.Get(ctx, other, id); err == nil {
		t.Fatal("cross tenant read")
	}
	if _, err := s.Get(ctx, org, "../secret"); err == nil {
		t.Fatal("unsafe key")
	}
	data, err := s.Get(ctx, org, id)
	if err != nil || string(data) != "original" {
		t.Fatal("original changed")
	}
}
func TestPermissionsBeforeStorage(t *testing.T) {
	s := Service{}
	ctx := context.Background()
	a := user.Context{Permissions: []string{"document.read", "document.upload", "document.sales"}}
	if _, err := s.Upload(ctx, a, Input{Workspace: "corporate"}, nil, ""); !errors.Is(err, user.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.Review(ctx, a, "", Review{}, ""); !errors.Is(err, user.ErrForbidden) {
		t.Fatal(err)
	}
	if got := Workspaces(a); len(got) != 1 || got[0] != "sales" {
		t.Fatal(got)
	}
	if _, err := s.Get(ctx, user.Context{}, ""); !errors.Is(err, user.ErrForbidden) {
		t.Fatal(err)
	}
}
