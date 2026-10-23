package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type researchRetrievalKBFixture struct {
	researchKBAccessFixture
	hits   []*types.SearchResult
	params types.SearchParams
	calls  int
	err    error
}

func (f *researchRetrievalKBFixture) HybridSearch(_ context.Context, _ string, params types.SearchParams) ([]*types.SearchResult, error) {
	f.calls++
	f.params = params
	return f.hits, f.err
}
func researchRetrievalHarness(t *testing.T) (context.Context, *ResearchDraftService, *researchRetrievalKBFixture, *types.Knowledge, *writingChatFixture) {
	t.Helper()
	ctx, s := researchWritingHarness(t)
	require.NoError(t, s.db.AutoMigrate(&types.Tenant{}, &types.Chunk{}, &types.ResearchSourceVersion{}))
	kb := &researchRetrievalKBFixture{researchKBAccessFixture: s.kb.(researchKBAccessFixture)}
	kb.kbs["source-kb"] = &types.KnowledgeBase{ID: "source-kb", TenantID: 1, EmbeddingModelID: "embedding", IndexingStrategy: types.IndexingStrategy{VectorEnabled: true}}
	s.kb = kb
	source := &types.Knowledge{ID: "source", TenantID: 1, KnowledgeBaseID: "source-kb", Title: "具身智能证据", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", EmbeddingModelID: "embedding", FileHash: "original-hash"}
	s.knowledge = researchKnowledgeAccessFixture{items: map[string]*types.Knowledge{source.ID: source}}
	s.chunks = repository.NewChunkRepository(s.db)
	chunks := []*types.Chunk{}
	for _, kind := range []string{types.ChunkTypeText, types.ChunkTypeImageOCR, types.ChunkTypeImageCaption, types.ChunkTypeImageVector} {
		chunks = append(chunks, &types.Chunk{ID: kind, TenantID: 1, KnowledgeBaseID: source.KnowledgeBaseID, KnowledgeID: source.ID, ChunkType: kind, Content: "CANONICAL_" + kind, IndexStatus: "ready"})
		kb.hits = append(kb.hits, &types.SearchResult{ID: kind, KnowledgeID: source.ID, Content: "FORGED_SEARCH_BODY", MatchType: types.MatchTypeEmbedding, Score: .9})
	}
	chunks = append(chunks, &types.Chunk{ID: "unused", TenantID: 1, KnowledgeBaseID: source.KnowledgeBaseID, KnowledgeID: source.ID, ChunkType: types.ChunkTypeText, Content: "EXCLUDED_UNRETRIEVED", IndexStatus: "ready"})
	require.NoError(t, s.chunks.CreateChunks(ctx, chunks))
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "[已核实证据](knowledge://source/chunks/text)", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	return ctx, s, kb, source, model
}
func createRetrievedDraft(t *testing.T, ctx context.Context, s *ResearchDraftService) *types.WikiPage {
	t.Helper()
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "语义证据稿", SourceScope: "retrieval", RetrievalKBID: "source-kb", RetrievalQuery: "具身智能有哪些迁移证据？"})
	require.NoError(t, err)
	return page
}
func TestResearchRetrievalUsesCanonicalVectorHitsAndPersistsProvenance(t *testing.T) {
	ctx, s, kb, _, model := researchRetrievalHarness(t)
	page := createRetrievedDraft(t, ctx, s)
	require.True(t, kb.params.DisableKeywordsMatch)
	require.False(t, kb.params.DisableVectorMatch)
	require.True(t, kb.params.SkipContextEnrichment)
	require.Equal(t, 8, kb.params.MatchCount)
	require.Empty(t, kb.params.KnowledgeIDs)
	snapshot, err := researchPageRetrieval(page)
	require.NoError(t, err)
	require.Equal(t, "embedding", snapshot.EmbeddingModelID)
	require.Equal(t, "vector_only", snapshot.Mode)
	require.Len(t, snapshot.Results, 3)
	require.Equal(t, []string{types.ChunkTypeImageVector}, snapshot.ExcludedChunkIDs)
	require.Len(t, page.ChunkRefs, 3)
	for _, hit := range snapshot.Results {
		require.Equal(t, "CANONICAL_"+hit.ChunkType, hit.Content)
		require.Equal(t, researchChunkHash(hit.Content), hit.ContentHash)
	}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: page.Version, ModelID: "native-model", Instruction: "根据检索证据写出结论"})
	require.NoError(t, err)
	require.Equal(t, 1, kb.calls, "frozen evidence is reused without another search")
	require.Len(t, model.messages, 2)
	for _, kind := range []string{types.ChunkTypeText, types.ChunkTypeImageOCR, types.ChunkTypeImageCaption} {
		require.Contains(t, model.messages[1].Content, "CANONICAL_"+kind)
	}
	require.NotContains(t, model.messages[1].Content, "EXCLUDED_")
	require.NotContains(t, model.messages[1].Content, "FORGED_")
	require.NotContains(t, model.messages[1].Content, "CANONICAL_image_vector")
	saved, err := researchRetrievalFromVersions(proposal.SourceVersions)
	require.NoError(t, err)
	require.Equal(t, snapshot, saved)
	accepted, err := s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
	require.NoError(t, err)
	current := accepted.(*types.WikiPage)
	require.Equal(t, 2, current.Version)
	require.Contains(t, researchExportMarkdown(current, []*types.Knowledge{{ID: "source"}}, "md"), "content_sha256")
	var events []types.ResearchWritingEvent
	require.NoError(t, s.db.Where("page_id = ?", page.ID).Find(&events).Error)
	require.GreaterOrEqual(t, len(events), 3)
	for _, event := range events {
		require.Contains(t, string(event.SourceVersions), "_retrieval")
	}
}
func TestResearchRetrievalDoesNotPretendUnindexedOrKeywordDataIsSemantic(t *testing.T) {
	for _, scenario := range []string{"vector_disabled", "no_model", "unindexed_source", "disabled_source", "pending_chunk", "disabled_chunk", "empty", "keyword", "search_failure", "image_only"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, s, kb, source, model := researchRetrievalHarness(t)
			switch scenario {
			case "vector_disabled":
				kb.kbs["source-kb"].IndexingStrategy.VectorEnabled = false
			case "no_model":
				kb.kbs["source-kb"].EmbeddingModelID = ""
			case "unindexed_source":
				source.EmbeddingModelID = "old-model"
			case "disabled_source":
				source.EnableStatus = "disabled"
			case "pending_chunk":
				require.NoError(t, s.db.Model(&types.Chunk{}).Where("id = ?", "text").Update("index_status", "processing").Error)
			case "disabled_chunk":
				require.NoError(t, s.db.Model(&types.Chunk{}).Where("id = ?", "text").Update("is_enabled", false).Error)
			case "empty":
				kb.hits = nil
			case "keyword":
				kb.hits[0].MatchType = types.MatchTypeKeywords
			case "search_failure":
				kb.err = errors.New("embedding unavailable")
			case "image_only":
				kb.hits = kb.hits[3:]
			}
			_, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Must fail", SourceScope: "retrieval", RetrievalKBID: "source-kb", RetrievalQuery: "question"})
			require.Error(t, err)
			require.Zero(t, model.calls)
			var count int64
			require.NoError(t, s.db.Model(&types.WikiPage{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}
func TestResearchRetrievalRejectsUnreturnedCitationsAndEvidenceRaces(t *testing.T) {
	for _, scenario := range []string{"unreturned_citation", "before_generation", "during_generation", "before_acceptance", "source_version_changed", "source_archived", "chunk_disabled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, s, _, source, model := researchRetrievalHarness(t)
			page := createRetrievedDraft(t, ctx, s)
			mutate := func() {
				require.NoError(t, s.db.Model(&types.Chunk{}).Where("id = ?", "text").Update("content", "Changed evidence").Error)
			}
			switch scenario {
			case "unreturned_citation":
				model.response.Content = "[未检索资料](knowledge://source/chunks/unused)"
			case "before_generation":
				mutate()
			case "during_generation":
				model.beforeReturn = mutate
			}
			proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, ModelID: "native-model", Instruction: "核实"})
			if scenario == "unreturned_citation" {
				require.ErrorContains(t, err, "unverified source")
				return
			}
			if scenario == "before_generation" || scenario == "during_generation" {
				require.ErrorIs(t, err, ErrResearchConflict)
				return
			}
			require.NoError(t, err)
			if scenario == "before_acceptance" {
				mutate()
			} else if scenario == "source_archived" {
				source.ParseStatus = types.ParseStatusArchived
			} else if scenario == "chunk_disabled" {
				require.NoError(t, s.db.Model(&types.Chunk{}).Where("id = ?", "text").Update("is_enabled", false).Error)
			} else {
				source.FileHash = "changed-file"
			}
			_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
			require.ErrorIs(t, err, ErrResearchConflict)
			loaded, err := s.Get(ctx, "research-kb", page.ID)
			require.NoError(t, err)
			require.Equal(t, 1, loaded.Version)
		})
	}
}
func TestResearchRetrievalRefreshIsLimitedToLinkedSources(t *testing.T) {
	ctx, s, kb, source, _ := researchRetrievalHarness(t)
	page := createRetrievedDraft(t, ctx, s)
	_, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, ModelID: "native-model", Instruction: "核实", RetrievalQuery: "新的检索问题", RetrievalKBID: "source-kb"})
	require.NoError(t, err)
	require.Equal(t, []string{source.ID}, kb.params.KnowledgeIDs)
	kb.hits[0].KnowledgeID = "unlinked"
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, ModelID: "native-model", Instruction: "核实", RetrievalQuery: "新的检索问题", RetrievalKBID: "source-kb"})
	require.ErrorIs(t, err, access.ErrForbidden)
	kb.kbs["other-kb"] = &types.KnowledgeBase{ID: "other-kb", TenantID: 1, EmbeddingModelID: "embedding", IndexingStrategy: types.IndexingStrategy{VectorEnabled: true}}
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, ModelID: "native-model", RetrievalQuery: "question", RetrievalKBID: "other-kb"})
	require.ErrorContains(t, err, "已关联")
}
func TestResearchRetrievalEnforcesSourcePermissionsAndDestinationAudience(t *testing.T) {
	for _, scenario := range []string{"private_source", "wider_destination", "wrong_chunk_source"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, s, kb, source, _ := researchRetrievalHarness(t)
			if scenario == "wrong_chunk_source" {
				require.NoError(t, s.db.Model(&types.Chunk{}).Where("id = ?", "text").Update("knowledge_id", "other").Error)
			} else {
				source.TenantID = 2
				kb.kbs["source-kb"].TenantID = 2
				require.NoError(t, s.db.Model(&types.Chunk{}).Where("knowledge_id = ?", "source").Update("tenant_id", 2).Error)
				if scenario == "wider_destination" {
					require.NoError(t, s.db.Create(&types.Tenant{ID: 3, Status: "active"}).Error)
					s.shares = researchShareAccessFixture{visible: map[string]map[uint64]bool{"source-kb": {1: true}, "research-kb": {2: true, 3: true}}}
				}
			}
			_, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Forbidden", SourceScope: "retrieval", RetrievalKBID: "source-kb", RetrievalQuery: "question"})
			require.Error(t, err)
		})
	}
}
func TestResearchRetrievalPreservesLegacySelectedSourceFlow(t *testing.T) {
	ctx, s, kb, source, model := researchRetrievalHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Legacy", SourceIDs: []string{source.ID}})
	require.NoError(t, err)
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, ModelID: "native-model", Instruction: "完整阅读资料"})
	require.NoError(t, err)
	require.Zero(t, kb.calls)
	require.Contains(t, model.messages[1].Content, "EXCLUDED_UNRETRIEVED")
	snapshot, err := researchPageRetrieval(page)
	require.NoError(t, err)
	require.Nil(t, snapshot)
}
func TestResearchRetrievalBudgetFailsWithoutTruncation(t *testing.T) {
	ctx, s, kb, _, _ := researchRetrievalHarness(t)
	kb.hits = kb.hits[:1]
	require.NoError(t, s.db.Model(&types.Chunk{}).Where("id = ?", "text").Update("content", strings.Repeat("x", 100001)).Error)
	_, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Too big", SourceScope: "retrieval", RetrievalKBID: "source-kb", RetrievalQuery: "question"})
	require.ErrorContains(t, err, "未截断")
	versions := map[string]any{"source": map[string]any{"file_hash": "a"}}
	raw, _ := json.Marshal(versions)
	require.True(t, researchSourceVersionsEqual(versions, raw))
	with := researchVersionsWithRetrieval(versions, &researchRetrievalSnapshot{Version: 1})
	delete(versions, researchRetrievalKey)
	require.True(t, researchSourceVersionsEqual(versions, with))
}
