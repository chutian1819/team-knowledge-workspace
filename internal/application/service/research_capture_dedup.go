package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type researchInboxDeduplicator interface {
	ResearchInboxDedupActive(context.Context) bool
	WithResearchInboxDedup(context.Context, string, string,
		func(context.Context) (*types.Knowledge, error)) (bool, *types.Knowledge, error)
}

func (s *knowledgeService) researchInboxDedupActive(ctx context.Context) bool {
	repo, ok := s.repo.(researchInboxDeduplicator)
	return ok && repo.ResearchInboxDedupActive(ctx)
}
