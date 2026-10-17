package document

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"strings"
	"time"
)

type extractionJob struct{ org, uid, id, jobID, hash, provider, model, prompt string }

func (s *Service) RunExtraction(ctx context.Context, logger *slog.Logger) {
	for {
		worked, e := s.processExtraction(ctx)
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			logger.Error("Document AI worker failed")
		}
		if worked && e == nil {
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
func (s *Service) extractionSnapshot(ctx context.Context, a user.Context, j extractionJob) (string, error) {
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	v, e := extractionSource(ctx, tx, a, j.id)
	if e != nil {
		return "", e
	}
	input, h, e := extractionFingerprint(v)
	if e != nil {
		return "", e
	}
	if h != j.hash {
		return "", ErrExtractionChanged
	}
	return input, tx.Commit(ctx)
}
func (s *Service) processExtraction(ctx context.Context) (bool, error) {
	_, e := s.DB.Exec(ctx, `DELETE FROM document_ai_extractions WHERE requested_at<=now()-interval '30 days'`)
	if e != nil {
		return false, e
	}
	_, e = s.DB.Exec(ctx, `UPDATE document_ai_extractions SET status='failed',version=version+1,lease_until=NULL,completed_at=now() WHERE (status='processing' AND lease_until<now()) OR (status='queued' AND requested_at<now()-interval '10 minutes')`)
	if e != nil {
		return false, e
	}
	if s.LLM == nil {
		return false, nil
	}
	var j extractionJob
	e = s.DB.QueryRow(ctx, `UPDATE document_ai_extractions SET status='processing',version=version+1,lease_until=now()+interval '90 seconds' WHERE job_id IN(SELECT job_id FROM document_ai_extractions WHERE status='queued' ORDER BY requested_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING organization_id,user_id,document_id,job_id,source_hash,provider,model,prompt_version`).Scan(&j.org, &j.uid, &j.id, &j.jobID, &j.hash, &j.provider, &j.model, &j.prompt)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	a := user.Context{User: user.User{ID: j.uid}, Organization: user.Organization{ID: j.org}}
	input, failure := s.extractionSnapshot(ctx, a, j)
	text := ""
	if failure == nil {
		req, prompt, err := ai.InvoiceExtraction(input)
		failure = err
		if failure == nil && (j.provider != s.AIProvider || j.model != s.AIModel || j.prompt != prompt) {
			failure = ErrExtractionChanged
		}
		if failure == nil {
			callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			text, failure = s.LLM.Generate(callCtx, req)
			cancel()
		}
	}
	text = strings.TrimSpace(text)
	if failure == nil {
		var src ExtractionSource
		if e := json.Unmarshal([]byte(input), &src); e != nil {
			failure = e
		} else {
			var suggested ExtractionSuggestion
			suggested, failure = parseExtraction(text, src.Text)
			if failure == nil {
				b, _ := json.Marshal(suggested)
				text = string(b)
				if len(b) > 8000 {
					failure = ErrCaptureInvalid
				}
			}
		}
	}
	// Recheck current records and permissions after the external call, in the same
	// repeatable-read snapshot used to publish. Later changes are hidden on reads.
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return true, e
	}
	defer tx.Rollback(ctx)
	v, err := extractionSource(ctx, tx, a, j.id)
	if err != nil {
		failure = err
	} else {
		_, h, err := extractionFingerprint(v)
		if err != nil || h != j.hash {
			failure = ErrExtractionChanged
		}
	}
	status, action := "ready", "document.ai_completed"
	if failure != nil {
		status = "failed"
		text = ""
		action = "document.ai_failed"
	}
	tag, e := tx.Exec(ctx, `UPDATE document_ai_extractions SET status=$2,output=$3,version=version+1,completed_at=now(),lease_until=NULL WHERE job_id=$1 AND status='processing'`, j.jobID, status, text)
	if e != nil {
		return true, e
	}
	if tag.RowsAffected() > 0 {
		if e = audit(ctx, tx, a, action, j.id, "extraction-worker", nil); e != nil {
			return true, e
		}
	}
	return true, tx.Commit(ctx)
}
