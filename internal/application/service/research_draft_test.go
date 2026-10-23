package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func researchDraftHarness(t *testing.T) (context.Context, *ResearchDraftService) {
	t.Helper()
	ctx, native, db := newWikiRevisionTestService(t)
	require.NoError(t, db.AutoMigrate(&types.ResearchProposal{}, &types.ResearchExport{}, &types.ResearchWritingEvent{}, &types.ResearchWritingLearningSetting{}, &types.ResearchWritingExtraction{}))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "author")
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "author", Role: types.TenantRoleOwner})
	ctx = types.WithExecutionTenant(ctx, 1)
	s := &ResearchDraftService{db: db, wiki: native.svc,
		kb: researchKBAccessFixture{kbs: map[string]*types.KnowledgeBase{
			"research-kb": {ID: "research-kb", TenantID: 1, WikiConfig: &types.WikiConfig{ResearchOnly: true}},
		}},
		knowledge: researchKnowledgeAccessFixture{items: map[string]*types.Knowledge{}}}
	return ctx, s
}

func TestResearchVersionConflictPreservesHumanWorkAndProposal(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	draft, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "AI4S", SourceScope: "selected", TemplateID: "one-pager"})
	require.NoError(t, err)
	candidate := "AI candidate"
	proposal, err := s.Propose(ctx, "research-kb", draft.ID, ResearchProposeRequest{BaseVersion: 1, Content: &candidate})
	require.NoError(t, err)
	saved, err := s.Save(ctx, "research-kb", draft.ID, ResearchSaveRequest{BaseVersion: 1, Title: draft.Title, Content: "Human edits"})
	require.NoError(t, err)
	require.Equal(t, 2, saved.Version)
	_, err = s.Decide(ctx, "research-kb", draft.ID, proposal.ID, true)
	require.ErrorIs(t, err, ErrResearchConflict)
	loaded, err := s.Get(ctx, "research-kb", draft.ID)
	require.NoError(t, err)
	require.Equal(t, "Human edits", loaded.Content)
	var pending types.ResearchProposal
	require.NoError(t, s.db.First(&pending, "id = ?", proposal.ID).Error)
	require.Equal(t, "pending", pending.Status)
	_, err = s.Save(ctx, "research-kb", draft.ID, ResearchSaveRequest{BaseVersion: 1, Title: draft.Title, Content: "stale"})
	require.ErrorIs(t, err, ErrResearchConflict)
}

func TestResearchAcceptedProposalAndRevisionCommitTogether(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	draft, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "AI4S", Content: "Original"})
	require.NoError(t, err)
	candidate := "Reviewed candidate"
	proposal, err := s.Propose(ctx, "research-kb", draft.ID, ResearchProposeRequest{BaseVersion: 1, Content: &candidate})
	require.NoError(t, err)
	result, err := s.Decide(ctx, "research-kb", draft.ID, proposal.ID, true)
	require.NoError(t, err)
	require.Equal(t, 2, result.(*types.WikiPage).Version)
	revision, err := s.wiki.GetRevision(ctx, "research-kb", draft.Slug, 1)
	require.NoError(t, err)
	require.Equal(t, "Original", revision.Content)
	reverted, err := s.Revert(ctx, "research-kb", draft.ID, 1, 2)
	require.NoError(t, err)
	require.Equal(t, "Original", reverted.Content)
	require.Equal(t, 3, reverted.Version)
	_, err = s.Decide(ctx, "research-kb", draft.ID, proposal.ID, true)
	require.ErrorIs(t, err, ErrResearchConflict)
}

func TestResearchProtectedFromEveryAutomaticWikiMutation(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	draft, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "AI4S", Content: "Human"})
	require.NoError(t, err)
	for _, source := range []string{types.WikiEditSourcePipeline, types.WikiEditSourceAgent, types.WikiEditSourceUser} {
		t.Run(source, func(t *testing.T) {
			edit := *draft
			edit.Content = "unexpected"
			writeCtx := types.WithWikiEditSource(ctx, source)
			_, err := s.wiki.UpdatePage(writeCtx, &edit)
			require.ErrorIs(t, err, ErrResearchProtected)
			require.ErrorIs(t, s.wiki.UpdatePageMeta(writeCtx, &edit), ErrResearchProtected)
			require.ErrorIs(t, s.wiki.UpdateAutoLinkedContent(writeCtx, &edit), ErrResearchProtected)
			require.ErrorIs(t, s.wiki.DeletePage(writeCtx, "research-kb", draft.Slug), ErrResearchProtected)
		})
	}
	require.False(t, (&types.KnowledgeBase{IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
		WikiConfig: &types.WikiConfig{ResearchOnly: true}}).IsWikiEnabled())
	// Renaming or copying a research page cannot erase its protected purpose.
	copied := *draft
	copied.ID, copied.Slug = "copied-page", "concept/copied"
	_, err = s.wiki.CreatePage(ctx, &copied)
	require.ErrorIs(t, err, ErrResearchProtected)
}

func TestResearchRejectsCrossTenantAndMismatchedPage(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	draft, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Private"})
	require.NoError(t, err)
	foreign := types.WithCaller(context.Background(), types.Caller{TenantID: 2, Role: types.TenantRoleOwner})
	_, err = s.Get(foreign, "research-kb", draft.ID)
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = s.page(ctx, "other-kb", draft.ID)
	require.ErrorIs(t, err, access.ErrNotFound)
}

type researchQueueFixture struct{ task *asynq.Task }

func (q *researchQueueFixture) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.task = task
	return &asynq.TaskInfo{}, nil
}

type researchFileFixture struct {
	interfaces.FileService
	data map[string][]byte
}

func (f *researchFileFixture) SaveBytes(_ context.Context, content []byte, _ uint64, name string, _ bool) (string, error) {
	if f.data == nil {
		f.data = map[string][]byte{}
	}
	f.data[name] = content
	return name, nil
}
func (f *researchFileFixture) GetFile(_ context.Context, name string) (io.ReadCloser, error) {
	if data, ok := f.data[name]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil, errors.New("missing file")
}

type researchConverterFixture struct {
	interfaces.DocumentReader
	fail bool
}

func (c *researchConverterFixture) ExportResearch(context.Context, string, string, string, map[string][]byte) ([]byte, error) {
	if c.fail {
		return nil, errors.New("conversion timeout")
	}
	return []byte("converted document"), nil
}

func TestResearchExportFailureCanRetryWithoutLosingDraft(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	// Worker identity without a human is an authenticated tenant job; human
	// membership checks are covered by the integration/auth suites.
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, Role: types.TenantRoleOwner})
	queue := &researchQueueFixture{}
	files := &researchFileFixture{}
	converter := &researchConverterFixture{fail: true}
	s.task, s.files, s.reader = queue, files, converter
	draft, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Export", Content: "Evidence"})
	require.NoError(t, err)
	export, err := s.RequestExport(ctx, "research-kb", draft.ID, "docx")
	require.NoError(t, err)
	require.Equal(t, types.TypeResearchExport, queue.task.Type())
	require.ErrorContains(t, s.HandleResearchExport(context.Background(), queue.task), "conversion timeout")
	status, err := s.GetExport(ctx, "research-kb", export.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", status.Status)
	require.Empty(t, status.FilePath)
	converter.fail = false
	require.NoError(t, s.HandleResearchExport(context.Background(), queue.task))
	status, err = s.GetExport(ctx, "research-kb", export.ID)
	require.NoError(t, err)
	require.Equal(t, "ready", status.Status)
	current, err := s.Get(ctx, "research-kb", draft.ID)
	require.NoError(t, err)
	require.Equal(t, 1, current.Version)
	require.Equal(t, "Evidence", current.Content)
}

func TestResearchArchivePortableAndRejectsUnsafePaths(t *testing.T) {
	archive := newResearchArchive()
	require.Error(t, archive.add("../secret", []byte("no")))
	require.NoError(t, archive.add("markdown/source.md", []byte("# 中文研究\n原始证据")))
	manifest, err := json.Marshal(map[string]any{"format_version": 1, "markdown": "markdown/source.md"})
	require.NoError(t, err)
	require.NoError(t, archive.add("manifest.json", manifest))
	require.NoError(t, archive.writer.Close())
	zipFile, err := zip.NewReader(bytes.NewReader(archive.buffer.Bytes()), int64(archive.buffer.Len()))
	require.NoError(t, err)
	require.Len(t, zipFile.File, 2)
	file, err := zipFile.Open("markdown/source.md")
	require.NoError(t, err)
	defer file.Close()
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Contains(t, string(content), "原始证据")
	require.Equal(t, "source.docx", archiveFileName("../../source.docx", "source"))
}

type researchModelFixture struct {
	interfaces.ModelService
	model chat.Chat
}

func (m researchModelFixture) GetChatModel(context.Context, string) (chat.Chat, error) {
	return m.model, nil
}

type researchChatFixture struct {
	response *types.ChatResponse
	calls    int
}

func (c *researchChatFixture) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("streaming is not used by research proposals")
}
func (c *researchChatFixture) GetModelID() string   { return "native-model" }
func (c *researchChatFixture) GetModelName() string { return "native-model" }

func (c *researchChatFixture) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	c.calls++
	return c.response, nil
}

type researchChunkFixture struct {
	interfaces.ChunkRepository
	chunks []*types.Chunk
}

func (c researchChunkFixture) ListChunksByKnowledgeID(context.Context, uint64, string) ([]*types.Chunk, error) {
	return c.chunks, nil
}

func (c researchChunkFixture) ListChunksByKnowledgeIDAndTypes(_ context.Context, _ uint64, _ string, chunkTypes []types.ChunkType) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	for _, chunk := range c.chunks {
		kind := chunk.ChunkType
		if kind == "" {
			kind = types.ChunkTypeText // Match the native database default.
		}
		for _, allowed := range chunkTypes {
			if kind == allowed {
				chunks = append(chunks, chunk)
				break
			}
		}
	}
	return chunks, nil
}

func TestResearchAIRefusesTruncationMissingOrInventedEvidence(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	model := &researchChatFixture{}
	s.models = researchModelFixture{model: model}
	s.chunks = researchChunkFixture{chunks: []*types.Chunk{{ID: "chunk-1", Content: "Source evidence"}}}
	sources := []*types.Knowledge{{ID: "source-1", TenantID: 1, Title: "Evidence"}}
	for _, response := range []*types.ChatResponse{
		{Content: "No citation", FinishReason: "stop"},
		{Content: "[fake](knowledge://source-2/chunks/chunk-9)", FinishReason: "stop"},
		{Content: "[broken](knowledge://source-1)", FinishReason: "stop"},
		{Content: "[real](knowledge://source-1/chunks/chunk-1)", FinishReason: "length"},
	} {
		model.response = response
		_, err := s.generate(ctx, &types.WikiPage{Content: "Draft"}, sources, &ResearchProposeRequest{ModelID: "native-model", Instruction: "Revise"})
		require.ErrorIs(t, err, ErrResearchInput)
	}
	model.response = &types.ChatResponse{Content: "[real](knowledge://source-1/chunks/chunk-1)", FinishReason: "stop"}
	content, err := s.generate(ctx, &types.WikiPage{Content: "Draft"}, sources, &ResearchProposeRequest{ModelID: "native-model", Instruction: "Revise"})
	require.NoError(t, err)
	require.Contains(t, content, "chunk-1")
}

func TestResearchAIRejectsOversizedEvidenceBeforeSendingToModel(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	model := &researchChatFixture{}
	s.models = researchModelFixture{model: model}
	s.chunks = researchChunkFixture{chunks: []*types.Chunk{{ID: "chunk", Content: strings.Repeat("x", 120001)}}}
	_, err := s.generate(ctx, &types.WikiPage{}, []*types.Knowledge{{ID: "source"}},
		&ResearchProposeRequest{ModelID: "native-model", Instruction: "Read complete project"})
	require.ErrorIs(t, err, ErrResearchInput)
	require.Equal(t, 0, model.calls)
}

func TestResearchOldAssetExportBecomesInaccessibleAfterSourceMove(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	require.NoError(t, s.db.Exec("CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, s.db.Exec("INSERT INTO knowledges(id, knowledge_base_id) VALUES ('evidence', 'research-kb')").Error)
	export := &types.ResearchExport{KnowledgeBaseID: "research-kb", SourceIDs: types.StringArray{"evidence"}}
	require.NoError(t, s.validateArchiveMembership(ctx, export))
	require.NoError(t, s.db.Exec("UPDATE knowledges SET knowledge_base_id = 'private' WHERE id = 'evidence'").Error)
	require.ErrorIs(t, s.validateArchiveMembership(ctx, export), access.ErrForbidden)
	require.NoError(t, s.db.Exec("DELETE FROM knowledges WHERE id = 'evidence'").Error)
	require.ErrorIs(t, s.validateArchiveMembership(ctx, export), access.ErrForbidden)
}

type researchResourceFixture struct {
	interfaces.ResourceCatalog
	owned bool
}

func (r researchResourceFixture) IsReferencedByKnowledgeBase(context.Context, uint64, string, string) (bool, error) {
	return r.owned, nil
}

func TestResearchImageRequiresExplicitNativeKnowledgeBinding(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	source := &types.Knowledge{TenantID: 1, KnowledgeBaseID: "research-kb"}
	s.catalog = researchResourceFixture{owned: false}
	require.ErrorIs(t, s.validateResearchImageOwnership(ctx, source, "resource://guess"), access.ErrForbidden)
	s.catalog = researchResourceFixture{owned: true}
	require.NoError(t, s.validateResearchImageOwnership(ctx, source, "resource://owned"))
}
