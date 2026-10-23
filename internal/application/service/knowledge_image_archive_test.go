package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type imageArchiveRepo struct {
	*embedFailureKnowledgeRepo
	versions map[string]*types.ResearchSourceVersion
	fail     bool
}

func (r *imageArchiveRepo) FindResearchSourceVersion(_ context.Context, _ uint64, _ string, hash string) (*types.ResearchSourceVersion, error) {
	return r.versions[hash], nil
}
func (r *imageArchiveRepo) CreateResearchSourceVersion(_ context.Context, v *types.ResearchSourceVersion) error {
	if r.fail {
		return errors.New("storage unavailable")
	}
	r.versions[v.ContentHash] = v
	return nil
}

type imageArchiveKBService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s imageArchiveKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func TestResearchImageArchiveOnlyIsExplicitAndImageSpecific(t *testing.T) {
	kb := &types.KnowledgeBase{VLMConfig: types.VLMConfig{Enabled: true, ModelID: "vlm"}, ImageProcessingConfig: types.ImageProcessingConfig{ImageAttrsEnabled: true, ImageVectorEnabled: true}}
	archive, legacyMultimodal := true, true
	overrides := &types.KnowledgeProcessOverrides{ImageArchiveOnly: &archive}
	eff, err := resolveFileImportProcessConfig(context.Background(), kb, "png", overrides, &legacyMultimodal)
	require.NoError(t, err)
	require.True(t, eff.ImageArchiveOnly)
	require.False(t, eff.EnableMultimodel)
	require.False(t, eff.ImageAttrsEnabled)
	require.False(t, eff.ImageVectorEnabled)
	require.False(t, eff.SummaryEnabled)
	require.False(t, eff.GraphEnabled)
	require.False(t, eff.QuestionGenerationConfig.Enabled)
	_, err = resolveFileImportProcessConfig(context.Background(), &types.KnowledgeBase{}, "png", overrides, nil)
	require.NoError(t, err)
	_, err = resolveFileImportProcessConfig(context.Background(), &types.KnowledgeBase{}, "png", nil, nil)
	require.ErrorContains(t, err, "VLM")
	for _, fileTypes := range [][]string{nil, {"pdf"}, {"png", "txt"}} {
		require.Error(t, ValidateProcessOverrides(context.Background(), kb, overrides, fileTypes))
	}
}

func TestResearchImageArchiveStoresOriginalAndReferenceWithoutModelsOrChunks(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "storage_failure"}[fail], func(t *testing.T) {
			archive := true
			k := &types.Knowledge{ID: "image", TenantID: 1, KnowledgeBaseID: "kb", Type: "file", FilePath: "local://original.png", FileName: "figure.png", FileType: "png", ParseStatus: types.ParseStatusPending}
			require.NoError(t, k.SetProcessOverrides(&types.KnowledgeProcessOverrides{ImageArchiveOnly: &archive}))
			repo := &imageArchiveRepo{embedFailureKnowledgeRepo: &embedFailureKnowledgeRepo{knowledge: k}, versions: map[string]*types.ResearchSourceVersion{}, fail: fail}
			original := []byte("original image bytes")
			files := &researchSourceFiles{objects: map[string][]byte{k.FilePath: original}}
			kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
			// Nil parser/model/chunk/task services prove this path performs no
			// inference, indexing, text chunk creation or follow-up enrichment.
			svc := &knowledgeService{repo: repo, fileSvc: files, kbService: imageArchiveKBService{kb: kb}, tenantRepo: &summaryRefreshTenantRepo{tenant: &types.Tenant{ID: 1}}}
			payload, err := json.Marshal(types.DocumentProcessPayload{TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: k.ID, FilePath: k.FilePath, FileName: k.FileName, FileType: k.FileType})
			require.NoError(t, err)
			task := asynq.NewTask(types.TypeDocumentProcess, payload)
			err = svc.ProcessDocument(context.Background(), task)
			if fail {
				require.Error(t, err)
				require.Equal(t, types.ParseStatusFailed, k.ParseStatus)
				require.Empty(t, repo.versions)
				require.Len(t, files.objects, 1, "failed archive must keep original and clean partial copies")
				return
			}
			require.NoError(t, err)
			require.Equal(t, types.ParseStatusArchived, k.ParseStatus)
			require.Equal(t, "disabled", k.EnableStatus)
			require.Contains(t, k.ErrorMessage, "不可作为文字证据")
			require.Len(t, repo.versions, 1)
			for _, v := range repo.versions {
				require.Equal(t, original, files.objects[v.OriginalPath])
				require.Len(t, v.ImagePaths, 1)
				require.Equal(t, original, files.objects[v.ImagePaths[0]])
				require.Contains(t, string(files.objects[v.MarkdownPath]), v.ImagePaths[0])
			}
			updates := len(repo.updates)
			require.NoError(t, svc.ProcessDocument(context.Background(), task))
			require.Len(t, repo.updates, updates, "duplicate archive task is terminal and idempotent")
		})
	}
}

func TestResearchDraftCannotUseArchivedImageAsTextEvidence(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	require.NoError(t, s.db.AutoMigrate(&types.Tenant{}))
	s.knowledge = researchKnowledgeAccessFixture{items: map[string]*types.Knowledge{"image": {ID: "image", TenantID: 1, KnowledgeBaseID: "research-kb", Type: "file", FileType: "png", ParseStatus: types.ParseStatusArchived}}}
	_, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "research", SourceIDs: []string{"image"}})
	require.ErrorContains(t, err, "尚无可读文字")
	require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}))
	require.NoError(t, s.db.Create(&types.Knowledge{ID: "image", TenantID: 1, KnowledgeBaseID: "research-kb", Type: "file", FileType: "png", ParseStatus: types.ParseStatusArchived}).Error)
	_, err = s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "all sources", SourceScope: "all", SourceKBID: "research-kb"})
	require.ErrorContains(t, err, "original-only archived")
}

func TestResearchWritingRejectsBareImageReferencesWithoutInference(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	model := &researchChatFixture{}
	s.models = researchModelFixture{model: model}
	s.chunks = researchChunkFixture{chunks: []*types.Chunk{{ID: "image-ref", Content: "![原图](resource://image)"}}}
	_, err := s.generate(ctx, &types.WikiPage{}, []*types.Knowledge{{ID: "image", TenantID: 1, FileType: "png", ParseStatus: types.ParseStatusCompleted}}, &ResearchProposeRequest{ModelID: "model"})
	require.ErrorContains(t, err, "no recognized text")
	require.Zero(t, model.calls)
}

func TestResearchArchivedImageCanExplicitlyResumeRecognition(t *testing.T) {
	h := newReparseHarness(t, types.ParseStatusArchived)
	h.repo.row.FileName, h.repo.row.FileType = "image.png", "png"
	archive, recognize := true, true
	require.NoError(t, h.repo.row.SetProcessOverrides(&types.KnowledgeProcessOverrides{ImageArchiveOnly: &archive}))
	kb, err := h.svc.kbService.GetKnowledgeBaseByID(h.ctx, "kb-1")
	require.NoError(t, err)
	kb.VLMConfig = types.VLMConfig{Enabled: true, ModelID: "vision-model"}
	archive = false
	result, err := h.svc.ReparseKnowledge(h.ctx, h.original.ID, &types.KnowledgeProcessOverrides{ImageArchiveOnly: &archive, EnableMultimodel: &recognize})
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, result.ParseStatus)
	stored, err := result.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, stored.ImageArchiveOnly)
	require.False(t, *stored.ImageArchiveOnly)
	require.Len(t, h.tasks.payloads, 1)
	require.True(t, h.tasks.payloads[0].EnableMultimodel)
}
