package settings

import (
	"ai-business-api/internal/document"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Service) GetDocumentPolicy(ctx context.Context, a user.Context) (document.UploadPolicy, error) {
	var p document.UploadPolicy
	if !a.Allows("settings.manage") {
		return p, user.ErrForbidden
	}
	err := s.DB.QueryRow(ctx, `SELECT `+document.PolicyColumns+` FROM organizations WHERE id=$1`, a.Organization.ID).Scan(&p.MaxMiB, &p.AllowPDF, &p.AllowPNG, &p.AllowJPEG, &p.Version)
	return p, err
}
func (s *Service) SaveDocumentPolicy(ctx context.Context, a user.Context, p document.UploadPolicy, rid string) (document.UploadPolicy, error) {
	if !a.Allows("settings.manage") {
		return document.UploadPolicy{}, user.ErrForbidden
	}
	if err := p.Validate(); err != nil {
		return document.UploadPolicy{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return document.UploadPolicy{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE organizations SET document_max_mib=$2,document_allow_pdf=$3,document_allow_png=$4,document_allow_jpeg=$5,document_policy_version=document_policy_version+1 WHERE id=$1 AND document_policy_version=$6 RETURNING document_policy_version`, a.Organization.ID, p.MaxMiB, p.AllowPDF, p.AllowPNG, p.AllowJPEG, p.Version).Scan(&p.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return document.UploadPolicy{}, ErrConflict
	}
	if err != nil {
		return document.UploadPolicy{}, err
	}
	data, _ := json.Marshal(p)
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,resource_id,action,request_id,after_value) VALUES($1,$2,$1,'organization.document_policy_updated',$3,$4)`, a.Organization.ID, a.User.ID, rid, data)
	if err != nil {
		return document.UploadPolicy{}, err
	}
	return p, tx.Commit(ctx)
}
