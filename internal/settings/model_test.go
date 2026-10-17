package settings

import (
	"ai-business-api/internal/user"
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func validInput() Input {
	return Input{Name: "Company", Timezone: "Asia/Phnom_Penh", DefaultCurrency: "USD", Version: 1}
}
func TestValidateProfile(t *testing.T) {
	good := validInput()
	good.Name = " Company "
	good.DefaultCurrency = " khr "
	if e := Validate(&good); e != nil || good.Name != "Company" || good.DefaultCurrency != "KHR" {
		t.Fatalf("normalization: %+v %v", good, e)
	}
	for _, tc := range []struct {
		name   string
		change func(*Input)
	}{
		{"blank name", func(v *Input) { v.Name = " " }}, {"long name", func(v *Input) { v.Name = strings.Repeat("x", 201) }},
		{"header controls", func(v *Input) { v.Name = "A\nB" }}, {"email", func(v *Input) { v.ContactEmail = "Name <a@example.test>" }},
		{"timezone", func(v *Input) { v.Timezone = "Unknown/Zone" }}, {"machine timezone", func(v *Input) { v.Timezone = "Local" }},
		{"currency", func(v *Input) { v.DefaultCurrency = "ZZZ" }}, {"version", func(v *Input) { v.Version = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := validInput()
			tc.change(&v)
			if !errors.Is(Validate(&v), user.ErrInvalid) {
				t.Fatal("invalid profile accepted")
			}
		})
	}
}
func TestLogoNormalization(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	var source bytes.Buffer
	_ = png.Encode(&source, img)
	input := append(source.Bytes(), []byte("private metadata")...)
	out, e := NormalizeLogo(input)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(out, []byte("private metadata")) {
		t.Fatal("trailing bytes retained")
	}
	cfg, kind, e := image.DecodeConfig(bytes.NewReader(out))
	if e != nil || kind != "png" || cfg.Width != 32 || cfg.Height != 16 {
		t.Fatal("bad normalized image")
	}
	source.Reset()
	_ = jpeg.Encode(&source, img, nil)
	if _, e = NormalizeLogo(source.Bytes()); e != nil {
		t.Fatal("JPEG rejected", e)
	}
	source.Reset()
	_ = png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 2049, 1)))
	for _, data := range [][]byte{nil, []byte("<svg></svg>"), out[:len(out)/2], make([]byte, MaxLogoSize+1), source.Bytes()} {
		if _, e = NormalizeLogo(data); !errors.Is(e, ErrLogo) {
			t.Fatal("invalid image accepted")
		}
	}
}
func TestServiceRejectsNonAdminBeforeStorage(t *testing.T) {
	s := &Service{}
	a := user.Context{}
	ctx := context.Background()
	if _, e := s.Get(ctx, a); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Save(ctx, a, validInput(), "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.SetLogo(ctx, a, 1, nil, true, "test"); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Logo(ctx, a); !errors.Is(e, user.ErrForbidden) {
		t.Fatal(e)
	}
}
