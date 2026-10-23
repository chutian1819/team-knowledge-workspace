package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// ValidateResearchSources checks both the author's access and the audience of
// the destination. Reading a private source is not permission to publish its
// contents into a more widely shared research draft. This uses the native KB
// and agent grants; it does not introduce another ACL or copy source content.
// Call it again when accepting a proposal or executing an export: grants can
// change while an asynchronous operation is waiting.
func ValidateResearchSources(
	ctx context.Context, db *gorm.DB, targetKBID string, sourceIDs []string,
	kbService interfaces.KnowledgeBaseService, shares interfaces.KBShareService,
	agents interfaces.AgentShareService, knowledge interfaces.KnowledgeService,
) ([]*types.Knowledge, error) {
	caller := types.CallerFromContext(ctx).Normalize()
	if caller.TenantID == 0 {
		return nil, apperrors.NewUnauthorizedError("请先登录")
	}
	if db == nil || kbService == nil || knowledge == nil {
		return nil, fmt.Errorf("research source authorization is not configured")
	}
	if len(sourceIDs) > 200 {
		return nil, apperrors.NewBadRequestError("一次研究最多选择 200 份来源资料")
	}
	target, err := kbService.GetKnowledgeBaseByIDOnly(ctx, targetKBID)
	if err != nil || target == nil {
		return nil, apperrors.NewNotFoundError("研究稿资料集不存在或已移除")
	}
	if _, err = access.ResolveKB(ctx, access.KBRequest{Caller: caller}, target,
		types.OrgRoleViewer, shares, agents); err != nil {
		return nil, apperrors.NewForbiddenError("无权访问研究稿资料集")
	}
	result := make([]*types.Knowledge, 0, len(sourceIDs))
	sourceKBs := make(map[string]*types.KnowledgeBase)
	seen := make(map[string]bool)
	for _, rawID := range sourceIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		source, lookupErr := knowledge.GetKnowledgeByIDOnly(ctx, id)
		if lookupErr != nil || source == nil || source.DeletedAt.Valid {
			return nil, apperrors.NewNotFoundError("所选来源已移除或不可访问")
		}
		kb := sourceKBs[source.KnowledgeBaseID]
		if kb == nil {
			kb, lookupErr = kbService.GetKnowledgeBaseByIDOnly(ctx, source.KnowledgeBaseID)
			if lookupErr != nil || kb == nil || kb.TenantID != source.TenantID {
				return nil, apperrors.NewNotFoundError("所选来源已移除或不可访问")
			}
			if _, lookupErr = access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb,
				types.OrgRoleViewer, shares, agents); lookupErr != nil {
				return nil, apperrors.NewForbiddenError("所选来源已撤销访问权限")
			}
			sourceKBs[kb.ID] = kb
		}
		result = append(result, source)
	}
	if len(sourceKBs) == 0 {
		return result, nil
	}

	// A small team has few workspaces. Check the real effective audience,
	// including agent-based grants, rather than assuming matching org IDs
	// imply matching visibility. No tenant names or contents are returned.
	var tenantIDs []uint64
	if err := db.WithContext(ctx).Model(&types.Tenant{}).
		Where("status = ?", "active").Pluck("id", &tenantIDs).Error; err != nil {
		return nil, fmt.Errorf("load research audience: %w", err)
	}
	for _, tenantID := range tenantIDs {
		visible, checkErr := researchAudienceCanRead(ctx, target, tenantID, shares, agents)
		if checkErr != nil {
			return nil, fmt.Errorf("check research audience: %w", checkErr)
		}
		if !visible {
			continue
		}
		for _, sourceKB := range sourceKBs {
			allowed, checkErr := researchAudienceCanRead(ctx, sourceKB, tenantID, shares, agents)
			if checkErr != nil {
				return nil, fmt.Errorf("check research source audience: %w", checkErr)
			}
			if !allowed {
				return nil, apperrors.NewForbiddenError("研究稿的可见范围大于部分来源资料，请先将来源共享给同一项目，或使用私密研究稿资料集")
			}
		}
	}
	return result, nil
}

func researchAudienceCanRead(ctx context.Context, kb *types.KnowledgeBase, tenantID uint64,
	shares interfaces.KBShareService, agents interfaces.AgentShareService,
) (bool, error) {
	if kb == nil || tenantID == 0 {
		return false, nil
	}
	if kb.TenantID == tenantID {
		return true, nil
	}
	if shares != nil {
		role, shared, err := shares.CheckTenantKBPermission(ctx, kb.ID, tenantID, types.TenantRoleOwner)
		if err != nil {
			return false, err
		}
		if shared && role.IsValid() && role.HasPermission(types.OrgRoleViewer) {
			return true, nil
		}
	}
	if agents != nil {
		return agents.TenantCanAccessKBViaSomeSharedAgent(ctx, tenantID, types.TenantRoleOwner, kb)
	}
	return false, nil
}
