package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResearchCaptureRepository struct{ DB *gorm.DB }

func NewResearchCaptureRepository(db *gorm.DB) *ResearchCaptureRepository {
	return &ResearchCaptureRepository{DB: db}
}
func (r *ResearchCaptureRepository) Get(ctx context.Context, tenant uint64, id string) (*types.ResearchCaptureJob, error) {
	var j types.ResearchCaptureJob
	err := r.DB.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenant, id).First(&j).Error
	return &j, err
}
func (r *ResearchCaptureRepository) List(ctx context.Context, tenant uint64, kb string) ([]types.ResearchCaptureJob, error) {
	var jobs []types.ResearchCaptureJob
	err := r.DB.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenant, kb).Order("created_at DESC").Limit(50).Find(&jobs).Error
	return jobs, err
}
func (r *ResearchCaptureRepository) Create(ctx context.Context, j *types.ResearchCaptureJob) (*types.ResearchCaptureJob, error) {
	// GORM's JSON serializer writes a nil slice as SQL NULL rather than using
	// the column default. Bookmark jobs have no selected audience, but the
	// persisted JSON collections are deliberately NOT NULL in PostgreSQL.
	if j.Items == nil {
		j.Items = []types.ResearchCaptureItem{}
	}
	if j.SelectedTenantIDs == nil {
		j.SelectedTenantIDs = []uint64{}
	}
	if err := r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(j).Error; err != nil {
		return nil, err
	}
	var saved types.ResearchCaptureJob
	if err := r.DB.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", j.TenantID, j.IdempotencyKey).First(&saved).Error; err != nil {
		return nil, err
	}
	if saved.RequestHash != j.RequestHash || saved.UserID != j.UserID {
		return nil, werrors.NewConflictError("idempotency key already used by another request")
	}
	return &saved, nil
}
func (r *ResearchCaptureRepository) Save(ctx context.Context, j *types.ResearchCaptureJob) error {
	q := r.DB.WithContext(ctx).Model(&types.ResearchCaptureJob{}).Where("id = ? AND tenant_id = ?", j.ID, j.TenantID)
	if j.ClaimedLease != nil {
		q = q.Where("lease_until = ?", j.ClaimedLease)
	}
	result := q.Select("status", "completed", "failed", "items", "error", "lease_until", "updated_at").Updates(j)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return werrors.NewConflictError("capture task lease changed; stale progress was not saved")
	}
	return nil
}
func (r *ResearchCaptureRepository) Claim(ctx context.Context, j *types.ResearchCaptureJob) (bool, error) {
	claimed := false
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.ResearchCaptureJob
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND status <> 'completed' AND (lease_until IS NULL OR lease_until < ?)", j.ID, j.TenantID, time.Now()).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		until := time.Now().Add(25 * time.Minute).Truncate(time.Microsecond)
		if err := tx.Model(&types.ResearchCaptureJob{}).Where("id = ? AND tenant_id = ?", j.ID, j.TenantID).Updates(map[string]any{"lease_until": until, "status": "processing"}).Error; err != nil {
			return err
		}
		current.LeaseUntil = &until
		current.ClaimedLease = &until
		current.Status = "processing"
		*j = current
		claimed = true
		return nil
	})
	return claimed, err
}
func (r *ResearchCaptureRepository) PrepareRetry(ctx context.Context, j *types.ResearchCaptureJob) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.ResearchCaptureJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", j.ID, j.TenantID).First(&current).Error; err != nil {
			return err
		}
		if current.Status == "completed" {
			*j = current
			return nil
		}
		if current.LeaseUntil != nil && current.LeaseUntil.After(time.Now()) {
			return werrors.NewConflictError("job is still running")
		}
		if err := tx.Model(&types.ResearchCaptureJob{}).Where("id = ? AND tenant_id = ?", j.ID, j.TenantID).Updates(map[string]any{"status": "queued", "error": "", "lease_until": nil}).Error; err != nil {
			return err
		}
		current.Status = "queued"
		current.Error = ""
		current.LeaseUntil = nil
		*j = current
		return nil
	})
}

// Serialize scopes intake/claim operations across app instances without a second queue.
func (r *ResearchCaptureRepository) Serialize(ctx context.Context, key string, fn func() error) error {
	if r.DB.Dialector.Name() != "postgres" {
		return fn()
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
			return err
		}
		return fn()
	})
}

// WithAudienceLock serializes the final audience check and native share against
// organization membership writes. The membership trigger takes this same lock.
// Keep this transaction read-only: native services use their normal connection,
// and no share-table lock is taken here (which would deadlock that connection).
func (r *ResearchCaptureRepository) WithAudienceLock(ctx context.Context, organizationID string, fn func() error) error {
	if organizationID == "" {
		return werrors.NewBadRequestError("organization_id is required")
	}
	if r.DB.Dialector.Name() != "postgres" {
		return fn()
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "research-audience:"+organizationID).Error; err != nil {
			return err
		}
		return fn()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
}
func (r *ResearchCaptureRepository) PersonalWorkspace(ctx context.Context, tenant uint64, user string) error {
	var members []types.TenantMember
	if err := r.DB.WithContext(ctx).Where("tenant_id = ? AND status = ?", tenant, types.TenantMemberStatusActive).Find(&members).Error; err != nil {
		return err
	}
	if len(members) != 1 || members[0].UserID != user {
		return werrors.NewConflictError("private inbox requires a personal workspace with exactly one active member; create or switch to your personal workspace")
	}
	return nil
}
func (r *ResearchCaptureRepository) CurrentMember(ctx context.Context, tenant uint64, user string) (*types.TenantMember, error) {
	var m types.TenantMember
	err := r.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND status = ?", tenant, user, types.TenantMemberStatusActive).First(&m).Error
	return &m, err
}
