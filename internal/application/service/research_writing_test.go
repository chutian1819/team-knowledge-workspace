package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func researchWritingHarness(t *testing.T) (context.Context, *ResearchDraftService) {
	ctx, s := researchDraftHarness(t)
	require.NoError(t, s.db.AutoMigrate(&types.ResearchWritingRule{}, &types.ResearchWritingFeedback{}))
	return ctx, s
}

type writingChatFixture struct {
	researchChatFixture
	messages     []chat.Message
	beforeReturn func()
}

func (m *writingChatFixture) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	m.messages = messages
	if m.beforeReturn != nil {
		m.beforeReturn()
	}
	return m.response, nil
}

func activeWritingRule(t *testing.T, ctx context.Context, s *ResearchDraftService, templateID string) *types.ResearchWritingRule {
	t.Helper()
	rule, err := s.CreateWritingRule(ctx, "research-kb", ResearchWritingRuleRequest{Name: "结论先行", Instruction: "先写核心结论，再解释依据。", TemplateID: templateID})
	require.NoError(t, err)
	rule, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "active"})
	require.NoError(t, err)
	return rule
}

func acceptedWritingProposal(t *testing.T, ctx context.Context, s *ResearchDraftService) (*types.WikiPage, *types.ResearchProposal) {
	t.Helper()
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "样稿", TemplateID: "brief", Content: "先铺垫背景，再给结论。"})
	require.NoError(t, err)
	candidate := "先给结论，再解释背景。"
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "先写结论", Content: &candidate})
	require.NoError(t, err)
	_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
	require.NoError(t, err)
	proposal.Status = "accepted"
	return page, proposal
}

func TestResearchWritingCandidateReviewCASAndScope(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", TemplateID: "brief"})
	require.NoError(t, err)
	rule, err := s.CreateWritingRule(ctx, "research-kb", ResearchWritingRuleRequest{Name: "结构", Instruction: "采用短段落。", TemplateID: "brief"})
	require.NoError(t, err)
	require.Equal(t, "candidate", rule.Status)
	_, err = s.loadWritingRules(ctx, "research-kb", page, []string{rule.ID})
	require.ErrorIs(t, err, ErrResearchInput)
	edited := "先结论，再用短段落解释。"
	rule, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "active", Instruction: &edited})
	require.NoError(t, err)
	require.Equal(t, 2, rule.Version)
	require.Contains(t, string(rule.Metadata), "approved_by")
	_, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "revoked"})
	require.ErrorIs(t, err, ErrResearchConflict)
	_, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 2, Status: "active", Instruction: &edited})
	require.ErrorIs(t, err, ErrResearchInput)
	_, err = s.loadWritingRules(ctx, "research-kb", &types.WikiPage{}, []string{rule.ID})
	require.ErrorIs(t, err, ErrResearchInput)
	foreign := types.WithCaller(ctx, types.Caller{TenantID: 2, UserID: "other", Role: types.TenantRoleOwner})
	_, err = s.WritingRules(foreign, "research-kb")
	require.ErrorIs(t, err, access.ErrForbidden)
	viewer := types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "viewer", Role: types.TenantRoleViewer})
	_, err = s.CreateWritingRule(viewer, "research-kb", ResearchWritingRuleRequest{Name: "结构", Instruction: "短段落"})
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = s.CreateWritingRule(ctx, "research-kb", ResearchWritingRuleRequest{Name: "结构", Instruction: "短段落", TemplateID: "unknown"})
	require.ErrorIs(t, err, ErrResearchInput)
	rule, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 2, Status: "revoked"})
	require.NoError(t, err)
	require.Equal(t, 3, rule.Version)
	require.Contains(t, string(rule.Metadata), "revoked_by")
	_, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 3, Status: "active"})
	require.ErrorIs(t, err, ErrResearchConflict)
}

func TestResearchWritingNativePromptSnapshotAndForgery(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	rule := activeWritingRule(t, ctx, s, "brief")
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件", TemplateID: "brief"})
	require.NoError(t, err)
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "完整候选稿。", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "本轮保留背景顺序。", ModelID: "native-model", WritingRuleIDs: []string{rule.ID}})
	require.NoError(t, err)
	require.Contains(t, model.messages[0].Content, rule.Instruction)
	require.Contains(t, model.messages[0].Content, "优先级低于事实证据")
	require.Contains(t, model.messages[1].Content, "本轮保留背景顺序")
	var snapshots []types.ResearchWritingRuleSnapshot
	require.NoError(t, json.Unmarshal(proposal.WritingRules, &snapshots))
	require.Equal(t, []types.ResearchWritingRuleSnapshot{{ID: rule.ID, Version: 2, Name: rule.Name, Instruction: rule.Instruction}}, snapshots)
	fake := "manually supplied"
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Content: &fake, WritingRuleIDs: []string{rule.ID}})
	require.ErrorIs(t, err, ErrResearchInput)
	require.Equal(t, 1, model.calls)
}

func TestResearchWritingRevocationDuringGenerationAndBeforeAccept(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	rule := activeWritingRule(t, ctx, s, "")
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "稿件"})
	require.NoError(t, err)
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "候选稿。", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	model.beforeReturn = func() {
		_, revokeErr := s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 2, Status: "revoked"})
		require.NoError(t, revokeErr)
	}
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "生成", ModelID: "native", WritingRuleIDs: []string{rule.ID}})
	require.ErrorIs(t, err, ErrResearchConflict)
	var count int64
	require.NoError(t, s.db.Model(&types.ResearchProposal{}).Count(&count).Error)
	require.Zero(t, count)
	model.beforeReturn = nil
	rule = activeWritingRule(t, ctx, s, "")
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "生成", ModelID: "native", WritingRuleIDs: []string{rule.ID}})
	require.NoError(t, err)
	_, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 2, Status: "revoked"})
	require.NoError(t, err)
	_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
	require.ErrorIs(t, err, ErrResearchConflict)
	rows, err := s.Proposals(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "pending", rows[0].Status)
}

func TestResearchWritingExtractUsesAcceptedRealFeedbackAndStaysCandidate(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	rule := activeWritingRule(t, ctx, s, "brief")
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "真实样稿", TemplateID: "brief", Content: "背景在前"})
	require.NoError(t, err)
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "结论在前，背景在后。", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "采用所选风格", ModelID: "native", WritingRuleIDs: []string{rule.ID}})
	require.NoError(t, err)
	_, err = s.ExtractWritingRules(ctx, "research-kb", page.ID, ResearchWritingExtractRequest{ProposalID: proposal.ID, ModelID: "native"})
	require.ErrorIs(t, err, ErrResearchInput)
	_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
	require.NoError(t, err)
	_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: "misapplied", Reason: "研究方法部分仍应先说明口径。"})
	require.NoError(t, err)
	model.response = &types.ChatResponse{Content: `{"rules":[{"name":"按段落用途调整","instruction":"摘要先写结论，方法段先说明口径。"}]}`, FinishReason: "stop"}
	rules, err := s.ExtractWritingRules(ctx, "research-kb", page.ID, ResearchWritingExtractRequest{ProposalID: proposal.ID, ModelID: "native"})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "candidate", rules[0].Status)
	require.Equal(t, "brief", rules[0].TemplateID)
	require.Equal(t, page.ID, rules[0].SourcePageID)
	require.Contains(t, model.messages[1].Content, "misapplied")
	require.Contains(t, model.messages[1].Content, "研究方法部分仍应先说明口径")
	require.Contains(t, model.messages[1].Content, rule.ID)
	require.Contains(t, model.messages[0].Content, "不学习事实")
	require.Contains(t, string(rules[0].Metadata), "sample_sha256")
	require.Contains(t, string(rules[0].Metadata), "extraction_prompt")
	_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: "useful", Reason: "后续另一次评价"})
	require.NoError(t, err)
	require.Contains(t, string(rules[0].Metadata), "misapplied")
	require.NotContains(t, string(rules[0].Metadata), "后续另一次评价")
	var unchanged types.ResearchWritingRule
	require.NoError(t, s.db.First(&unchanged, "id = ?", rule.ID).Error)
	require.Equal(t, "active", unchanged.Status)
	require.Equal(t, 2, unchanged.Version)
}

func TestResearchWritingExtractRejectsMalformedOutputAtomically(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	page, proposal := acceptedWritingProposal(t, ctx, s)
	model := &writingChatFixture{}
	s.models = researchModelFixture{model: model}
	outputs := []string{"bad JSON", `{}`, `{"rules":null}`, `{"rules":[],"extra":true}`, `{"rules":[]} {}`, `{"rules":[{"name":"a","instruction":"b","status":"active"}]}`, `{"rules":[{"name":"a","instruction":"b"},{"name":"a","instruction":"b"},{"name":"a","instruction":"b"},{"name":"a","instruction":"b"}]}`, `{"rules":[{"name":"a","instruction":"` + strings.Repeat("x", 1201) + `"}]}`}
	for _, output := range outputs {
		model.response = &types.ChatResponse{Content: output, FinishReason: "stop"}
		_, err := s.ExtractWritingRules(ctx, "research-kb", page.ID, ResearchWritingExtractRequest{ProposalID: proposal.ID, ModelID: "native"})
		require.ErrorIs(t, err, ErrResearchInput, output)
	}
	model.response = &types.ChatResponse{Content: `{"rules":[]}`, FinishReason: "length"}
	_, err := s.ExtractWritingRules(ctx, "research-kb", page.ID, ResearchWritingExtractRequest{ProposalID: proposal.ID, ModelID: "native"})
	require.ErrorIs(t, err, ErrResearchInput)
	var count int64
	require.NoError(t, s.db.Model(&types.ResearchWritingRule{}).Count(&count).Error)
	require.Zero(t, count)
	model.response = &types.ChatResponse{Content: `{"rules":[]}`, FinishReason: "stop"}
	rules, err := s.ExtractWritingRules(ctx, "research-kb", page.ID, ResearchWritingExtractRequest{ProposalID: proposal.ID, ModelID: "native"})
	require.NoError(t, err)
	require.Empty(t, rules)
}

func TestResearchWritingFeedbackCannotForgeUsageAndUpsertsPerCaller(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	rule := activeWritingRule(t, ctx, s, "")
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "反馈"})
	require.NoError(t, err)
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "正文", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "生成", ModelID: "native", WritingRuleIDs: []string{rule.ID}})
	require.NoError(t, err)
	_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: "invented", Rating: "useful"})
	require.ErrorIs(t, err, ErrResearchInput)
	_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: "unknown"})
	require.ErrorIs(t, err, ErrResearchInput)
	for _, rating := range []string{"useful", "useless"} {
		_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: rating})
		require.NoError(t, err)
	}
	rows, err := s.WritingFeedback(ctx, "research-kb", page.ID, proposal.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "useless", rows[0].Rating)
	require.True(t, rows[0].IsMine)
	other := types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "another-author", Role: types.TenantRoleOwner})
	_, err = s.SaveWritingFeedback(other, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: "useful"})
	require.NoError(t, err)
	rows, err = s.WritingFeedback(ctx, "research-kb", page.ID, proposal.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	mine := 0
	for _, row := range rows {
		if row.IsMine {
			mine++
		}
	}
	require.Equal(t, 1, mine)
}

func TestResearchWritingTransitiveProvenanceRevocationAndCycleFailClosed(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	originalPage, originalProposal := acceptedWritingProposal(t, ctx, s)
	rule1, err := s.CreateWritingRule(ctx, "research-kb", ResearchWritingRuleRequest{Name: "原始经验", Instruction: "采用短段落。", SourceProposalID: originalProposal.ID})
	require.NoError(t, err)
	rule1, err = s.DecideWritingRule(ctx, "research-kb", rule1.ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "active"})
	require.NoError(t, err)
	derivedPage, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "二次样稿", TemplateID: "brief"})
	require.NoError(t, err)
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "二次稿。", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	derivedProposal, err := s.Propose(ctx, "research-kb", derivedPage.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "改写", ModelID: "native", WritingRuleIDs: []string{rule1.ID}})
	require.NoError(t, err)
	_, err = s.Decide(ctx, "research-kb", derivedPage.ID, derivedProposal.ID, true)
	require.NoError(t, err)
	model.response = &types.ChatResponse{Content: `{"rules":[{"name":"再提炼经验","instruction":"每段围绕单一观点展开。"}]}`, FinishReason: "stop"}
	rules, err := s.ExtractWritingRules(ctx, "research-kb", derivedPage.ID, ResearchWritingExtractRequest{ProposalID: derivedProposal.ID, ModelID: "native"})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	rule2, err := s.DecideWritingRule(ctx, "research-kb", rules[0].ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "active"})
	require.NoError(t, err)
	_, err = s.loadWritingRules(ctx, "research-kb", derivedPage, []string{rule2.ID})
	require.NoError(t, err)
	// Only the grandparent draft loses evidence; the direct source remains
	// source-free, reproducing the permission leak in a one-level check.
	require.NoError(t, s.db.Model(&types.WikiPage{}).Where("id = ?", originalPage.ID).Update("source_refs", types.StringArray{"withdrawn|private"}).Error)
	_, err = s.loadWritingRules(ctx, "research-kb", derivedPage, []string{rule2.ID})
	require.Error(t, err)
	_, err = s.WritingFeedback(ctx, "research-kb", derivedPage.ID, derivedProposal.ID)
	require.Error(t, err)
	require.NoError(t, s.db.Model(&types.WikiPage{}).Where("id = ?", originalPage.ID).Update("source_refs", types.StringArray{}).Error)
	// Deliberately corrupt the otherwise chronological DAG to exercise the
	// defensive cycle guard; no public API can rewrite provenance this way.
	require.NoError(t, s.db.Model(&types.ResearchWritingRule{}).Where("id = ?", rule1.ID).Updates(map[string]any{"source_page_id": derivedPage.ID, "source_proposal_id": derivedProposal.ID}).Error)
	_, err = s.loadWritingRules(ctx, "research-kb", derivedPage, []string{rule2.ID})
	require.ErrorIs(t, err, ErrResearchInput)
	require.Contains(t, err.Error(), "cyclic")
}

func TestResearchWritingRevokedSourceBlocksHistoricalReadsAndFeedback(t *testing.T) {
	ctx, s := researchWritingHarness(t)
	sourcePage, sourceProposal := acceptedWritingProposal(t, ctx, s)
	rule, err := s.CreateWritingRule(ctx, "research-kb", ResearchWritingRuleRequest{Name: "结构", Instruction: "使用短段落", SourceProposalID: sourceProposal.ID})
	require.NoError(t, err)
	rule, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "active"})
	require.NoError(t, err)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "另稿"})
	require.NoError(t, err)
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "正文", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, Instruction: "生成", ModelID: "native", WritingRuleIDs: []string{rule.ID}})
	require.NoError(t, err)
	// The source draft now contains a withdrawn evidence binding. Metadata
	// is changed directly only to model external source revocation in this test.
	require.NoError(t, s.db.Model(&types.WikiPage{}).Where("id = ?", sourcePage.ID).Update("source_refs", types.StringArray{"withdrawn-evidence|private"}).Error)
	independent := activeWritingRule(t, ctx, s, "")
	visible, err := s.WritingRules(ctx, "research-kb")
	require.NoError(t, err)
	require.Len(t, visible, 1)
	require.Equal(t, independent.ID, visible[0].ID)
	_, err = s.Proposals(ctx, "research-kb", page.ID)
	require.Error(t, err)
	_, err = s.WritingFeedback(ctx, "research-kb", page.ID, proposal.ID)
	require.Error(t, err)
	_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: "useful"})
	require.Error(t, err)
	_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
	require.Error(t, err)
	_, err = s.DecideWritingRule(ctx, "research-kb", rule.ID, ResearchWritingRuleDecision{BaseVersion: 2, Status: "revoked"})
	require.Error(t, err)
	_, err = s.loadWritingRules(ctx, "research-kb", page, []string{rule.ID})
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrResearchConflict))
}
