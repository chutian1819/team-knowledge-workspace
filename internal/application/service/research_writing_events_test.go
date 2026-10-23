package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type writingFailureChat struct{ researchChatFixture }

func (m *writingFailureChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	return nil, errors.New("upstream https://provider.example/?api_key=SECRET_TOKEN body=private")
}

type writingNoModels struct{ researchModelFixture }

func (m writingNoModels) ListModels(context.Context) ([]*types.Model, error) {
	return []*types.Model{}, nil
}

func writingEventOfKind(t *testing.T, ctx context.Context, s *ResearchDraftService, pageID, kind string) *types.ResearchWritingEvent {
	t.Helper()
	events, err := s.WritingEvents(ctx, "research-kb", pageID)
	require.NoError(t, err)
	for _, event := range events {
		if event.Kind == kind {
			return event
		}
	}
	t.Fatalf("missing %s event", kind)
	return nil
}
func writingExtractionModel() *writingChatFixture {
	return &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: `{"rules":[{"name":"短段落","instruction":"每段只阐明一个判断，优先使用短段落。"}]}`, FinishReason: "stop"}}}
}

func TestResearchWritingJournalCapturesActualSavesAndFailedPrompts(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原文"})
	require.NoError(t, err)
	created := writingEventOfKind(t, ctx, s, page.ID, "create")
	require.Equal(t, 0, created.BaseVersion)
	require.Equal(t, 1, created.ResultVersion)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "人工修改"})
	require.NoError(t, err)
	saved := writingEventOfKind(t, ctx, s, page.ID, "save")
	require.Empty(t, saved.Instruction)
	require.Equal(t, "原文", saved.BaseContent)
	require.Equal(t, "人工修改", saved.Content)
	unchanged, err := s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 2, Title: page.Title, Content: page.Content})
	require.NoError(t, err)
	require.Equal(t, 2, unchanged.Version)
	events, err := s.WritingEvents(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	count := len(events)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "stale"})
	require.ErrorIs(t, err, ErrResearchConflict)
	events, err = s.WritingEvents(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Len(t, events, count)
	s.models = researchModelFixture{model: &writingFailureChat{}}
	prompt := "请保留全部背景，只缩短段落。"
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 2, Instruction: prompt, ModelID: "model"})
	require.Error(t, err)
	submitted := writingEventOfKind(t, ctx, s, page.ID, "prompt")
	failed := writingEventOfKind(t, ctx, s, page.ID, "generation_failed")
	require.Equal(t, prompt, submitted.Instruction)
	require.Equal(t, "submitted", submitted.Status)
	require.Equal(t, submitted.ID, failed.PromptEventID)
	require.Equal(t, "failed", failed.Status)
	require.NotContains(t, failed.Error, "SECRET_TOKEN")
	require.NotContains(t, failed.Error, "provider.example")
	require.Equal(t, "人工修改", failed.BaseContent)
	require.Empty(t, failed.Content)
}

func TestResearchWritingCreateAndSaveJournalFailuresRollbackTogether(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	require.NoError(t, s.db.Exec(`CREATE TRIGGER reject_writing_journal BEFORE INSERT ON research_writing_events BEGIN SELECT RAISE(ABORT, 'journal failed'); END;`).Error)
	_, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "must rollback", Content: "content"})
	require.Error(t, err)
	var count int64
	require.NoError(t, s.db.Model(&types.WikiPage{}).Where("slug LIKE ?", "research/%").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, s.db.Exec("DROP TRIGGER reject_writing_journal").Error)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "draft", Content: "original"})
	require.NoError(t, err)
	require.NoError(t, s.db.Exec(`CREATE TRIGGER reject_writing_journal BEFORE INSERT ON research_writing_events BEGIN SELECT RAISE(ABORT, 'journal failed'); END;`).Error)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "should rollback"})
	require.Error(t, err)
	current, err := s.Get(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Equal(t, 1, current.Version)
	require.Equal(t, "original", current.Content)
}

func TestResearchWritingManualEventExtractionReusesPromptsAndIsIdempotent(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "背景很长，末尾结论。", TemplateID: "brief"})
	require.NoError(t, err)
	s.models = researchModelFixture{model: &writingFailureChat{}}
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "失败提示词：结论先行", ModelID: "model"})
	require.Error(t, err)
	model := writingExtractionModel()
	s.models = researchModelFixture{model: model}
	model.response = &types.ChatResponse{Content: "不需要的候选稿", FinishReason: "stop"}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "拒绝提示词：删除背景", ModelID: "model"})
	require.NoError(t, err)
	_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, false)
	require.NoError(t, err)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "先给结论，再保留背景。"})
	require.NoError(t, err)
	event := writingEventOfKind(t, ctx, s, page.ID, "save")
	model.response = writingExtractionModel().response
	rules, err := s.ExtractWritingEventRules(ctx, "research-kb", page.ID, event.ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "candidate", rules[0].Status)
	require.Equal(t, event.ID, rules[0].SourceEventID)
	require.Empty(t, rules[0].SourceProposalID)
	require.Contains(t, model.messages[1].Content, "失败提示词：结论先行")
	require.Contains(t, model.messages[1].Content, "拒绝提示词：删除背景")
	require.Contains(t, model.messages[1].Content, "generation_failed")
	require.Contains(t, model.messages[1].Content, "reject")
	require.NotContains(t, model.messages[1].Content, "SECRET_TOKEN")
	calls := model.calls
	again, err := s.ExtractWritingEventRules(ctx, "research-kb", page.ID, event.ID, ResearchWritingExtractRequest{ModelID: "different"})
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, rules[0].ID, again[0].ID)
	require.Equal(t, calls, model.calls)
	_, err = s.ExtractWritingEventRules(ctx, "research-kb", page.ID, writingEventOfKind(t, ctx, s, page.ID, "reject").ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.ErrorIs(t, err, ErrResearchInput)
}

func TestResearchWritingFeedbackFromPriorAcceptedVersionReachesNextManualSave(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	rule := activeWritingRule(t, ctx, s, "")
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原稿"})
	require.NoError(t, err)
	model := writingExtractionModel()
	model.response = &types.ChatResponse{Content: "本轮候选", FinishReason: "stop"}
	s.models = researchModelFixture{model: model}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "先结论", ModelID: "model", WritingRuleIDs: []string{rule.ID}})
	require.NoError(t, err)
	_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
	require.NoError(t, err)
	_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: "misapplied", Reason: "不能删掉必要铺垫"})
	require.NoError(t, err)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 2, Title: page.Title, Content: "人工补回必要铺垫，再给判断"})
	require.NoError(t, err)
	event := writingEventOfKind(t, ctx, s, page.ID, "save")
	require.Empty(t, event.ProposalID)
	model.response = writingExtractionModel().response
	_, err = s.ExtractWritingEventRules(ctx, "research-kb", page.ID, event.ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.NoError(t, err)
	require.Contains(t, model.messages[1].Content, "misapplied")
	require.Contains(t, model.messages[1].Content, "不能删掉必要铺垫")
	require.Contains(t, model.messages[1].Content, rule.Instruction)
}

func prepareWritingWorkerAccount(t *testing.T, s *ResearchDraftService) {
	t.Helper()
	require.NoError(t, s.db.AutoMigrate(&types.User{}, &types.TenantMember{}))
	require.NoError(t, s.db.Create(&types.User{ID: "author", Username: "author", Email: "author@example.invalid", IsActive: true}).Error)
	require.NoError(t, s.db.Create(&types.TenantMember{TenantID: 1, UserID: "author", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}).Error)
}

func TestResearchWritingAutomaticLearningToggleWorkerAndRevocation(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	prepareWritingWorkerAccount(t, s)
	queue := &researchQueueFixture{}
	s.task = queue
	model := writingExtractionModel()
	s.models = researchModelFixture{model: model}
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原稿"})
	require.NoError(t, err)
	setting, err := s.WritingLearning(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.False(t, setting.Enabled)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "关闭时人工改稿"})
	require.NoError(t, err)
	require.Nil(t, queue.task)
	_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: true, ModelID: "model"})
	require.NoError(t, err)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 2, Title: page.Title, Content: "开启后人工改稿"})
	require.NoError(t, err)
	require.NotNil(t, queue.task)
	require.NoError(t, s.HandleResearchWritingLearning(context.Background(), queue.task))
	require.Equal(t, 1, model.calls)
	require.NoError(t, s.HandleResearchWritingLearning(context.Background(), queue.task))
	require.Equal(t, 1, model.calls)
	setting, err = s.WritingLearning(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", setting.Status)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 3, Title: page.Title, Content: "排队后撤销权限"})
	require.NoError(t, err)
	require.NoError(t, s.db.Model(&types.TenantMember{}).Where("user_id = ?", "author").Update("status", types.TenantMemberStatusSuspended).Error)
	require.Error(t, s.HandleResearchWritingLearning(context.Background(), queue.task))
	require.Equal(t, 1, model.calls)
	var job types.ResearchWritingExtraction
	require.NoError(t, s.db.First(&job, "event_id = ?", writingEventOfKind(t, ctx, s, page.ID, "save").ID).Error)
	require.Equal(t, "failed", job.Status)
	current, err := s.Get(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Equal(t, page.Content, current.Content)
}

func TestResearchWritingAutomaticFailureAndDisableDoNotRollbackDraft(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	prepareWritingWorkerAccount(t, s)
	queue := &researchQueueFixture{}
	s.task = queue
	s.models = writingNoModels{}
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原稿"})
	require.NoError(t, err)
	_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: true})
	require.NoError(t, err)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "没有模型也保留人工修改"})
	require.NoError(t, err)
	require.ErrorIs(t, s.HandleResearchWritingLearning(context.Background(), queue.task), ErrResearchInput)
	setting, err := s.WritingLearning(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", setting.Status)
	require.Contains(t, setting.LastError, "configure a KnowledgeQA model")
	event := writingEventOfKind(t, ctx, s, page.ID, "save")
	model := writingExtractionModel()
	s.models = researchModelFixture{model: model}
	_, err = s.ExtractWritingEventRules(ctx, "research-kb", page.ID, event.ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.NoError(t, err)
	require.Equal(t, 1, model.calls)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 2, Title: page.Title, Content: "关闭前已排队"})
	require.NoError(t, err)
	_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: false})
	require.NoError(t, err)
	require.Error(t, s.HandleResearchWritingLearning(context.Background(), queue.task))
	require.Equal(t, 1, model.calls)
	before := queue.task
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 3, Title: page.Title, Content: "关闭后仅采集"})
	require.NoError(t, err)
	require.Same(t, before, queue.task)
}

func TestResearchWritingLongJournalAndOriginalSourceRevocation(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "第0稿"})
	require.NoError(t, err)
	for i := 1; i <= 65; i++ {
		page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: page.Version, Title: page.Title, Content: fmt.Sprintf("第%d稿", i)})
		require.NoError(t, err)
	}
	events, err := s.WritingEvents(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Len(t, events, 66)
	s.models = researchModelFixture{model: writingExtractionModel()}
	rules, err := s.ExtractWritingEventRules(ctx, "research-kb", page.ID, events[0].ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	_, err = s.DecideWritingRule(ctx, "research-kb", rules[0].ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "active"})
	require.NoError(t, err)
	// Simulate historical evidence becoming unavailable while the current page's
	// refs no longer mention it. Both journal text and derived rules must deny.
	require.NoError(t, s.db.Model(&types.ResearchWritingEvent{}).Where("id = ?", events[len(events)-1].ID).Update("source_refs", types.StringArray{"withdrawn|old evidence"}).Error)
	_, err = s.WritingEvents(ctx, "research-kb", page.ID)
	require.Error(t, err)
	visible, err := s.WritingRules(ctx, "research-kb")
	require.NoError(t, err)
	require.Empty(t, visible) // Inaccessible rules are omitted without exposing their text.
}

func TestResearchWritingAutomaticSettingsChangesInvalidateOldJobs(t *testing.T) {
	for _, change := range []string{"disable-and-enable", "model"} {
		t.Run(change, func(t *testing.T) {
			ctx, s := researchWritingHarness(t)
			prepareWritingWorkerAccount(t, s)
			queue := &researchQueueFixture{}
			s.task = queue
			model := writingExtractionModel()
			s.models = researchModelFixture{model: model}
			page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原稿"})
			require.NoError(t, err)
			_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: true, ModelID: "old-model"})
			require.NoError(t, err)
			page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "已经排队的旧修改"})
			require.NoError(t, err)
			oldTask := queue.task
			require.NotNil(t, oldTask)
			if change == "disable-and-enable" {
				_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: false})
				require.NoError(t, err)
			}
			_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: true, ModelID: "new-model"})
			require.NoError(t, err)
			require.ErrorIs(t, s.HandleResearchWritingLearning(context.Background(), oldTask), asynq.SkipRetry)
			require.Zero(t, model.calls)
			status, err := s.WritingLearning(ctx, "research-kb", page.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", status.Status)
			require.Contains(t, status.LastError, "settings changed")
			// New edits use the current setting and remain eligible.
			_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 2, Title: page.Title, Content: "重新开启后的新修改"})
			require.NoError(t, err)
			require.NoError(t, s.HandleResearchWritingLearning(context.Background(), queue.task))
			require.Equal(t, 1, model.calls)
		})
	}
}

func TestResearchWritingRuleEventCyclesAndInferenceRevocation(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	prepareWritingWorkerAccount(t, s)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原稿"})
	require.NoError(t, err)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "改稿"})
	require.NoError(t, err)
	event := writingEventOfKind(t, ctx, s, page.ID, "save")
	rule, err := s.CreateWritingRule(ctx, "research-kb", ResearchWritingRuleRequest{Name: "循环", Instruction: "用短句"})
	require.NoError(t, err)
	require.NoError(t, s.db.Model(rule).Updates(map[string]any{"source_page_id": page.ID, "source_event_id": event.ID}).Error)
	raw, _ := json.Marshal([]types.ResearchWritingRuleSnapshot{{ID: rule.ID, Version: 1, Name: rule.Name, Instruction: rule.Instruction}})
	require.NoError(t, s.db.Model(event).Update("writing_rules", types.JSON(raw)).Error)
	_, err = s.WritingRules(ctx, "research-kb")
	require.ErrorIs(t, err, ErrResearchInput)
	require.NoError(t, s.db.Model(event).Update("writing_rules", types.JSON(`[]`)).Error)
	queue := &researchQueueFixture{}
	s.task = queue
	_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: true, ModelID: "model"})
	require.NoError(t, err)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 2, Title: page.Title, Content: "推理中停用账号"})
	require.NoError(t, err)
	model := writingExtractionModel()
	model.beforeReturn = func() {
		require.NoError(t, s.db.Model(&types.User{}).Where("id = ?", "author").Update("is_active", false).Error)
	}
	s.models = researchModelFixture{model: model}
	require.ErrorIs(t, s.HandleResearchWritingLearning(context.Background(), queue.task), access.ErrForbidden)
	var count int64
	require.NoError(t, s.db.Model(&types.ResearchWritingRule{}).Where("source_event_id <> ? AND id <> ?", "", rule.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestResearchWritingExpiredWorkerCannotFailNewLease(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原稿"})
	require.NoError(t, err)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "改稿"})
	require.NoError(t, err)
	event := writingEventOfKind(t, ctx, s, page.ID, "save")
	model := writingExtractionModel()
	newLease := time.Now().Add(10 * time.Minute)
	model.beforeReturn = func() {
		require.NoError(t, s.db.Model(&types.ResearchWritingExtraction{}).Where("event_id = ?", event.ID).Updates(map[string]any{"lease_until": newLease, "status": "processing"}).Error)
	}
	s.models = researchModelFixture{model: model}
	_, err = s.ExtractWritingEventRules(ctx, "research-kb", page.ID, event.ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.ErrorIs(t, err, ErrResearchConflict)
	var job types.ResearchWritingExtraction
	require.NoError(t, s.db.First(&job, "event_id = ?", event.ID).Error)
	require.Equal(t, "processing", job.Status)
	require.NotNil(t, job.LeaseUntil)
	require.WithinDuration(t, newLease, *job.LeaseUntil, time.Millisecond)
}

// Keep contract regressions explicit: journal rows have complete source text,
// but extraction must reject rather than silently truncate a large interval.
func TestResearchWritingEventContextBudgetDoesNotPartiallyLearn(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: strings.Repeat("原", 21000)})
	require.NoError(t, err)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: strings.Repeat("改", 21000)})
	require.NoError(t, err)
	model := writingExtractionModel()
	s.models = researchModelFixture{model: model}
	_, err = s.ExtractWritingEventRules(ctx, "research-kb", page.ID, writingEventOfKind(t, ctx, s, page.ID, "save").ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.ErrorIs(t, err, ErrResearchInput)
	require.Zero(t, model.calls)
}

func TestResearchWritingHistoryCursorAndExactSourceRemainReachable(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "初稿"})
	require.NoError(t, err)
	original := writingEventOfKind(t, ctx, s, page.ID, "create")
	for i := 1; i <= 105; i++ {
		page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: page.Version, Title: page.Title, Content: fmt.Sprintf("第%d次", i)})
		require.NoError(t, err)
	}
	first, err := s.WritingEventsPage(ctx, "research-kb", page.ID, "")
	require.NoError(t, err)
	require.Len(t, first, 100)
	second, err := s.WritingEventsPage(ctx, "research-kb", page.ID, first[len(first)-1].ID)
	require.NoError(t, err)
	require.Len(t, second, 6)
	require.Equal(t, original.ID, second[len(second)-1].ID)
	exact, err := s.WritingEvent(ctx, "research-kb", page.ID, original.ID)
	require.NoError(t, err)
	require.Equal(t, "初稿", exact.Content)
	other, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "另一稿"})
	require.NoError(t, err)
	_, err = s.WritingEventsPage(ctx, "research-kb", other.ID, original.ID)
	require.ErrorIs(t, err, access.ErrNotFound)
	_, err = s.WritingEvent(ctx, "research-kb", other.ID, original.ID)
	require.ErrorIs(t, err, access.ErrNotFound)
}

func TestResearchWritingApprovedRulesSurviveGrowingCandidateQueue(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	active := activeWritingRule(t, ctx, s, "")
	candidates := make([]*types.ResearchWritingRule, 210)
	for i := range candidates {
		candidates[i] = &types.ResearchWritingRule{ID: fmt.Sprintf("candidate-%d", i), TenantID: 1, KnowledgeBaseID: "research-kb", Name: "待确认经验", Instruction: "短句", Status: "candidate", Version: 1, CreatedBy: "author", CreatedAt: time.Now().Add(time.Hour)}
	}
	require.NoError(t, s.db.Create(&candidates).Error)
	rules, err := s.WritingRules(ctx, "research-kb")
	require.NoError(t, err)
	require.Len(t, rules, 200)
	require.Equal(t, active.ID, rules[0].ID)
}

func TestResearchWritingExtractedRuleRechecksPromptContextSources(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "背景后结论"})
	require.NoError(t, err)
	s.models = researchModelFixture{model: &writingFailureChat{}}
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "先结论再背景", ModelID: "model"})
	require.Error(t, err)
	prompt := writingEventOfKind(t, ctx, s, page.ID, "prompt")
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "先结论再背景"})
	require.NoError(t, err)
	event := writingEventOfKind(t, ctx, s, page.ID, "save")
	s.models = researchModelFixture{model: writingExtractionModel()}
	rules, err := s.ExtractWritingEventRules(ctx, "research-kb", page.ID, event.ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Contains(t, string(rules[0].Metadata), prompt.ID)
	// The save's parent chain does not contain this failed prompt. Its recorded
	// extraction context must still be rechecked whenever this rule is read.
	require.NoError(t, s.db.Model(prompt).Update("source_refs", types.StringArray{"withdrawn|withdrawn context evidence"}).Error)
	visible, err := s.WritingRules(ctx, "research-kb")
	require.NoError(t, err)
	require.Empty(t, visible)
	// Retrying a successful extraction must enforce the same provenance
	// boundary as listing it; the idempotency shortcut cannot expose it.
	retried, err := s.ExtractWritingEventRules(ctx, "research-kb", page.ID, event.ID, ResearchWritingExtractRequest{ModelID: "model"})
	require.Error(t, err)
	require.Nil(t, retried)
}

func TestResearchWritingQueuedSourceRevocationHasVisibleFailure(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	prepareWritingWorkerAccount(t, s)
	queue := &researchQueueFixture{}
	s.task = queue
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", Content: "原稿"})
	require.NoError(t, err)
	_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: true, ModelID: "model"})
	require.NoError(t, err)
	_, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: 1, Title: page.Title, Content: "队列期间撤销访问"})
	require.NoError(t, err)
	event := writingEventOfKind(t, ctx, s, page.ID, "save")
	s.kb = researchKBAccessFixture{kbs: map[string]*types.KnowledgeBase{}}
	require.ErrorIs(t, s.HandleResearchWritingLearning(context.Background(), queue.task), access.ErrNotFound)
	var job types.ResearchWritingExtraction
	require.NoError(t, s.db.First(&job, "event_id = ?", event.ID).Error)
	require.Equal(t, "failed", job.Status)
	require.Contains(t, job.Error, "access was revoked")
	var current types.WikiPage
	require.NoError(t, s.db.First(&current, "id = ?", page.ID).Error)
	require.Equal(t, "队列期间撤销访问", current.Content)
}
