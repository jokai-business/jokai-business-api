package document

import (
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

var ErrOCRDisabled = errors.New("OCR is not configured")

const MaxOCRText = 8000

type OCRResult struct {
	Text  string
	Pages int
}
type Extractor interface {
	Extract(context.Context, []byte, string) (OCRResult, error)
}
type OCR struct {
	Language     string     `json:"language"`
	Languages    []string   `json:"languages"`
	Available    bool       `json:"available"`
	Status       string     `json:"status"`
	Version      int        `json:"version"`
	Attempts     int        `json:"attempts"`
	SourceSHA256 string     `json:"sourceSha256"`
	Engine       string     `json:"engine"`
	RawText      string     `json:"rawText"`
	ReviewedText string     `json:"reviewedText"`
	Pages        int        `json:"pages"`
	Failure      string     `json:"failure"`
	RequestedAt  *time.Time `json:"requestedAt"`
	CompletedAt  *time.Time `json:"completedAt"`
	ReviewedAt   *time.Time `json:"reviewedAt"`
	ReviewerName string     `json:"reviewerName"`
	ReviewNote   string     `json:"reviewNote"`
}
type OCRReview struct {
	Version   int    `json:"version"`
	Status    string `json:"status"`
	Text      string `json:"text"`
	Note      string `json:"note"`
	Confirmed bool   `json:"confirmed"`
}

func (s *Service) GetOCR(ctx context.Context, a user.Context, id string) (OCR, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return OCR{}, err
	}
	v := OCR{Available: s.OCR != nil || s.KhmerOCR != nil, Status: "not_requested", Engine: "PaddleOCR", Language: "en", Languages: []string{}}
	if s.OCR != nil {
		v.Languages = append(v.Languages, "en")
	}
	if s.KhmerOCR != nil {
		v.Languages = append(v.Languages, "km+en")
	}
	err := s.DB.QueryRow(ctx, `SELECT o.language,o.status,o.version,o.attempts,o.source_sha256,o.engine,o.raw_text,o.reviewed_text,o.pages,o.failure,o.requested_at,o.completed_at,o.reviewed_at,COALESCE(u.name,''),o.review_note FROM document_ocr o LEFT JOIN users u ON u.organization_id=o.organization_id AND u.id=o.reviewed_by WHERE o.organization_id=$1 AND o.document_id=$2`, a.Organization.ID, id).Scan(&v.Language, &v.Status, &v.Version, &v.Attempts, &v.SourceSHA256, &v.Engine, &v.RawText, &v.ReviewedText, &v.Pages, &v.Failure, &v.RequestedAt, &v.CompletedAt, &v.ReviewedAt, &v.ReviewerName, &v.ReviewNote)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return v, err
}
func (s *Service) RequestOCR(ctx context.Context, a user.Context, id string, version int, rid string) error {
	return s.RequestOCRLanguage(ctx, a, id, version, "en", rid)
}
func (s *Service) RequestOCRLanguage(ctx context.Context, a user.Context, id string, version int, language, rid string) error {
	if language == "" {
		language = "en"
	}
	if language != "en" && language != "km+en" {
		return user.ErrInvalid
	}
	engine := "PaddleOCR"
	extractor := s.OCR
	if language == "km+en" {
		engine = "Tesseract 5 (Khmer + English)"
		extractor = s.KhmerOCR
	}

	if !a.Allows("document.review") {
		return user.ErrForbidden
	}
	d, err := s.Get(ctx, a, id)
	if err != nil {
		return err
	}
	if extractor == nil {
		return ErrOCRDisabled
	}
	if version < 0 {
		return user.ErrInvalid
	}
	if d.Status != "accepted" {
		return ErrConflict
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize first requests as well as retries; immutable source and intake decision.
	if _, err = tx.Exec(ctx, `SELECT id FROM documents WHERE organization_id=$1 AND id=$2 FOR UPDATE`, a.Organization.ID, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO document_ocr(organization_id,document_id,status,requested_by,source_sha256,language,engine)
 SELECT $1,$2,'queued',$3,$4,$6,$7 WHERE $5=0
 ON CONFLICT(organization_id,document_id) DO NOTHING`, a.Organization.ID, id, a.User.ID, d.SHA256, version, language, engine)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		tag, err = tx.Exec(ctx, `UPDATE document_ocr SET language=$5,engine=$6,status='queued',requested_by=$3,requested_at=now(),failure='',version=version+1,lease_until=NULL WHERE organization_id=$1 AND document_id=$2 AND version=$4 AND status='failed' AND attempts<3`, a.Organization.ID, id, a.User.ID, version, language, engine)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
	}
	if err = audit(ctx, tx, a, "document.ocr_requested", id, rid, map[string]any{"sha256": d.SHA256, "language": language, "engine": engine}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func validOCRText(s string) bool { return len(s) <= MaxOCRText && product.Text(s, MaxOCRText) }
func (s *Service) ReviewOCR(ctx context.Context, a user.Context, id string, in OCRReview, rid string) error {
	if !a.Allows("document.review") {
		return user.ErrForbidden
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return err
	}
	in.Text = strings.TrimSpace(in.Text)
	in.Note = strings.TrimSpace(in.Note)
	if !in.Confirmed || in.Version < 1 || (in.Status != "approved" && in.Status != "rejected") || !validOCRText(in.Text) || !product.Text(in.Note, 2000) || (in.Status == "approved" && in.Text == "") || (in.Status == "rejected" && in.Note == "") {
		return user.ErrInvalid
	}
	if in.Status == "rejected" {
		in.Text = ""
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE document_ocr SET status=$4,reviewed_text=$5,review_note=$6,reviewed_by=$7,reviewed_at=now(),version=version+1 WHERE organization_id=$1 AND document_id=$2 AND version=$3 AND status='ready'`, a.Organization.ID, id, in.Version, in.Status, in.Text, in.Note, a.User.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if err = audit(ctx, tx, a, "document.ocr_reviewed", id, rid, map[string]any{"status": in.Status, "version": in.Version + 1}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
