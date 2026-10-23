package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type researchDedupOwnerKey struct{}

func (r *knowledgeRepository) ResearchInboxDedupActive(ctx context.Context) bool {
	return ctx.Value(researchDedupOwnerKey{}) != nil
}

// WithResearchInboxDedup extends native ingestion only for a bound personal
// inbox. Existing evidence may have moved into another project owned by the
// same person. Keep its ID instead of creating another parse/index pipeline.
func (r *knowledgeRepository) WithResearchInboxDedup(ctx context.Context, kbID, key string,
	fn func(context.Context) (*types.Knowledge, error),
) (bool, *types.Knowledge, error) {
	if ctx.Value(researchDedupOwnerKey{}) != nil || !r.db.Migrator().HasTable(&types.ResearchInbox{}) {
		return false, nil, nil
	}
	caller := types.CallerFromContext(ctx)
	var binding types.ResearchInbox
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", caller.TenantID, kbID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil, nil
	}
	if err != nil {
		return true, nil, err
	}
	_, machine := types.TenantAPIKeyScopeFromContext(ctx)
	if caller.UserID != binding.UserID && !(machine && types.IsSyntheticUserID(caller.UserID)) {
		return false, nil, nil
	}
	lockedContext := context.WithValue(ctx, researchDedupOwnerKey{}, binding.UserID)
	var knowledge *types.Knowledge
	var ingestError error
	run := func() error {
		knowledge, ingestError = fn(lockedContext)
		// Native duplicate errors are an expected successful read. They must
		// not prevent the advisory-lock transaction from releasing normally.
		return nil
	}
	if r.db.Dialector.Name() == "postgres" {
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
				fmt.Sprintf("research-dedup:%d:%s", binding.TenantID, key)).Error; err != nil {
				return err
			}
			return run()
		})
	} else {
		err = run()
	}
	if err != nil {
		return true, nil, err
	}
	return true, knowledge, ingestError
}
