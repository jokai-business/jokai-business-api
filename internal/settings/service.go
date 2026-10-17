package settings

import (
	"ai-business-api/internal/document"
	"ai-business-api/internal/user"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	Health       *HealthMonitor
	Integrations IntegrationConfig
	DB           *pgxpool.Pool
	Blobs        document.BlobStore
	StorageID    string
	LegacyBlobs  document.BlobStore
}

const columns = `name,address,contact_email,phone,timezone,default_currency,settings_version,logo_id IS NOT NULL,settings_updated_at`

func scan(row pgx.Row) (Profile, error) {
	var p Profile
	e := row.Scan(&p.Name, &p.Address, &p.ContactEmail, &p.Phone, &p.Timezone, &p.DefaultCurrency, &p.Version, &p.HasLogo, &p.UpdatedAt)
	return p, e
}
func (s *Service) Get(ctx context.Context, a user.Context) (Profile, error) {
	if !a.Allows("settings.manage") {
		return Profile{}, user.ErrForbidden
	}
	return scan(s.DB.QueryRow(ctx, `SELECT `+columns+` FROM organizations WHERE id=$1`, a.Organization.ID))
}
func audit(ctx context.Context, tx pgx.Tx, a user.Context, action, rid string, before, after Profile) error {
	b, _ := json.Marshal(before)
	v, _ := json.Marshal(after)
	_, e := tx.Exec(ctx, `INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id,before_value,after_value) VALUES($1,$2,$3,$1,$4,$5,$6)`, a.Organization.ID, a.User.ID, action, rid, b, v)
	return e
}
func (s *Service) Save(ctx context.Context, a user.Context, in Input, rid string) (Profile, error) {
	if !a.Allows("settings.manage") {
		return Profile{}, user.ErrForbidden
	}
	if e := Validate(&in); e != nil {
		return Profile{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Profile{}, e
	}
	defer tx.Rollback(ctx)
	old, e := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM organizations WHERE id=$1 FOR UPDATE`, a.Organization.ID))
	if e != nil {
		return Profile{}, e
	}
	if old.Version != in.Version {
		return Profile{}, ErrConflict
	}
	p, e := scan(tx.QueryRow(ctx, `UPDATE organizations SET name=$2,address=$3,contact_email=$4,phone=$5,timezone=$6,default_currency=$7,settings_version=settings_version+1,settings_updated_at=now() WHERE id=$1 RETURNING `+columns, a.Organization.ID, in.Name, in.Address, in.ContactEmail, in.Phone, in.Timezone, in.DefaultCurrency))
	if e != nil {
		return Profile{}, e
	}
	if e = audit(ctx, tx, a, "organization.settings_updated", rid, old, p); e != nil {
		return Profile{}, e
	}
	return p, tx.Commit(ctx)
}
func hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

// Old blobs are retained: replacement/removal only updates the current pointer.
func (s *Service) SetLogo(ctx context.Context, a user.Context, version int, data []byte, remove bool, rid string) (Profile, error) {
	if !a.Allows("settings.manage") {
		return Profile{}, user.ErrForbidden
	}
	if version < 1 {
		return Profile{}, user.ErrInvalid
	}
	var normalized []byte
	var e error
	if !remove {
		normalized, e = NormalizeLogo(data)
		if e != nil {
			return Profile{}, e
		}
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Profile{}, e
	}
	defer tx.Rollback(ctx)
	old, e := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM organizations WHERE id=$1 FOR UPDATE`, a.Organization.ID))
	if e != nil {
		return Profile{}, e
	}
	if old.Version != version {
		return Profile{}, ErrConflict
	}
	id, storage, digest := "", "", ""
	action := "organization.logo_removed"
	if !remove {
		if e = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
			return Profile{}, e
		}
		storage = s.StorageID
		digest = hash(normalized)
		action = "organization.logo_updated"
		if s.Blobs == nil {
			return Profile{}, document.ErrStorageUnavailable
		}
		if e = s.Blobs.Put(ctx, a.Organization.ID, id, normalized); e != nil {
			return Profile{}, document.ErrStorageUnavailable
		}
	}
	p, e := scan(tx.QueryRow(ctx, `UPDATE organizations SET logo_id=NULLIF($2,'')::uuid,logo_storage_id=$3,logo_sha256=$4,settings_version=settings_version+1,settings_updated_at=now() WHERE id=$1 RETURNING `+columns, a.Organization.ID, id, storage, digest))
	if e != nil {
		return Profile{}, e
	}
	if e = audit(ctx, tx, a, action, rid, old, p); e != nil {
		return Profile{}, e
	}
	// A failed/ambiguous commit can leave an unreferenced private blob. Do not delete
	// on commit error because the pointer may have committed successfully.
	return p, tx.Commit(ctx)
}
func (s *Service) Logo(ctx context.Context, a user.Context) ([]byte, error) {
	if !a.Allows("settings.manage") {
		return nil, user.ErrForbidden
	}
	var id, storage, digest string
	e := s.DB.QueryRow(ctx, `SELECT COALESCE(logo_id::text,''),logo_storage_id,logo_sha256 FROM organizations WHERE id=$1`, a.Organization.ID).Scan(&id, &storage, &digest)
	if e != nil {
		return nil, e
	}
	if id == "" {
		return nil, ErrNotFound
	}
	var store document.BlobStore
	if storage == s.StorageID {
		store = s.Blobs
	} else if storage == "local" {
		store = s.LegacyBlobs
	}
	if store == nil {
		return nil, document.ErrStorageUnavailable
	}
	data, e := store.Get(ctx, a.Organization.ID, id)
	if e != nil || len(data) > MaxLogoSize || hash(data) != digest {
		return nil, document.ErrStorageUnavailable
	}
	return data, nil
}
