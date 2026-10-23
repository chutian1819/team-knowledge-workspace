package service

import (
	"context"
	"encoding/json"
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

type captureWorkerTenant struct{ interfaces.TenantService }

func (captureWorkerTenant) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: id}, nil
}

type captureWorkerKB struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s captureWorkerKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}
func TestResearchCaptureWorkerRechecksMembershipAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status types.TenantMemberStatus
		owner  string
	}{
		{"suspended", types.TenantMemberStatusSuspended, "alice"},
		{"ownership changed", types.TenantMemberStatusActive, "bob"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&types.ResearchCaptureJob{}, &types.TenantMember{}))
			sql, _ := db.DB()
			defer sql.Close()
			repo := repository.NewResearchCaptureRepository(db)
			require.NoError(t, db.Create(&types.TenantMember{TenantID: 1, UserID: "alice", Role: types.TenantRoleOwner, Status: tc.status}).Error)
			j := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: 1, UserID: "alice", KnowledgeBaseID: "source", Kind: "bookmarks", Status: "queued", IdempotencyKey: uuid.NewString(), Items: []types.ResearchCaptureItem{{URL: "https://example.com", Status: "queued"}}}
			_, err = repo.Create(context.Background(), j)
			require.NoError(t, err)
			svc := &ResearchCaptureService{Repo: repo, kb: captureWorkerKB{kb: &types.KnowledgeBase{ID: "source", TenantID: 1, CreatorID: tc.owner}}, tenant: captureWorkerTenant{}}
			payload, _ := json.Marshal(map[string]any{"id": j.ID, "tenant_id": 1})
			require.ErrorIs(t, svc.ProcessTask(context.Background(), asynq.NewTask(TypeResearchCapture, payload)), access.ErrForbidden)
			saved, err := repo.Get(context.Background(), 1, j.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", saved.Status)
			require.Equal(t, "queued", saved.Items[0].Status)
		})
	}
}
