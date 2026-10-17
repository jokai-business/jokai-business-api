package document

import (
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"unicode"
)

func Workspaces(a user.Context) []string {
	out := []string{}
	for _, v := range []string{"sales", "corporate", "pmo", "finance"} {
		if a.Allows("document." + v) {
			out = append(out, v)
		}
	}
	return out
}
func allowed(a user.Context, w string) bool {
	for _, v := range Workspaces(a) {
		if v == w {
			return true
		}
	}
	return false
}
func Validate(in *Input, data []byte) (string, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Title == "" || !product.Text(in.Title, 200) || !product.Text(in.Notes, 4000) || (in.CustomerID != "" && !customer.UUID(in.CustomerID)) {
		return "", user.ErrInvalid
	}
	switch in.Workspace {
	case "sales", "corporate", "pmo", "finance":
	default:
		return "", user.ErrInvalid
	}
	// Never use client filenames as paths or allow header-control characters.
	if in.Filename == "" || len(in.Filename) > 240 || strings.ContainsAny(in.Filename, "/\\") || strings.IndexFunc(in.Filename, unicode.IsControl) >= 0 {
		return "", user.ErrInvalid
	}
	if len(data) > MaxSize {
		return "", ErrSize
	}
	if len(data) == 0 {
		return "", user.ErrInvalid
	}
	ext := strings.ToLower(filepath.Ext(in.Filename))
	if ext == ".pdf" && bytes.HasPrefix(data, []byte("%PDF-")) && bytes.Contains(data[max(0, len(data)-1024):], []byte("%%EOF")) {
		return "application/pdf", nil
	}
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err == nil && config.Width > 0 && config.Height > 0 && int64(config.Width)*int64(config.Height) <= 40000000 && ((ext == ".png" && format == "png") || (ext != ".png" && format == "jpeg")) {
			return "image/" + format, nil
		}
	}
	return "", ErrType
}
func hash(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
