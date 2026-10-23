package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The current page's sources, original evidence and every inherited writing
// rule are all checked. A source moved after writing must not leak via a diff.
func (s *ResearchDraftService) validateWritingEventSourcesVisited(ctx context.Context, kbID string, event *types.ResearchWritingEvent, visited map[string]int, depth int) (err error) {
	// Content versions form a linear journal, not a chain of inferred rules.
	// Walk it iteratively so ordinary long-lived drafts have no recursion limit.
	active := []string{}
	defer func() {
		for _, key := range active {
			if err == nil {
				visited[key] = 2
			} else {
				delete(visited, key)
			}
		}
	}()
	for event != nil {
		key := "event:" + event.ID
		if visited[key] == 1 {
			return researchInput("cyclic writing event provenance")
		}
		if visited[key] == 2 {
			return nil
		}
		if len(visited) >= 10000 {
			return researchInput("writing event provenance exceeds 10,000 nodes")
		}
		visited[key] = 1
		active = append(active, key)
		if _, err = s.sources(ctx, kbID, researchSources(&types.WikiPage{SourceRefs: event.SourceRefs})); err != nil {
			return err
		}
		if err = s.validateProposalWritingSourcesVisited(ctx, kbID, &types.ResearchProposal{WritingRules: event.WritingRules}, visited, depth); err != nil {
			return err
		}
		for _, id := range event.AncestorProposalIDs {
			proposal, _, e := s.writingSourceProposal(ctx, kbID, event.PageID, id)
			if e != nil {
				return e
			}
			if err = s.validateProposalWritingSourcesVisited(ctx, kbID, proposal, visited, depth); err != nil {
				return err
			}
		}
		if event.ParentEventID == "" {
			return nil
		}
		var parent types.ResearchWritingEvent
		if err = s.db.WithContext(ctx).Where("id = ? AND knowledge_base_id = ? AND page_id = ?", event.ParentEventID, kbID, event.PageID).First(&parent).Error; err != nil {
			return access.ErrNotFound
		}
		event = &parent
	}
	return nil
}

func (s *ResearchDraftService) loadWritingSourceEvent(ctx context.Context, kbID, pageID, eventID string) (*types.ResearchWritingEvent, *types.WikiPage, error) {
	var event types.ResearchWritingEvent
	query := s.db.WithContext(ctx).Where("id = ? AND knowledge_base_id = ?", eventID, kbID)
	if pageID != "" {
		query = query.Where("page_id = ?", pageID)
	}
	if err := query.First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, access.ErrNotFound
		}
		return nil, nil, err
	}
	page, err := s.Get(ctx, kbID, event.PageID)
	if err != nil {
		return nil, nil, err
	}
	return &event, page, nil
}

func (s *ResearchDraftService) writingSourceEvent(ctx context.Context, kbID, pageID, eventID string) (*types.ResearchWritingEvent, *types.WikiPage, error) {
	event, page, err := s.loadWritingSourceEvent(ctx, kbID, pageID, eventID)
	if err != nil {
		return nil, nil, err
	}
	if err = s.validateWritingEventSourcesVisited(ctx, kbID, event, map[string]int{}, 0); err != nil {
		return nil, nil, err
	}
	return event, page, nil
}

func (s *ResearchDraftService) newWritingEvent(ctx context.Context, page *types.WikiPage, kind string) (*types.ResearchWritingEvent, error) {
	event := &types.ResearchWritingEvent{ID: uuid.NewString(), TenantID: page.TenantID, KnowledgeBaseID: page.KnowledgeBaseID, PageID: page.ID, Kind: kind, BaseVersion: page.Version, BaseContent: page.Content, Content: page.Content, ResultVersion: page.Version, SourceRefs: append(types.StringArray{}, page.SourceRefs...), SourceVersions: types.JSON(`{}`), WritingRules: types.JSON(`[]`), AncestorProposalIDs: types.StringArray{}, Status: "completed", CreatedBy: researchWritingAuthor(ctx), CreatedAt: time.Now()}
	var metadata struct {
		SourceVersions json.RawMessage `json:"source_versions"`
	}
	_ = json.Unmarshal(page.PageMetadata, &metadata)
	if len(metadata.SourceVersions) > 0 {
		event.SourceVersions = types.JSON(metadata.SourceVersions)
	}
	var parent types.ResearchWritingEvent
	err := s.db.WithContext(ctx).Where("page_id = ? AND result_version = ? AND status = ? AND kind IN ?", page.ID, page.Version, "completed", []string{"create", "save", "accept", "revert"}).Order("created_at DESC, id DESC").First(&parent).Error
	if err == nil {
		event.ParentEventID = parent.ID
		event.WritingRules = append(types.JSON{}, parent.WritingRules...)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	} else {
		// Older drafts have no journal: retain accepted proposal lineage rather than inventing events.
		if err = s.db.WithContext(ctx).Model(&types.ResearchProposal{}).Where("page_id = ? AND status = ? AND base_version < ?", page.ID, "accepted", page.Version).Order("created_at ASC").Pluck("id", &event.AncestorProposalIDs).Error; err != nil {
			return nil, err
		}
	}
	return event, nil
}

func (s *ResearchDraftService) WritingEvents(ctx context.Context, kbID, pageID string) ([]*types.ResearchWritingEvent, error) {
	return s.WritingEventsPage(ctx, kbID, pageID, "")
}

func (s *ResearchDraftService) WritingEvent(ctx context.Context, kbID, pageID, eventID string) (*types.ResearchWritingEvent, error) {
	event, _, err := s.writingSourceEvent(ctx, kbID, pageID, eventID)
	if err != nil {
		return nil, err
	}
	if err = s.enrichWritingEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *ResearchDraftService) enrichWritingEvent(ctx context.Context, event *types.ResearchWritingEvent) error {
	var extraction types.ResearchWritingExtraction
	err := s.db.WithContext(ctx).First(&extraction, "event_id = ?", event.ID).Error
	if err == nil {
		event.LearningStatus, event.LearningError = extraction.Status, extraction.Error
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func (s *ResearchDraftService) WritingEventsPage(ctx context.Context, kbID, pageID, beforeID string) ([]*types.ResearchWritingEvent, error) {
	ctx, _, err := s.authorize(ctx, kbID, false)
	if err != nil {
		return nil, err
	}
	if _, err = s.Get(ctx, kbID, pageID); err != nil {
		return nil, err
	}
	events := []*types.ResearchWritingEvent{}
	query := s.db.WithContext(ctx).Where("knowledge_base_id = ? AND page_id = ?", kbID, pageID)
	if beforeID != "" {
		cursor, _, cursorErr := s.writingSourceEvent(ctx, kbID, pageID, beforeID)
		if cursorErr != nil {
			return nil, cursorErr
		}
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	if err = query.Order("created_at DESC, id DESC").Limit(100).Find(&events).Error; err != nil {
		return nil, err
	}
	visited := map[string]int{}
	for _, event := range events {
		if err = s.validateWritingEventSourcesVisited(ctx, kbID, event, visited, 0); err != nil {
			return nil, err
		}
		if err = s.enrichWritingEvent(ctx, event); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (s *ResearchDraftService) recordGenerationFailure(ctx context.Context, prompt *types.ResearchWritingEvent, modelID string, cause error) error {
	ctx, observation := startResearchObservation(ctx, "draft.generation_failed", prompt.KnowledgeBaseID, prompt.PageID, map[string]any{"model_id": modelID})
	defer func() { observation.Finish(cause) }()
	failed := *prompt
	failed.ID, failed.Kind, failed.Status, failed.Error, failed.ModelID, failed.PromptEventID, failed.CreatedAt = uuid.NewString(), "generation_failed", "failed", safeWritingLearningError(cause), modelID, prompt.ID, time.Now()
	err := s.db.WithContext(context.WithoutCancel(ctx)).Create(&failed).Error
	if err == nil {
		observation.Event(ctx, &failed)
	}
	return err
}

func writingEventLearnable(event *types.ResearchWritingEvent) error {
	if event.Status != "completed" || (event.Kind != "save" && event.Kind != "accept" && event.Kind != "revert") || event.BaseContent == event.Content {
		return researchInput("learn from a saved, accepted or reverted event with an actual content difference")
	}
	return nil
}

func (s *ResearchDraftService) writingEventContext(ctx context.Context, event *types.ResearchWritingEvent) ([]*types.ResearchWritingEvent, error) {
	// Context is bounded by the preceding real content version. Include all
	// submitted prompts and their failed/rejected outcomes in this interval.
	var previous types.ResearchWritingEvent
	query := s.db.WithContext(ctx).Where("page_id = ? AND created_at < ? AND kind IN ? AND result_version <= ?", event.PageID, event.CreatedAt, []string{"create", "save", "accept", "revert"}, event.BaseVersion).Order("created_at DESC").First(&previous)
	if query.Error != nil && !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return nil, query.Error
	}
	rows := []*types.ResearchWritingEvent{}
	q := s.db.WithContext(ctx).Where("page_id = ? AND created_at <= ? AND kind IN ?", event.PageID, event.CreatedAt, []string{"prompt", "proposal", "generation_failed", "reject"})
	if previous.ID != "" {
		q = q.Where("created_at >= ?", previous.CreatedAt)
	}
	// Do not silently truncate prompts: fail explicitly if this interval is too large.
	if err := q.Order("created_at ASC, id ASC").Limit(501).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 500 {
		return nil, researchInput("writing context exceeds 500 events; no partial learning was performed")
	}
	for _, row := range rows {
		if err := s.validateWritingEventSourcesVisited(ctx, event.KnowledgeBaseID, row, map[string]int{}, 0); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func journalWriteError(err error) error {
	return fmt.Errorf("writing activity could not be recorded: %w", err)
}

// Provider errors may contain credential-bearing URLs or response bodies.
// Persist only known application messages or a fixed provider-safe summary.
func safeWritingLearningError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, access.ErrForbidden) || errors.Is(err, access.ErrNotFound) {
		return "writing source or account access was revoked"
	}
	if errors.Is(err, ErrResearchInput) {
		return err.Error()
	}
	if errors.Is(err, ErrResearchConflict) {
		return "writing content or learning task changed; retry"
	}
	return "模型调用或学习任务失败，请检查模型配置后重试"
}

func (s *ResearchDraftService) writingEventFeedback(ctx context.Context, event *types.ResearchWritingEvent) ([]*types.ResearchWritingFeedback, error) {
	proposalIDs := map[string]bool{}
	seen := map[string]bool{}
	for event != nil {
		if seen[event.ID] {
			return nil, researchInput("cyclic writing event provenance")
		}
		seen[event.ID] = true
		if len(seen) > 10000 {
			return nil, researchInput("writing event provenance exceeds 10,000 nodes")
		}
		if event.ProposalID != "" {
			proposalIDs[event.ProposalID] = true
		}
		for _, id := range event.AncestorProposalIDs {
			proposalIDs[id] = true
		}
		if event.ParentEventID == "" {
			break
		}
		var parent types.ResearchWritingEvent
		if err := s.db.WithContext(ctx).Where("id = ? AND page_id = ? AND knowledge_base_id = ?", event.ParentEventID, event.PageID, event.KnowledgeBaseID).First(&parent).Error; err != nil {
			return nil, access.ErrNotFound
		}
		event = &parent
	}
	feedback := []*types.ResearchWritingFeedback{}
	for id := range proposalIDs {
		rows, err := s.WritingFeedback(ctx, event.KnowledgeBaseID, event.PageID, id)
		if err != nil {
			return nil, err
		}
		feedback = append(feedback, rows...)
	}
	// Canonical ordering keeps the sample hash stable across task retries.
	sort.Slice(feedback, func(i, j int) bool {
		a, b := feedback[i], feedback[j]
		return fmt.Sprintf("%s/%s/%d/%s", a.ProposalID, a.RuleID, a.CallerTenantID, a.CreatedBy) < fmt.Sprintf("%s/%s/%d/%s", b.ProposalID, b.RuleID, b.CallerTenantID, b.CreatedBy)
	})
	return feedback, nil
}
