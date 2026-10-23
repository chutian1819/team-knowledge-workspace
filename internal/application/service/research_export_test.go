package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestResearchExportReadableProvenanceRetainsPortableFullRecord(t *testing.T) {
	sourceID := "11111111-2222-3333-4444-555555555555"
	captureID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	hash := strings.Repeat("abc123", 10)
	metadata, err := json.Marshal(map[string]any{"source_versions": map[string]any{
		sourceID: map[string]any{"capture_version_id": captureID, "content_hash": hash,
			"observed_updated_at": "2026-10-20T08:00:00Z"},
	}})
	require.NoError(t, err)
	page := &types.WikiPage{Content: "已审阅结论 [证据](knowledge://" + sourceID + "/chunks/chunk-1)", Version: 3, PageMetadata: metadata}
	sources := []*types.Knowledge{{ID: sourceID, Title: "原始研究资料", Source: "https://example.com/paper"}}
	for _, format := range []string{"docx", "pdf"} {
		export := researchExportMarkdown(page, sources, format)
		require.True(t, strings.HasPrefix(export, page.Content), "reviewed body and citations are unchanged")
		require.Contains(t, export, "原始研究资料")
		require.Contains(t, export, "证据版本："+captureID[:12])
		require.Contains(t, export, "内容指纹："+hash[:12])
		require.Contains(t, export, "2026-10-20 08:00:00 UTC")
		require.Contains(t, export, "研究稿版本：3")
		require.NotContains(t, export, "capture_version_id")
		require.NotContains(t, export, captureID)
		require.NotContains(t, export, "```json")
	}
	portable := researchExportMarkdown(page, sources, "md")
	require.Contains(t, portable, captureID)
	require.Contains(t, portable, hash)
	require.Contains(t, portable, sourceID)
	require.Contains(t, portable, "observed_updated_at")
}

func TestResearchSourceVersionAssetsUseCurrentMembershipAndNativeDownloadBoundary(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	require.NoError(t, s.db.AutoMigrate(&types.ResearchSourceVersion{}))
	item := &types.Knowledge{ID: "source", TenantID: 1, KnowledgeBaseID: "research-kb"}
	s.knowledge = researchKnowledgeAccessFixture{items: map[string]*types.Knowledge{"source": item}}
	version := types.ResearchSourceVersion{ID: "v1", TenantID: 1, KnowledgeID: item.ID, ContentHash: "sha256",
		OriginalPath: "stored-original", FileName: "../../原始资料.docx", MarkdownPath: "stored-md", SnapshotPath: "stored-snapshot"}
	require.NoError(t, s.db.Create(&version).Error)
	s.files = &researchFileFixture{data: map[string][]byte{
		"stored-original": []byte("immutable original"), "stored-md": []byte("archived Markdown"), "stored-snapshot": []byte("<html>archived</html>"),
	}}
	reader, name, err := s.SourceVersionAsset(ctx, "research-kb", item.ID, version.ID, "original")
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, "immutable original", string(body))
	require.Equal(t, "原始资料.docx", name)

	// An owner in another workspace can only read the shared source; that
	// does not elevate the share's Viewer grant into original download rights.
	shared := types.WithCaller(context.Background(), types.Caller{TenantID: 2, UserID: "colleague", Role: types.TenantRoleOwner})
	s.shares = researchShareAccessFixture{visible: map[string]map[uint64]bool{"research-kb": {2: true}}}
	for _, kind := range []string{"markdown", "snapshot"} {
		reader, _, err := s.SourceVersionAsset(shared, "research-kb", item.ID, version.ID, kind)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
	}
	_, _, err = s.SourceVersionAsset(shared, "research-kb", item.ID, version.ID, "original")
	require.ErrorIs(t, err, access.ErrForbidden)
	viewer := types.WithCaller(ctx, types.Caller{TenantID: 1, Role: types.TenantRoleViewer})
	_, _, err = s.SourceVersionAsset(viewer, "research-kb", item.ID, version.ID, "original")
	require.ErrorIs(t, err, access.ErrForbidden)
	_, _, err = s.SourceVersionAsset(ctx, "research-kb", "another-source", version.ID, "markdown")
	require.ErrorIs(t, err, access.ErrNotFound)
	item.KnowledgeBaseID = "moved-private"
	_, _, err = s.SourceVersionAsset(ctx, "research-kb", item.ID, version.ID, "markdown")
	require.ErrorIs(t, err, access.ErrNotFound)
}

func (c researchChunkFixture) ListAllChunksByKnowledgeID(context.Context, uint64, string) ([]*types.Chunk, error) {
	return c.chunks, nil
}

func TestResearchArchiveIncludesOriginalOnlyHistoricalVersion(t *testing.T) {
	ctx, s := researchDraftHarness(t)
	require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}, &types.ResearchSourceVersion{}))
	item := &types.Knowledge{ID: "source", TenantID: 1, KnowledgeBaseID: "research-kb", Title: "New content",
		FilePath: "current", FileName: "new.docx", CustomMetadata: types.JSON("{}")}
	require.NoError(t, s.db.Create(item).Error)
	version := &types.ResearchSourceVersion{ID: "old-version", TenantID: 1, KnowledgeID: item.ID, ContentHash: "original-sha256",
		OriginalPath: "old", FileName: "../old.docx", FileType: "docx", FileHash: "native-md5", CapturedAt: time.Now()}
	require.NoError(t, s.db.Create(version).Error)
	s.files = &researchFileFixture{data: map[string][]byte{"current": []byte("new original"), "old": []byte("old original")}}
	s.chunks = researchChunkFixture{chunks: []*types.Chunk{{ID: "chunk", ChunkType: types.ChunkTypeText, Content: "current text"}}}
	data, err := s.buildAssetArchive(ctx, &types.ResearchExport{KnowledgeBaseID: "research-kb", SourceIDs: types.StringArray{item.ID}})
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	file, err := archive.Open("versions/old-version/original/old.docx")
	require.NoError(t, err)
	body, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Equal(t, "old original", string(body))
	manifest, err := archive.Open("manifest.json")
	require.NoError(t, err)
	body, err = io.ReadAll(manifest)
	require.NoError(t, err)
	require.NoError(t, manifest.Close())
	require.Contains(t, string(body), `"original": "versions/old-version/original/old.docx"`)
	require.Contains(t, string(body), `"file_hash": "native-md5"`)
	require.Contains(t, string(body), `"content_hash": "original-sha256"`)
}
