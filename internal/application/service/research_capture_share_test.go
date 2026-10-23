package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type captureShareKB struct {
	interfaces.KnowledgeBaseService
	bases map[string]*types.KnowledgeBase
}

func (s captureShareKB) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return s.bases[id], nil
}

type captureShareOrg struct{ interfaces.OrganizationService }

func (captureShareOrg) ListTenantMembers(context.Context, string) ([]*types.OrganizationTenantMember, error) {
	return []*types.OrganizationTenantMember{{TenantID: 1}, {TenantID: 2}}, nil
}
func (captureShareOrg) GetTenantRoleInOrg(context.Context, string, uint64) (types.OrgMemberRole, error) {
	return types.OrgRoleEditor, nil
}

type captureShareGrant struct{ interfaces.KBShareService }

func (captureShareGrant) ListSharesByKnowledgeBase(context.Context, string, uint64) ([]*types.KnowledgeBaseShare, error) {
	return []*types.KnowledgeBaseShare{{OrganizationID: "org", Permission: types.OrgRoleViewer}}, nil
}

type captureShareTags struct{ interfaces.KnowledgeTagService }

func (captureShareTags) FindOrCreateTagByName(_ context.Context, _ string, name string) (*types.KnowledgeTag, error) {
	return &types.KnowledgeTag{ID: "target-" + name, Name: name}, nil
}

type captureShareKnowledge struct {
	interfaces.KnowledgeService
	document   *types.Knowledge
	actualTags []*types.KnowledgeTag
	calls      int
	restored   []string
	taskIDs    []string
}

func (s *captureShareKnowledge) GetKnowledgeByID(context.Context, string) (*types.Knowledge, error) {
	return s.document, nil
}
func (s *captureShareKnowledge) GetKnowledgeTags(context.Context, []string) (map[string][]*types.KnowledgeTag, error) {
	return map[string][]*types.KnowledgeTag{"doc": s.actualTags}, nil
}
func (s *captureShareKnowledge) ProcessKnowledgeMove(_ context.Context, task *asynq.Task) error {
	var payload types.KnowledgeMovePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	s.taskIDs = append(s.taskIDs, payload.TaskID)
	s.calls++
	s.actualTags = nil // Native move clears KB-scoped tag relations before its final record update.
	if s.calls == 1 {
		return errors.New("interrupted after tag cleanup")
	}
	s.document.KnowledgeBaseID = "target"
	return nil
}
func (s *captureShareKnowledge) SetKnowledgeTags(ctx context.Context, _ string, ids []string) error {
	if !access.HasKBGrant(ctx, "target", 1, types.OrgRoleEditor) {
		return access.ErrForbidden
	}
	s.restored = ids
	return nil
}
func TestResearchShareRetryPreservesTagCheckpointAndRechecksOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.ResearchCaptureJob{}, &types.TenantMember{}))
	require.NoError(t, db.Exec("CREATE TABLE custom_agents(tenant_id BIGINT,deleted_at DATETIME)").Error)
	sql, _ := db.DB()
	defer sql.Close()
	require.NoError(t, db.Create(&types.TenantMember{TenantID: 1, UserID: "alice", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}).Error)
	repo := repository.NewResearchCaptureRepository(db)
	job := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: 1, UserID: "alice", KnowledgeBaseID: "source", TargetKBID: "target", OrganizationID: "org", SelectedTenantIDs: []uint64{1, 2}, Kind: "share", Status: "processing", IdempotencyKey: uuid.NewString(), Items: []types.ResearchCaptureItem{{KnowledgeID: "doc", Status: "queued", Tags: []types.ResearchCaptureTag{{Name: "stale"}}}}}
	_, err = repo.Create(context.Background(), job)
	require.NoError(t, err)
	bases := map[string]*types.KnowledgeBase{"source": {ID: "source", TenantID: 1, CreatorID: "alice"}, "target": {ID: "target", TenantID: 1, CreatorID: "alice"}}
	kg := &captureShareKnowledge{document: &types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "source"}, actualTags: []*types.KnowledgeTag{{ID: "old-id", Name: "latest"}}}
	svc := &ResearchCaptureService{Repo: repo, kb: captureShareKB{bases: bases}, kg: kg, org: captureShareOrg{}, share: captureShareGrant{}, tags: captureShareTags{}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "alice", Role: types.TenantRoleOwner})
	ctx = types.WithExecutionTenant(ctx, 1)
	require.Error(t, svc.shareItem(ctx, job, &job.Items[0]))
	require.True(t, job.Items[0].TagsCaptured)
	job.Items[0].Status = "failed"
	require.NoError(t, repo.Save(ctx, job))
	bases["target"].CreatorID = "bob"
	require.ErrorIs(t, svc.shareItem(ctx, job, &job.Items[0]), access.ErrForbidden)
	require.Equal(t, 1, kg.calls)
	bases["target"].CreatorID = "alice"
	persisted, err := repo.Get(ctx, 1, job.ID)
	require.NoError(t, err)
	require.NoError(t, svc.shareItem(ctx, persisted, &persisted.Items[0]))
	require.Equal(t, []string{"target-latest"}, kg.restored)
	require.Equal(t, "moved", persisted.Items[0].Status)
	require.Len(t, kg.taskIDs, 2)
	require.Equal(t, kg.taskIDs[0], kg.taskIDs[1])
	require.Len(t, kg.taskIDs[0], 36)
}
