package document

import (
	"ai-business-api/internal/user"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"strings"
	"time"
)

func (s *Service) RunOCR(ctx context.Context, logger *slog.Logger) {
	if s.OCR == nil && s.KhmerOCR == nil {
		return
	}
	for {
		worked, err := s.processOCR(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Error("OCR worker failed")
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Service) processOCR(ctx context.Context) (bool, error) {
	// Interrupted jobs become explicitly retryable; no repeated unbounded inference.
	_, err := s.DB.Exec(ctx, `UPDATE document_ocr SET status='failed',failure='Processing was interrupted. Retry if attempts remain.',lease_until=NULL,version=version+1 WHERE status='processing' AND lease_until<now()`)
	if err != nil {
		return false, err
	}
	var org, id, uid, sha, language string
	var version int
	err = s.DB.QueryRow(ctx, `UPDATE document_ocr SET status='processing',attempts=attempts+1,version=version+1,lease_until=now()+interval '2 minutes' WHERE (organization_id,document_id) IN (SELECT organization_id,document_id FROM document_ocr WHERE status='queued' AND attempts<3 ORDER BY requested_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING organization_id,document_id,requested_by,source_sha256,version,language`).Scan(&org, &id, &uid, &sha, &version, &language)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	jobCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result := OCRResult{}
	failure := ""
	var subject string
	err = s.DB.QueryRow(jobCtx, `SELECT subject FROM users WHERE organization_id=$1 AND id=$2 AND active`, org, uid).Scan(&subject)
	a := user.Context{}
	if err == nil {
		a, err = (&user.Repository{DB: s.DB}).Resolve(jobCtx, subject)
	}
	if err != nil || a.Organization.ID != org || !a.Allows("document.review") {
		failure = "Requester no longer has access. An authorized reviewer can retry."
	} else {
		d, e := s.Get(jobCtx, a, id)
		if e != nil || d.Status != "accepted" || d.SHA256 != sha {
			failure = "Source document is unavailable."
		} else {
			store, e := s.storeFor(d.StorageID)
			var data []byte
			if e == nil {
				data, e = store.Get(jobCtx, org, id)
			}
			if e != nil || int64(len(data)) != d.SizeBytes || hash(data) != sha {
				failure = "Source file could not be verified."
			} else {
				extractor := s.OCR
				if language == "km+en" {
					extractor = s.KhmerOCR
				}
				if extractor == nil {
					e = ErrOCRDisabled
				} else {
					result, e = extractor.Extract(jobCtx, data, d.ContentType)
				}
				if e != nil {
					failure = "OCR could not process this file. Check the service or upload a smaller, clearer source."
				} else if !validOCRText(result.Text) || strings.TrimSpace(result.Text) == "" || result.Pages < 1 || result.Pages > 10 {
					failure = "OCR returned no usable text or exceeded the 10-page / 8,000-byte text limit."
				}
			}
		}
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	status := "ready"
	if failure != "" {
		status = "failed"
		result = OCRResult{}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE document_ocr SET status=$4,raw_text=$5,pages=$6,failure=$7,completed_at=now(),lease_until=NULL,version=version+1 WHERE organization_id=$1 AND document_id=$2 AND version=$3 AND status='processing'`, org, id, version, status, result.Text, result.Pages, failure)
	if err != nil {
		return true, err
	}
	if tag.RowsAffected() == 0 {
		return true, nil
	}
	actor := user.Context{Organization: user.Organization{ID: org}, User: user.User{ID: uid}}
	if err = audit(ctx, tx, actor, "document.ocr_completed", id, "ocr-worker", map[string]any{"status": status, "pages": result.Pages, "sha256": sha}); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
