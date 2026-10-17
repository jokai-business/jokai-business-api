package document

import (
	"ai-business-api/internal/user"
	"context"
	"errors"
)

var ErrPolicy = errors.New("document upload policy rejected file")

type UploadPolicy struct {
	MaxMiB    int  `json:"maxMiB"`
	AllowPDF  bool `json:"allowPdf"`
	AllowPNG  bool `json:"allowPng"`
	AllowJPEG bool `json:"allowJpeg"`
	Version   int  `json:"version"`
}

const PolicyColumns = `document_max_mib,document_allow_pdf,document_allow_png,document_allow_jpeg,document_policy_version`

func (p UploadPolicy) Validate() error {
	if p.MaxMiB < 1 || p.MaxMiB > MaxSize/(1<<20) || p.Version < 1 || !(p.AllowPDF || p.AllowPNG || p.AllowJPEG) {
		return user.ErrInvalid
	}
	return nil
}
func (p UploadPolicy) Check(size int, contentType string) error {
	if p.Validate() != nil || size > p.MaxMiB*(1<<20) {
		return ErrPolicy
	}
	if (contentType == "application/pdf" && p.AllowPDF) || (contentType == "image/png" && p.AllowPNG) || (contentType == "image/jpeg" && p.AllowJPEG) {
		return nil
	}
	return ErrPolicy
}
func (s *Service) GetUploadPolicy(ctx context.Context, a user.Context) (UploadPolicy, error) {
	var p UploadPolicy
	if !a.Allows("document.upload") {
		return p, user.ErrForbidden
	}
	err := s.DB.QueryRow(ctx, `SELECT `+PolicyColumns+` FROM organizations WHERE id=$1`, a.Organization.ID).Scan(&p.MaxMiB, &p.AllowPDF, &p.AllowPNG, &p.AllowJPEG, &p.Version)
	return p, err
}
