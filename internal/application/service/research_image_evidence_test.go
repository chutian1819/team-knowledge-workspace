package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Use the real chunk repository: a fixture that returns every chunk for the
// text-only method would hide the production OCR/caption omission.
func researchImageEvidenceHarness(t *testing.T, recognized []types.ChunkType) (context.Context, *ResearchDraftService, *types.Knowledge, *types.WikiPage, *writingChatFixture) {
	t.Helper()
	ctx, s := researchWritingHarness(t)
	require.NoError(t, s.db.AutoMigrate(&types.Tenant{}, &types.Chunk{}, &types.ResearchSourceVersion{}))
	source := &types.Knowledge{ID: "image", TenantID: 1, KnowledgeBaseID: "research-kb", Title: "Recognized diagram",
		FileType: "png", Type: "file", ParseStatus: types.ParseStatusCompleted, FileHash: "original-image-hash"}
	s.knowledge = researchKnowledgeAccessFixture{items: map[string]*types.Knowledge{source.ID: source}}
	s.chunks = repository.NewChunkRepository(s.db)
	chunks := []*types.Chunk{
		{ID: "image-reference", TenantID: 1, KnowledgeID: source.ID, KnowledgeBaseID: "research-kb", ChunkType: types.ChunkTypeText, Content: "![原图](resource://image)"},
		{ID: "vector", TenantID: 1, KnowledgeID: source.ID, KnowledgeBaseID: "research-kb", ChunkType: types.ChunkTypeImageVector, Content: "EXCLUDED_VECTOR"},
		{ID: "summary", TenantID: 1, KnowledgeID: source.ID, KnowledgeBaseID: "research-kb", ChunkType: types.ChunkTypeSummary, Content: "EXCLUDED_SUMMARY"},
		{ID: "foreign-ocr", TenantID: 2, KnowledgeID: source.ID, KnowledgeBaseID: "research-kb", ChunkType: types.ChunkTypeImageOCR, Content: "EXCLUDED_FOREIGN_TENANT"},
		{ID: "other-ocr", TenantID: 1, KnowledgeID: "other-image", KnowledgeBaseID: "research-kb", ChunkType: types.ChunkTypeImageOCR, Content: "EXCLUDED_OTHER_SOURCE"},
	}
	var output []string
	for index, kind := range recognized {
		content := "识别文字：感知、决策、执行形成反馈闭环。"
		if kind == types.ChunkTypeImageCaption {
			content = "图像描述：图中多个模块通过带方向的箭头相连。"
		}
		chunks = append(chunks, &types.Chunk{ID: kind, TenantID: 1, KnowledgeID: source.ID, KnowledgeBaseID: "research-kb", ChunkType: kind, Content: content, ChunkIndex: index + 1})
		output = append(output, "[图片证据](knowledge://image/chunks/"+kind+")")
	}
	require.NoError(t, s.chunks.CreateChunks(ctx, chunks))
	require.NoError(t, s.db.Create(&types.ResearchSourceVersion{ID: "recognized-capture", TenantID: 1, KnowledgeID: source.ID,
		ContentHash: "recognized-image-hash", CapturedAt: time.Now()}).Error)
	model := &writingChatFixture{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: strings.Join(output, "\n"), FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Image research", SourceIDs: []string{source.ID}})
	require.NoError(t, err)
	return ctx, s, source, page, model
}

func TestResearchRecognizedImageEvidenceCanBeCitedAndAccepted(t *testing.T) {
	for _, recognized := range [][]types.ChunkType{{types.ChunkTypeImageOCR}, {types.ChunkTypeImageCaption}, {types.ChunkTypeImageOCR, types.ChunkTypeImageCaption}} {
		t.Run(strings.Join(recognized, "+"), func(t *testing.T) {
			ctx, s, source, page, model := researchImageEvidenceHarness(t, recognized)
			// This global default remains text-only despite the research change.
			textChunks, err := s.chunks.ListChunksByKnowledgeID(ctx, source.TenantID, source.ID)
			require.NoError(t, err)
			require.Len(t, textChunks, 1)
			require.Equal(t, "image-reference", textChunks[0].ID)
			proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: page.Version, ModelID: "native-model", Instruction: "引用图片识别结果"})
			require.NoError(t, err)
			require.Equal(t, 1, model.calls)
			for _, kind := range recognized {
				require.Contains(t, model.messages[1].Content, "CHUNK "+kind)
				require.Contains(t, proposal.Content, "knowledge://image/chunks/"+kind)
			}
			require.NotContains(t, model.messages[1].Content, "EXCLUDED_")
			var versions map[string]map[string]any
			require.NoError(t, json.Unmarshal(proposal.SourceVersions, &versions))
			require.Equal(t, "recognized-capture", versions[source.ID]["capture_version_id"])
			require.Equal(t, "recognized-image-hash", versions[source.ID]["content_hash"])
			accepted, err := s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
			require.NoError(t, err)
			require.Equal(t, proposal.Content, accepted.(*types.WikiPage).Content)
		})
	}
}

func TestResearchImageEvidenceKeepsArchiveAndCitationGuards(t *testing.T) {
	t.Run("unrecognized_original", func(t *testing.T) {
		ctx, s, source, page, model := researchImageEvidenceHarness(t, nil)
		_, err := s.generate(ctx, page, []*types.Knowledge{source}, &ResearchProposeRequest{ModelID: "native-model"})
		require.ErrorContains(t, err, "no recognized text")
		require.Zero(t, model.calls)
	})
	t.Run("archived_even_with_old_recognition_chunks", func(t *testing.T) {
		ctx, s, source, page, model := researchImageEvidenceHarness(t, []types.ChunkType{types.ChunkTypeImageOCR})
		source.ParseStatus = types.ParseStatusArchived
		_, err := s.generate(ctx, page, []*types.Knowledge{source}, &ResearchProposeRequest{ModelID: "native-model"})
		require.ErrorContains(t, err, "尚无可读文字")
		require.Zero(t, model.calls)
	})
	t.Run("unreadable_chunk_citation", func(t *testing.T) {
		ctx, s, source, page, model := researchImageEvidenceHarness(t, []types.ChunkType{types.ChunkTypeImageOCR})
		model.response.Content = "[不可读向量](knowledge://image/chunks/vector)"
		_, err := s.generate(ctx, page, []*types.Knowledge{source}, &ResearchProposeRequest{ModelID: "native-model"})
		require.ErrorContains(t, err, "unverified source reference")
	})
}

func TestResearchImageEvidenceVersionChangesInvalidateProposal(t *testing.T) {
	for _, phase := range []string{"during_generation", "before_acceptance"} {
		t.Run(phase, func(t *testing.T) {
			ctx, s, source, page, model := researchImageEvidenceHarness(t, []types.ChunkType{types.ChunkTypeImageOCR, types.ChunkTypeImageCaption})
			changeCapture := func() {
				require.NoError(t, s.db.Create(&types.ResearchSourceVersion{ID: "new-capture", TenantID: 1, KnowledgeID: source.ID,
					ContentHash: "new-recognition", CapturedAt: time.Now().Add(time.Second)}).Error)
			}
			if phase == "during_generation" {
				model.beforeReturn = changeCapture
			}
			proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: page.Version, ModelID: "native-model", Instruction: "引用图片识别结果"})
			if phase == "during_generation" {
				require.ErrorIs(t, err, ErrResearchConflict)
				require.Nil(t, proposal)
			} else {
				require.NoError(t, err)
				changeCapture()
				_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
				require.ErrorIs(t, err, ErrResearchConflict)
			}
			current, err := s.Get(ctx, "research-kb", page.ID)
			require.NoError(t, err)
			require.Equal(t, page.Version, current.Version)
		})
	}
}
