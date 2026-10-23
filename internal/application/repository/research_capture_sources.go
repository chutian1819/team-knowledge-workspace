package repository

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrResearchSourceVersionExists = errors.New("source content version already archived")

func (r *knowledgeRepository) FindResearchSourceVersion(ctx context.Context, tenant uint64, knowledgeID, hash string) (*types.ResearchSourceVersion, error) {
	var v types.ResearchSourceVersion
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_id = ? AND content_hash = ?", tenant, knowledgeID, hash).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &v, err
}
func (r *knowledgeRepository) CreateResearchSourceVersion(ctx context.Context, v *types.ResearchSourceVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var live types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ? AND tenant_id = ?", v.KnowledgeID, v.TenantID).First(&live).Error; err != nil {
			return err
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(v)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrResearchSourceVersionExists
		}
		return nil
	})
}
