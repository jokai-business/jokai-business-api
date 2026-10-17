package document

import (
	"ai-business-api/internal/ai"
	"ai-business-api/internal/customer"
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

type Service struct {
	LLM                 ai.LLM
	AIProvider, AIModel string
	OCR                 Extractor
	KhmerOCR            Extractor
	DB                  *pgxpool.Pool
	Blobs               BlobStore
	StorageID           string
	LegacyBlobs         BlobStore
}

const columns = `d.id,d.title,d.workspace,COALESCE(d.customer_id::text,''),COALESCE(c.name,''),d.filename,d.content_type,d.size_bytes,d.sha256,d.notes,d.status,d.uploaded_by,u.name,COALESCE(d.reviewed_by::text,''),COALESCE(r.name,''),d.review_note,d.version,d.created_at,d.reviewed_at,d.storage_id`
const joined = ` FROM documents d JOIN users u ON u.organization_id=d.organization_id AND u.id=d.uploaded_by LEFT JOIN users r ON r.organization_id=d.organization_id AND r.id=d.reviewed_by LEFT JOIN customers c ON c.organization_id=d.organization_id AND c.id=d.customer_id`

func scan(row pgx.Row) (Document, error) {
	v := Document{}
	err := row.Scan(&v.ID, &v.Title, &v.Workspace, &v.CustomerID, &v.CustomerName, &v.Filename, &v.ContentType, &v.SizeBytes, &v.SHA256, &v.Notes, &v.Status, &v.UploadedBy, &v.UploaderName, &v.ReviewedBy, &v.ReviewerName, &v.ReviewNote, &v.Version, &v.CreatedAt, &v.ReviewedAt, &v.StorageID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, action, id, rid string, after any) error {
	v, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,after_value) VALUES($1,$2,$3,$4,$5,$6)`, a.Organization.ID, a.User.ID, action, id, rid, v)
	return err
}
func (s *Service) Upload(ctx context.Context, a user.Context, in Input, data []byte, rid string) (Document, error) {
	if !a.Allows("document.upload") || !allowed(a, in.Workspace) {
		return Document{}, user.ErrForbidden
	}
	contentType, err := Validate(&in, data)
	if err != nil {
		return Document{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	// Serialize policy changes against uploads so a completed change governs subsequent uploads.
	var policy UploadPolicy
	err = tx.QueryRow(ctx, `SELECT `+PolicyColumns+` FROM organizations WHERE id=$1 FOR SHARE`, a.Organization.ID).Scan(&policy.MaxMiB, &policy.AllowPDF, &policy.AllowPNG, &policy.AllowJPEG, &policy.Version)
	if err != nil {
		return Document{}, err
	}
	if err = policy.Check(len(data), contentType); err != nil {
		return Document{}, err
	}

	if in.CustomerID != "" {
		if !a.Allows("customer.read") {
			return Document{}, user.ErrForbidden
		}
		var exists bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE organization_id=$1 AND id=$2)`, a.Organization.ID, in.CustomerID).Scan(&exists)
		if err != nil {
			return Document{}, err
		}
		if !exists {
			return Document{}, customer.ErrNotFound
		}
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		return Document{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO documents(id,organization_id,title,workspace,customer_id,filename,content_type,size_bytes,sha256,notes,uploaded_by,storage_id) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,$12)`, id, a.Organization.ID, in.Title, in.Workspace, in.CustomerID, in.Filename, contentType, len(data), hash(data), in.Notes, a.User.ID, s.storageID())
	if err != nil {
		return Document{}, err
	}
	v, err := scan(tx.QueryRow(ctx, `SELECT `+columns+joined+` WHERE d.organization_id=$1 AND d.id=$2`, a.Organization.ID, id))
	if err != nil {
		return v, err
	}
	if err = audit(ctx, tx, a, "document.uploaded", id, rid, map[string]any{"workspace": v.Workspace, "sizeBytes": v.SizeBytes, "sha256": v.SHA256}); err != nil {
		return v, err
	}
	if err = s.Blobs.Put(ctx, a.Organization.ID, id, data); err != nil {
		return Document{}, ErrStorageUnavailable
	}
	// Keep the blob on an ambiguous commit failure: deleting it could lose a committed document.
	return v, tx.Commit(ctx)
}
func (s *Service) Get(ctx context.Context, a user.Context, id string) (Document, error) {
	if !a.Allows("document.read") {
		return Document{}, user.ErrForbidden
	}
	if !customer.UUID(id) {
		return Document{}, user.ErrInvalid
	}
	return scan(s.DB.QueryRow(ctx, `SELECT `+columns+joined+` WHERE d.organization_id=$1 AND d.id=$2 AND d.workspace=ANY($3::text[])`, a.Organization.ID, id, Workspaces(a)))
}
func (s *Service) List(ctx context.Context, a user.Context, q, workspace, status, cust string, page, size int) ([]Document, int, error) {
	if !a.Allows("document.read") {
		return nil, 0, user.ErrForbidden
	}
	if page < 1 || page > 1000000 || size < 1 || size > 100 || !product.Text(q, 200) || (cust != "" && !customer.UUID(cust)) {
		return nil, 0, user.ErrInvalid
	}
	if workspace != "" && !allowed(a, workspace) {
		return nil, 0, user.ErrForbidden
	}
	if status != "" && status != "pending" && status != "accepted" && status != "rejected" {
		return nil, 0, user.ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	where := ` WHERE d.organization_id=$1 AND d.workspace=ANY($2::text[]) AND ($3='' OR d.workspace=$3) AND ($4='' OR d.status=$4) AND ($5='' OR d.customer_id=NULLIF($5,'')::uuid) AND ($6='' OR strpos(lower(d.title || ' ' || d.filename),lower($6))>0)`
	args := []any{a.Organization.ID, Workspaces(a), workspace, status, cust, strings.TrimSpace(q)}
	var total int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM documents d`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := tx.Query(ctx, `SELECT `+columns+joined+where+` ORDER BY d.created_at DESC,d.id LIMIT $7 OFFSET $8`, args...)
	if err != nil {
		return nil, 0, err
	}
	out := []Document{}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return out, total, tx.Commit(ctx)
}
func (s *Service) Review(ctx context.Context, a user.Context, id string, in Review, rid string) (Document, error) {
	if !a.Allows("document.review") || !a.Allows("document.read") {
		return Document{}, user.ErrForbidden
	}
	in.Note = strings.TrimSpace(in.Note)
	if !customer.UUID(id) || in.Version < 1 || (in.Status != "accepted" && in.Status != "rejected") || !product.Text(in.Note, 2000) || (in.Status == "rejected" && in.Note == "") {
		return Document{}, user.ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	v, err := scan(tx.QueryRow(ctx, `SELECT `+columns+joined+` WHERE d.organization_id=$1 AND d.id=$2 AND d.workspace=ANY($3::text[]) FOR UPDATE OF d`, a.Organization.ID, id, Workspaces(a)))
	if err != nil {
		return v, err
	}
	if v.Version != in.Version || v.Status != "pending" {
		return Document{}, ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE documents SET status=$3,reviewed_by=$4,review_note=$5,reviewed_at=now(),version=version+1 WHERE organization_id=$1 AND id=$2`, a.Organization.ID, id, in.Status, a.User.ID, in.Note)
	if err != nil {
		return Document{}, err
	}
	if err = audit(ctx, tx, a, "document.reviewed", id, rid, map[string]any{"status": in.Status, "workspace": v.Workspace}); err != nil {
		return Document{}, err
	}
	v, err = scan(tx.QueryRow(ctx, `SELECT `+columns+joined+` WHERE d.organization_id=$1 AND d.id=$2`, a.Organization.ID, id))
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Download(ctx context.Context, a user.Context, id, rid string) (Document, []byte, error) {
	v, err := s.Get(ctx, a, id)
	if err != nil {
		return v, nil, err
	}
	store, err := s.storeFor(v.StorageID)
	if err != nil {
		return v, nil, err
	}
	data, err := store.Get(ctx, a.Organization.ID, id)
	if err != nil {
		return v, nil, ErrStorageUnavailable
	}
	if int64(len(data)) != v.SizeBytes || hash(data) != v.SHA256 {
		return v, nil, ErrStorageUnavailable
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return v, nil, err
	}
	defer tx.Rollback(ctx)
	if err = audit(ctx, tx, a, "document.downloaded", id, rid, map[string]string{"workspace": v.Workspace}); err != nil {
		return v, nil, err
	}
	return v, data, tx.Commit(ctx)
}

func (s *Service) storageID() string {
	if s.StorageID == "" {
		return "local"
	}
	return s.StorageID
}
func (s *Service) storeFor(id string) (BlobStore, error) {
	if id == s.storageID() && s.Blobs != nil {
		return s.Blobs, nil
	}
	if id == "local" && s.LegacyBlobs != nil {
		return s.LegacyBlobs, nil
	}
	return nil, ErrStorageUnavailable
}
