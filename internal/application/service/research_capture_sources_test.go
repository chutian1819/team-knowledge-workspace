package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type researchSourceRepoStub struct {
	interfaces.KnowledgeRepository
	versions map[string]*types.ResearchSourceVersion
	fail     bool
	conflict bool
}

func (r *researchSourceRepoStub) FindResearchSourceVersion(_ context.Context, _ uint64, _ string, hash string) (*types.ResearchSourceVersion, error) {
	return r.versions[hash], nil
}
func (r *researchSourceRepoStub) CreateResearchSourceVersion(_ context.Context, v *types.ResearchSourceVersion) error {
	if r.fail {
		return errors.New("database down")
	}
	if r.conflict {
		return repository.ErrResearchSourceVersionExists
	}
	r.versions[v.ContentHash] = v
	return nil
}

type researchSourceFiles struct {
	interfaces.FileService
	objects map[string][]byte
}

func (f *researchSourceFiles) SaveBytes(_ context.Context, b []byte, _ uint64, name string, _ bool) (string, error) {
	path := "local://1/exports/" + name
	f.objects[path] = append([]byte(nil), b...)
	return path, nil
}
func (f *researchSourceFiles) GetFile(_ context.Context, path string) (io.ReadCloser, error) {
	b, ok := f.objects[path]
	if !ok {
		return nil, errors.New("file missing")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (f *researchSourceFiles) DeleteFile(_ context.Context, path string) error {
	delete(f.objects, path)
	return nil
}
func (f *researchSourceFiles) CopyFile(_ context.Context, path string, _ uint64, _ string) (string, error) {
	data, ok := f.objects[path]
	if !ok {
		return "", errors.New("original missing")
	}
	copyPath := "local://1/originals/" + uuid.NewString() + ".txt"
	f.objects[copyPath] = append([]byte(nil), data...)
	return copyPath, nil
}
func TestResearchSourceArchiveImmutableAndContentDeduplicated(t *testing.T) {
	repo := &researchSourceRepoStub{versions: map[string]*types.ResearchSourceVersion{}}
	files := &researchSourceFiles{objects: map[string][]byte{"local://image.png": []byte("image bytes")}}
	s := &knowledgeService{repo: repo, fileSvc: files}
	ctx := context.Background()
	kb := &types.KnowledgeBase{ID: "kb"}
	k := &types.Knowledge{ID: "doc", TenantID: 1, Type: "url", Source: "https://example.com"}
	result := &types.ReadResult{MarkdownContent: "# First\n![image](local://image.png)"}
	images := []docparser.StoredImage{{ServingURL: "local://image.png"}}
	require.NoError(t, s.archiveResearchSource(ctx, kb, k, result, "first content", "<html>first</html>", images))
	v := repo.versions[sourceContentHash("first content")]
	require.NotNil(t, v)
	require.Len(t, v.ImagePaths, 1)
	require.Contains(t, string(files.objects[v.MarkdownPath]), v.ImagePaths[0])
	count := len(files.objects)
	require.NoError(t, s.archiveResearchSource(ctx, kb, k, result, "first content", "<html>noise</html>", images))
	require.Len(t, files.objects, count)
	delete(files.objects, "local://image.png") // Simulate native reparse cleanup.
	require.Equal(t, []byte("image bytes"), files.objects[v.ImagePaths[0]])
	require.NoError(t, s.archiveResearchSource(ctx, kb, k, &types.ReadResult{MarkdownContent: "second"}, "second content", "<html>second</html>", nil))
	require.Len(t, repo.versions, 2)
	require.Equal(t, "<html>first</html>", string(files.objects[v.SnapshotPath]))
}
func TestResearchSourceArchiveFailureRollsBackNewObjects(t *testing.T) {
	repo := &researchSourceRepoStub{versions: map[string]*types.ResearchSourceVersion{}, fail: true}
	files := &researchSourceFiles{objects: map[string][]byte{}}
	s := &knowledgeService{repo: repo, fileSvc: files}
	err := s.archiveResearchSource(context.Background(), &types.KnowledgeBase{ID: "kb"}, &types.Knowledge{ID: "doc", TenantID: 1, Type: "url"}, &types.ReadResult{MarkdownContent: "body"}, "body", "html", nil)
	require.Error(t, err)
	require.Empty(t, files.objects)
	require.Empty(t, repo.versions)
}
func TestResearchSourceHashIgnoresCRLFButTracksBodyChanges(t *testing.T) {
	require.Equal(t, sourceContentHash("a\r\nb"), sourceContentHash("a\nb"))
	require.NotEqual(t, sourceContentHash("a"), sourceContentHash("b"))
}

func TestResearchSourceArchiveConcurrentLoserCleansItsFiles(t *testing.T) {
	repo := &researchSourceRepoStub{versions: map[string]*types.ResearchSourceVersion{}, conflict: true}
	files := &researchSourceFiles{objects: map[string][]byte{}}
	s := &knowledgeService{repo: repo, fileSvc: files}
	require.NoError(t, s.archiveResearchSource(context.Background(), &types.KnowledgeBase{ID: "kb"}, &types.Knowledge{ID: "doc", TenantID: 1, Type: "url"}, &types.ReadResult{MarkdownContent: "body"}, "body", "html", nil))
	require.Empty(t, files.objects)
}

func TestResearchFileVersionsPreserveOriginalBytesEvenWhenParsedBodyIsUnchanged(t *testing.T) {
	repo := &researchSourceRepoStub{versions: map[string]*types.ResearchSourceVersion{}}
	files := &researchSourceFiles{objects: map[string][]byte{
		"local://upload": []byte("first\r\nbytes"), "local://image.png": []byte("image"),
	}}
	svc := &knowledgeService{repo: repo, fileSvc: files}
	kb := &types.KnowledgeBase{ID: "kb"}
	k := &types.Knowledge{ID: "doc", TenantID: 1, Type: "file", FilePath: "local://upload", FileName: "report.docx", FileType: "docx", FileHash: "native-md5-first"}
	parsed := &types.ReadResult{MarkdownContent: "unchanged body\n![image](local://image.png)"}
	images := []docparser.StoredImage{{ServingURL: "local://image.png"}}
	require.NoError(t, svc.archiveResearchSource(context.Background(), kb, k, parsed, "unchanged body", "", images))
	require.Len(t, repo.versions, 1)
	var first *types.ResearchSourceVersion
	for _, version := range repo.versions {
		first = version
	}
	require.NotEmpty(t, first.OriginalPath)
	require.Equal(t, "report.docx", first.FileName)
	require.Equal(t, "native-md5-first", first.FileHash)
	require.Equal(t, []byte("first\r\nbytes"), files.objects[first.OriginalPath])
	count := len(files.objects)
	require.NoError(t, svc.archiveResearchSource(context.Background(), kb, k, parsed, "unchanged body", "", images))
	require.Len(t, files.objects, count, "identical original bytes must not make another evidence copy")
	files.objects[k.FilePath] = []byte("first\nbytes")
	k.FileHash = "native-md5-second"
	require.NoError(t, svc.archiveResearchSource(context.Background(), kb, k, parsed, "unchanged body", "", images))
	require.Len(t, repo.versions, 2, "original byte changes must survive even when extracted body is identical")
	delete(files.objects, k.FilePath)
	delete(files.objects, "local://image.png")
	require.Equal(t, []byte("first\r\nbytes"), files.objects[first.OriginalPath])
	require.Equal(t, []byte("image"), files.objects[first.ImagePaths[0]])
	require.Contains(t, string(files.objects[first.MarkdownPath]), first.ImagePaths[0])
}

func TestResearchFileURLArchivesAlreadyDownloadedOriginalAndURLProvenance(t *testing.T) {
	repo := &researchSourceRepoStub{versions: map[string]*types.ResearchSourceVersion{}}
	files := &researchSourceFiles{objects: map[string][]byte{
		"local://downloaded-paper.pdf": []byte("%PDF-existing-downloaded-bytes"),
	}}
	svc := &knowledgeService{repo: repo, fileSvc: files}
	kb := &types.KnowledgeBase{ID: "kb"}
	k := &types.Knowledge{ID: "paper", TenantID: 1, Type: "file_url",
		Source: "https://papers.example/research.pdf", FilePath: "local://downloaded-paper.pdf",
		FileName: "research.pdf", FileType: "pdf", FileHash: "native-url-hash"}
	parsed := &types.ReadResult{MarkdownContent: "# Paper\n\nParsed evidence."}
	require.NoError(t, svc.archiveResearchSource(context.Background(), kb, k, parsed, parsed.MarkdownContent, "", nil))
	require.Len(t, repo.versions, 1)
	var version *types.ResearchSourceVersion
	for _, item := range repo.versions {
		version = item
	}
	require.Equal(t, k.Source, version.SourceURL)
	require.Equal(t, k.FileHash, version.FileHash, "native file URL hash remains source identity")
	require.NotEqual(t, k.FilePath, version.OriginalPath)
	require.Equal(t, []byte("%PDF-existing-downloaded-bytes"), files.objects[version.OriginalPath])
	require.Equal(t, parsed.MarkdownContent, string(files.objects[version.MarkdownPath]))
	count := len(files.objects)
	require.NoError(t, svc.archiveResearchSource(context.Background(), kb, k, parsed, parsed.MarkdownContent, "", nil))
	require.Len(t, files.objects, count)
	delete(files.objects, k.FilePath)
	require.Equal(t, []byte("%PDF-existing-downloaded-bytes"), files.objects[version.OriginalPath])
}

func TestResearchFileURLMissingDownloadedOriginalFailsClosed(t *testing.T) {
	repo := &researchSourceRepoStub{versions: map[string]*types.ResearchSourceVersion{}}
	svc := &knowledgeService{repo: repo, fileSvc: &researchSourceFiles{objects: map[string][]byte{}}}
	err := svc.archiveResearchSource(context.Background(), &types.KnowledgeBase{ID: "kb"},
		&types.Knowledge{ID: "paper", Type: "file_url", Source: "https://papers.example/research.pdf"},
		&types.ReadResult{MarkdownContent: "Paper"}, "Paper", "", nil)
	require.ErrorContains(t, err, "original path")
	require.Empty(t, repo.versions)
}
