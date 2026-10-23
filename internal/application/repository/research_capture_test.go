package repository

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestResearchAudienceLockPropagatesCallbackAndRejectsMissingScope(t *testing.T) {
	r := captureRepoTest(t)
	calls := 0
	failed := errors.New("native share failed")
	callback := func() error { calls++; return failed }
	require.Error(t, r.WithAudienceLock(context.Background(), "", callback))
	require.Zero(t, calls)
	require.ErrorIs(t, r.WithAudienceLock(context.Background(), "organization", callback), failed)
	require.Equal(t, 1, calls)
}

func TestResearchCaptureEmptyAudiencePersistsAsJSONArray(t *testing.T) {
	r := captureRepoTest(t)
	job := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: 1, UserID: "owner", KnowledgeBaseID: "kb", Kind: "bookmarks", IdempotencyKey: "empty-audience", RequestHash: "hash", Status: "queued"}
	saved, err := r.Create(context.Background(), job)
	require.NoError(t, err)
	require.NotNil(t, saved.SelectedTenantIDs)
	require.NotNil(t, saved.Items)
	var row struct {
		Audience string
		Items    string
	}
	require.NoError(t, r.DB.Raw("SELECT selected_tenant_ids AS audience, items FROM research_capture_jobs WHERE id = ?", job.ID).Scan(&row).Error)
	require.Equal(t, "[]", row.Audience)
	require.Equal(t, "[]", row.Items)
}

func captureRepoTest(t *testing.T) *ResearchCaptureRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.ResearchCaptureJob{}, &types.ResearchInbox{}, &types.TenantMember{}))
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	return NewResearchCaptureRepository(db)
}
func TestResearchCaptureRequestIdempotencyAndTenantIsolation(t *testing.T) {
	r := captureRepoTest(t)
	ctx := context.Background()
	job := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: 1, UserID: "owner", KnowledgeBaseID: "kb", Kind: "bookmarks", IdempotencyKey: "once", RequestHash: "hash", Status: "queued", Items: []types.ResearchCaptureItem{{URL: "https://example.com", Status: "queued"}}}
	first, err := r.Create(ctx, job)
	require.NoError(t, err)
	retry := *job
	retry.ID = uuid.NewString()
	second, err := r.Create(ctx, &retry)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	retry.RequestHash = "different"
	_, err = r.Create(ctx, &retry)
	require.Error(t, err)
	_, err = r.Get(ctx, 2, first.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	second.Items[0].Status = "saved"
	second.Completed = 1
	require.NoError(t, r.Save(ctx, second))
	roundtrip, err := r.Get(ctx, 1, first.ID)
	require.NoError(t, err)
	require.Equal(t, "saved", roundtrip.Items[0].Status)
}
func TestResearchCaptureLeaseAndResume(t *testing.T) {
	r := captureRepoTest(t)
	ctx := context.Background()
	job := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: 1, UserID: "u", IdempotencyKey: "a", Status: "queued"}
	_, err := r.Create(ctx, job)
	require.NoError(t, err)
	ok, err := r.Claim(ctx, job)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.Claim(ctx, job)
	require.NoError(t, err)
	require.False(t, ok)
	stale := time.Now().Add(-time.Minute)
	job.LeaseUntil = &stale
	require.NoError(t, r.Save(ctx, job))
	ok, err = r.Claim(ctx, job)
	require.NoError(t, err)
	require.True(t, ok)
	job.Status = "completed"
	job.LeaseUntil = nil
	require.NoError(t, r.Save(ctx, job))
	ok, err = r.Claim(ctx, job)
	require.NoError(t, err)
	require.False(t, ok)
}
func TestResearchPrivateInboxRejectsSharedWorkspace(t *testing.T) {
	r := captureRepoTest(t)
	ctx := context.Background()
	require.NoError(t, r.DB.Create(&types.TenantMember{TenantID: 1, UserID: "alice", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}).Error)
	require.NoError(t, r.PersonalWorkspace(ctx, 1, "alice"))
	require.Error(t, r.PersonalWorkspace(ctx, 1, "bob"))
	require.NoError(t, r.DB.Create(&types.TenantMember{TenantID: 1, UserID: "bob", Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}).Error)
	require.Error(t, r.PersonalWorkspace(ctx, 1, "alice"))
}

func TestResearchPrivateInboxPreventsNativeMemberCreation(t *testing.T) {
	r := captureRepoTest(t)
	ctx := context.Background()
	require.NoError(t, r.DB.Create(&types.ResearchInbox{TenantID: 1, UserID: "alice", KnowledgeBaseID: "private"}).Error)
	memberRepo := NewTenantMemberRepository(r.DB)
	require.NoError(t, memberRepo.Create(ctx, &types.TenantMember{TenantID: 1, UserID: "alice", Role: types.TenantRoleOwner}))
	require.ErrorIs(t, memberRepo.Create(ctx, &types.TenantMember{TenantID: 1, UserID: "bob", Role: types.TenantRoleContributor}), ErrResearchPrivateWorkspace)
	// Team workspaces remain available independently.
	require.NoError(t, memberRepo.Create(ctx, &types.TenantMember{TenantID: 2, UserID: "alice", Role: types.TenantRoleOwner}))
	require.NoError(t, memberRepo.Create(ctx, &types.TenantMember{TenantID: 2, UserID: "bob", Role: types.TenantRoleContributor}))
}

func TestResearchSourceVersionAtomicCreationAndDeletion(t *testing.T) {
	r := captureRepoTest(t)
	ctx := context.Background()
	require.NoError(t, r.DB.AutoMigrate(&types.ResearchSourceVersion{}))
	require.NoError(t, r.DB.Exec("CREATE TABLE knowledges (id TEXT PRIMARY KEY,tenant_id BIGINT,deleted_at DATETIME)").Error)
	require.NoError(t, r.DB.Exec("INSERT INTO knowledges(id,tenant_id) VALUES ('live',1),('deleted',1)").Error)
	kr := NewKnowledgeRepository(r.DB).(*knowledgeRepository)
	first := &types.ResearchSourceVersion{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "live", ContentHash: "content", MarkdownPath: "first.md"}
	require.NoError(t, kr.CreateResearchSourceVersion(ctx, first))
	duplicate := *first
	duplicate.ID = uuid.NewString()
	duplicate.MarkdownPath = "loser.md"
	require.ErrorIs(t, kr.CreateResearchSourceVersion(ctx, &duplicate), ErrResearchSourceVersionExists)
	found, err := kr.FindResearchSourceVersion(ctx, 1, "live", "content")
	require.NoError(t, err)
	require.Equal(t, "first.md", found.MarkdownPath)
	require.NoError(t, r.DB.Exec("UPDATE knowledges SET deleted_at=CURRENT_TIMESTAMP WHERE id='deleted'").Error)
	rejected := *first
	rejected.ID = uuid.NewString()
	rejected.KnowledgeID = "deleted"
	require.ErrorIs(t, kr.CreateResearchSourceVersion(ctx, &rejected), gorm.ErrRecordNotFound)
}

func TestResearchCaptureLeaseRejectsStaleWorkerAndRetry(t *testing.T) {
	r := captureRepoTest(t)
	ctx := context.Background()
	j := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: 1, UserID: "u", IdempotencyKey: "lease", Status: "queued"}
	_, err := r.Create(ctx, j)
	require.NoError(t, err)
	claimed, err := r.Claim(ctx, j)
	require.NoError(t, err)
	require.True(t, claimed)
	stale := *j
	require.Error(t, r.PrepareRetry(ctx, &stale))
	expired := time.Now().Add(-time.Minute)
	require.NoError(t, r.DB.Model(&types.ResearchCaptureJob{}).Where("id = ?", j.ID).Update("lease_until", expired).Error)
	current, err := r.Get(ctx, 1, j.ID)
	require.NoError(t, err)
	claimed, err = r.Claim(ctx, current)
	require.NoError(t, err)
	require.True(t, claimed)
	stale.Status = "failed"
	stale.LeaseUntil = nil
	require.Error(t, r.Save(ctx, &stale))
	latest, err := r.Get(ctx, 1, j.ID)
	require.NoError(t, err)
	require.Equal(t, "processing", latest.Status)
	require.NotNil(t, latest.LeaseUntil)
}
func TestResearchCaptureClaimLoadsLatestPersistedProgress(t *testing.T) {
	r := captureRepoTest(t)
	ctx := context.Background()
	j := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: 1, UserID: "u", IdempotencyKey: "fresh", Status: "queued"}
	_, err := r.Create(ctx, j)
	require.NoError(t, err)
	stale := *j
	j.Completed = 3
	j.Status = "partial"
	require.NoError(t, r.Save(ctx, j))
	claimed, err := r.Claim(ctx, &stale)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, 3, stale.Completed)
}
