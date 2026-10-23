package service

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type researchReplaceKnowledgeAPI struct {
	interfaces.KnowledgeService
	current *types.Knowledge
	calls   int
}

func (k *researchReplaceKnowledgeAPI) GetKnowledgeByID(context.Context, string) (*types.Knowledge, error) {
	return k.current, nil
}
func (k *researchReplaceKnowledgeAPI) ReplaceKnowledgeFile(context.Context, string, *multipart.FileHeader, string, map[string]string) (*types.Knowledge, error) {
	k.calls++
	return k.current, types.NewDuplicateFileError(k.current)
}

func TestResearchReplaceSourceChecksBindingAndTreatsNativeDuplicateAsSuccess(t *testing.T) {
	bases := captureShareKB{bases: map[string]*types.KnowledgeBase{"owned": {ID: "owned", TenantID: 1, CreatorID: "owner"}}}
	knowledge := &researchReplaceKnowledgeAPI{current: &types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "other"}}
	svc := &ResearchCaptureService{kb: bases, kg: knowledge}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleOwner})
	_, err := svc.ReplaceSourceFile(ctx, "owned", "doc", nil, "")
	require.ErrorIs(t, err, access.ErrForbidden)
	require.Zero(t, knowledge.calls)
	knowledge.current.KnowledgeBaseID = "owned"
	result, err := svc.ReplaceSourceFile(ctx, "owned", "doc", nil, "")
	require.NoError(t, err)
	require.Equal(t, "doc", result.ID)
	require.Equal(t, 1, knowledge.calls)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "other", Role: types.TenantRoleContributor})
	_, err = svc.ReplaceSourceFile(ctx, "owned", "doc", nil, "")
	require.ErrorIs(t, err, access.ErrForbidden)
	require.Equal(t, 1, knowledge.calls)
}

type researchReplaceRepo struct {
	*replaceFileRepo
	archive *researchSourceRepoStub
}

func (r *researchReplaceRepo) FindResearchSourceVersion(ctx context.Context, tenant uint64, id, hash string) (*types.ResearchSourceVersion, error) {
	return r.archive.FindResearchSourceVersion(ctx, tenant, id, hash)
}
func (r *researchReplaceRepo) CreateResearchSourceVersion(ctx context.Context, version *types.ResearchSourceVersion) error {
	return r.archive.CreateResearchSourceVersion(ctx, version)
}

type researchReplaceFiles struct {
	*replaceFileStore
	evidence *researchSourceFiles
	copyErr  error
}

func (f *researchReplaceFiles) GetFile(ctx context.Context, path string) (io.ReadCloser, error) {
	return f.evidence.GetFile(ctx, path)
}
func (f *researchReplaceFiles) CopyFile(ctx context.Context, path string, tenant uint64, knowledge string) (string, error) {
	if f.copyErr != nil {
		return "", f.copyErr
	}
	return f.evidence.CopyFile(ctx, path, tenant, knowledge)
}
func (f *researchReplaceFiles) DeleteFile(ctx context.Context, path string) error {
	if err := f.replaceFileStore.DeleteFile(ctx, path); err != nil {
		return err
	}
	return f.evidence.DeleteFile(ctx, path)
}
func (f *researchReplaceFiles) SaveFile(ctx context.Context, file *multipart.FileHeader, tenant uint64, id string) (string, error) {
	path, err := f.replaceFileStore.SaveFile(ctx, file, tenant, id)
	if err != nil {
		return "", err
	}
	rc, err := file.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err == nil {
		f.evidence.objects[path] = data
	}
	return path, err
}

func researchReplaceHarness(t *testing.T) (*replaceFileHarness, *researchReplaceRepo, *researchReplaceFiles) {
	t.Helper()
	h := newReplaceFileHarness(t)
	repo := &researchReplaceRepo{replaceFileRepo: h.repo, archive: &researchSourceRepoStub{versions: map[string]*types.ResearchSourceVersion{}}}
	files := &researchReplaceFiles{replaceFileStore: h.store, evidence: &researchSourceFiles{objects: map[string][]byte{h.original.FilePath: []byte(replaceFileOldContent)}}}
	h.svc.repo, h.svc.fileSvc = repo, files
	return h, repo, files
}

func TestResearchReplaceArchivesLegacyOriginalBeforeNativeDeletion(t *testing.T) {
	h, repo, files := researchReplaceHarness(t)
	got, err := h.replace(t, "new original", "a.md", nil)
	require.NoError(t, err)
	require.Equal(t, h.original.ID, got.ID)
	require.Len(t, repo.archive.versions, 1)
	for _, version := range repo.archive.versions {
		require.Equal(t, []byte(replaceFileOldContent), files.evidence.objects[version.OriginalPath])
		require.Equal(t, h.original.FileName, version.FileName)
		require.Equal(t, h.original.FileHash, version.FileHash)
		require.Empty(t, version.MarkdownPath, "legacy backup must not invent parsed text")
	}
	require.NotContains(t, files.evidence.objects, h.original.FilePath)
	require.Equal(t, []byte("new original"), files.evidence.objects[got.FilePath])
	require.Len(t, h.tasks.payloads, 1, "only the native replacement parse is enqueued")
}

func TestResearchReplaceArchiveFailureLeavesOriginalAndIndexUntouched(t *testing.T) {
	for _, stage := range []string{"copy", "database"} {
		t.Run(stage, func(t *testing.T) {
			h, repo, files := researchReplaceHarness(t)
			if stage == "copy" {
				files.copyErr = errors.New("storage unavailable")
			} else {
				repo.archive.fail = true
			}
			_, err := h.replace(t, "new original", "a.md", nil)
			require.Error(t, err)
			require.Equal(t, h.original.FilePath, repo.row.FilePath)
			require.Equal(t, h.original.ParseStatus, repo.row.ParseStatus)
			require.Zero(t, repo.columnsCalls)
			require.Zero(t, files.saved)
			require.Empty(t, h.tasks.payloads)
			require.Len(t, files.evidence.objects, 1, "failed archive copies are cleaned; the original remains")
			require.Equal(t, []byte(replaceFileOldContent), files.evidence.objects[h.original.FilePath])
		})
	}
}
