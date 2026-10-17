package document

import "testing"

func TestUploadPolicy(t *testing.T) {
	p := UploadPolicy{MaxMiB: 1, AllowPDF: true, Version: 1}
	if p.Check(1<<20, "application/pdf") != nil {
		t.Fatal("exact size should pass")
	}
	for _, c := range []struct {
		size int
		mime string
	}{{(1 << 20) + 1, "application/pdf"}, {1, "image/png"}, {1, "image/jpeg"}, {1, "text/plain"}} {
		if p.Check(c.size, c.mime) == nil {
			t.Fatal("policy accepted", c)
		}
	}
	for _, bad := range []UploadPolicy{{MaxMiB: 0, AllowPDF: true, Version: 1}, {MaxMiB: 11, AllowPDF: true, Version: 1}, {MaxMiB: 1, Version: 1}, {MaxMiB: 1, AllowPDF: true}} {
		if bad.Validate() == nil {
			t.Fatal("invalid policy", bad)
		}
	}
	p.AllowPNG = true
	p.AllowJPEG = true
	for _, mime := range []string{"image/png", "image/jpeg"} {
		if p.Check(100, mime) != nil {
			t.Fatal(mime)
		}
	}
}
