package settings

import (
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"bytes"
	"errors"
	"golang.org/x/text/currency"
	"image"
	_ "image/jpeg"
	"image/png"
	"net/mail"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
)

const MaxLogoSize = 2 << 20

var ErrConflict = errors.New("organization settings changed")
var ErrLogo = errors.New("invalid organization logo")
var ErrNotFound = errors.New("organization logo not found")

type Input struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	ContactEmail    string `json:"contactEmail"`
	Phone           string `json:"phone"`
	Timezone        string `json:"timezone"`
	DefaultCurrency string `json:"defaultCurrency"`
	Version         int    `json:"version"`
}
type Profile struct {
	Input
	HasLogo   bool      `json:"hasLogo"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func Validate(in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	in.ContactEmail = strings.TrimSpace(in.ContactEmail)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Timezone = strings.TrimSpace(in.Timezone)
	in.DefaultCurrency = strings.ToUpper(strings.TrimSpace(in.DefaultCurrency))
	if in.Version < 1 || in.Name == "" || !product.Text(in.Name, 200) || !product.Text(in.Address, 1000) || !product.Text(in.Phone, 50) || len(in.ContactEmail) > 254 || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 || strings.IndexFunc(in.Phone, unicode.IsControl) >= 0 {
		return user.ErrInvalid
	}
	if in.ContactEmail != "" {
		a, e := mail.ParseAddress(in.ContactEmail)
		if e != nil || a.Address != in.ContactEmail || strings.ContainsAny(in.ContactEmail, "\r\n") {
			return user.ErrInvalid
		}
	}
	if in.Timezone == "" || in.Timezone == "Local" || len(in.Timezone) > 100 {
		return user.ErrInvalid
	}
	if _, e := time.LoadLocation(in.Timezone); e != nil {
		return user.ErrInvalid
	}
	if !product.Currency.MatchString(in.DefaultCurrency) {
		return user.ErrInvalid
	}
	if _, e := currency.ParseISO(in.DefaultCurrency); e != nil {
		return user.ErrInvalid
	}
	return nil
}

// Decode and re-encode raster uploads to discard metadata and trailing content.
// Restrict dimensions before full decoding to bound memory usage.
func NormalizeLogo(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxLogoSize {
		return nil, ErrLogo
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 || int64(cfg.Width)*int64(cfg.Height) > 4000000 {
		return nil, ErrLogo
	}
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return nil, ErrLogo
	}
	var out bytes.Buffer
	if e = png.Encode(&out, img); e != nil || out.Len() > MaxLogoSize {
		return nil, ErrLogo
	}
	return out.Bytes(), nil
}
