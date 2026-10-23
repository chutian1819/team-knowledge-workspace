package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestResearchInboxDedupFindsMovedEvidenceButPreservesNativeAndOwnerScopes(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.ResearchInbox{}))
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, creator_id TEXT, deleted_at DATETIME)").Error)
	for _, row := range [][]any{{"inbox", 1, "owner"}, {"project", 1, "owner"}, {"other-owner", 1, "different"}, {"foreign", 2, "owner"}} {
		require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id, tenant_id, creator_id) VALUES (?, ?, ?)", row...).Error)
	}
	require.NoError(t, db.Create(&types.ResearchInbox{TenantID: 1, UserID: "owner", KnowledgeBaseID: "inbox"}).Error)
	for _, row := range [][]any{{"moved", 1, "project", "hash"}, {"private", 1, "other-owner", "private-hash"}, {"foreign", 2, "foreign", "foreign-hash"}} {
		require.NoError(t, db.Exec("INSERT INTO knowledges (id, tenant_id, knowledge_base_id, file_hash, type, file_type, parse_status) VALUES (?, ?, ?, ?, 'file', 'txt', 'completed')", row...).Error)
	}
	repo := &knowledgeRepository{db: db}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleOwner})
	params := &types.KnowledgeCheckParams{Type: "file", FileType: "txt", FileHash: "hash"}
	exists, _, err := repo.CheckKnowledgeExists(ctx, 1, "inbox", params)
	require.NoError(t, err)
	require.False(t, exists, "native callers retain per-KB identity")
	handled, existing, err := repo.WithResearchInboxDedup(ctx, "inbox", "file:txt:hash", func(locked context.Context) (*types.Knowledge, error) {
		// Re-entry must not attempt another transaction/lock.
		nested, _, e := repo.WithResearchInboxDedup(locked, "inbox", "same", nil)
		require.NoError(t, e)
		require.False(t, nested)
		found, item, e := repo.CheckKnowledgeExists(locked, 1, "inbox", params)
		require.True(t, found)
		return item, e
	})
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, "moved", existing.ID)
	for _, hash := range []string{"private-hash", "foreign-hash"} {
		_, _, err := repo.WithResearchInboxDedup(ctx, "inbox", hash, func(locked context.Context) (*types.Knowledge, error) {
			found, _, e := repo.CheckKnowledgeExists(locked, 1, "inbox", &types.KnowledgeCheckParams{Type: "file", FileType: "txt", FileHash: hash})
			require.False(t, found)
			return nil, e
		})
		require.NoError(t, err)
	}
	wrong := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "different", Role: types.TenantRoleOwner})
	handled, _, err = repo.WithResearchInboxDedup(wrong, "inbox", "hash", nil)
	require.NoError(t, err)
	require.False(t, handled)
}

func TestResearchPaperURLDedupSurvivesDownloadedContentHash(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	repo := &knowledgeRepository{db: db}
	const paperURL = "https://example.com/paper.pdf"
	require.NoError(t, db.Exec("INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, source, file_hash, parse_status) VALUES ('paper', 1, 'kb', 'file_url', ?, 'downloaded-file-hash', 'completed')", paperURL).Error)
	exists, paper, err := repo.CheckKnowledgeExists(context.Background(), 1, "kb", &types.KnowledgeCheckParams{Type: "file_url", URL: paperURL, FileHash: "url-hash"})
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "paper", paper.ID)
	exists, _, err = repo.CheckKnowledgeExists(context.Background(), 2, "kb", &types.KnowledgeCheckParams{Type: "file_url", URL: paperURL})
	require.NoError(t, err)
	require.False(t, exists)
}
