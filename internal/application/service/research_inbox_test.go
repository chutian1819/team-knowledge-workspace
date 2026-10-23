package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type inboxShares struct {
	interfaces.KBShareService
	shared bool
}

func (s inboxShares) ListSharesByKnowledgeBase(context.Context, string, uint64) ([]*types.KnowledgeBaseShare, error) {
	if s.shared {
		return []*types.KnowledgeBaseShare{{OrganizationID: "team"}}, nil
	}
	return nil, nil
}

func TestResearchInboxReadUsesBindingAndPreservesPrivacy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.ResearchInbox{}, &types.TenantMember{}))
	require.NoError(t, db.Exec("CREATE TABLE custom_agents(tenant_id BIGINT, deleted_at DATETIME)").Error)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "alice", Role: types.TenantRoleOwner})
	bases := map[string]*types.KnowledgeBase{
		"real":     {ID: "real", TenantID: 1, CreatorID: "alice", Name: "已改名"},
		"namesake": {ID: "namesake", TenantID: 1, CreatorID: "alice", Name: "我的收件箱"},
	}
	s := &ResearchCaptureService{Repo: repository.NewResearchCaptureRepository(db), kb: captureShareKB{bases: bases}, share: inboxShares{}}
	got, err := s.CurrentInbox(ctx)
	require.NoError(t, err)
	require.Nil(t, got) // Reading must not create or adopt a namesake.
	require.NoError(t, db.Create(&types.TenantMember{TenantID: 1, UserID: "alice", Status: types.TenantMemberStatusActive}).Error)
	require.NoError(t, db.Create(&types.ResearchInbox{TenantID: 1, UserID: "alice", KnowledgeBaseID: "real"}).Error)
	got, err = s.CurrentInbox(ctx)
	require.NoError(t, err)
	require.Equal(t, "real", got.ID)
	other := types.WithCaller(ctx, types.Caller{TenantID: 2, UserID: "bob"})
	got, err = s.CurrentInbox(other)
	require.NoError(t, err)
	require.Nil(t, got)
	s.share = inboxShares{shared: true}
	_, err = s.CurrentInbox(ctx)
	require.Error(t, err)
	s.share = inboxShares{}
	bases["real"].CreatorID = "bob"
	_, err = s.CurrentInbox(ctx)
	require.ErrorIs(t, err, access.ErrForbidden)
	bases["real"].CreatorID = "alice"
	require.NoError(t, db.Create(&types.TenantMember{TenantID: 1, UserID: "bob", Status: types.TenantMemberStatusActive}).Error)
	_, err = s.CurrentInbox(ctx)
	require.Error(t, err)
}

func TestResearchInboxChoosesAccessibleActiveDefaultModels(t *testing.T) {
	kb := &types.KnowledgeBase{}
	applyResearchInboxModels(kb, []*types.Model{
		{ID: "private-chat", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
		{ID: "team-chat", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive, IsBuiltin: true, IsDefault: true},
		{ID: "broken", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusDownloadFailed, IsDefault: true},
		{ID: "embedding", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, IsBuiltin: true},
		{ID: "vision", Type: types.ModelTypeVLLM, Status: types.ModelStatusActive, IsBuiltin: true},
		nil,
	})
	require.Equal(t, "team-chat", kb.SummaryModelID)
	require.Equal(t, "embedding", kb.EmbeddingModelID)
	require.Equal(t, "vision", kb.ImageProcessingConfig.ModelID)
	require.Equal(t, "vision", kb.VLMConfig.ModelID)
	require.True(t, kb.VLMConfig.Enabled)
	empty := &types.KnowledgeBase{}
	applyResearchInboxModels(empty, nil)
	require.Empty(t, empty.EmbeddingModelID)
	require.Empty(t, empty.SummaryModelID)
}
