package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type researchKBAccessFixture struct {
	interfaces.KnowledgeBaseService
	kbs map[string]*types.KnowledgeBase
}

func (f researchKBAccessFixture) GetKnowledgeBaseByIDOnly(_ context.Context, id string) (*types.KnowledgeBase, error) {
	kb := f.kbs[id]
	if kb == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return kb, nil
}

type researchKnowledgeAccessFixture struct {
	interfaces.KnowledgeService
	items map[string]*types.Knowledge
}

func (f researchKnowledgeAccessFixture) GetKnowledgeByIDOnly(_ context.Context, id string) (*types.Knowledge, error) {
	if k := f.items[id]; k != nil {
		return k, nil
	}
	return nil, gorm.ErrRecordNotFound
}

type researchShareAccessFixture struct {
	interfaces.KBShareService
	visible map[string]map[uint64]bool
	err     error
}

func (f researchShareAccessFixture) CheckTenantKBPermission(_ context.Context, id string, tenant uint64, _ types.TenantRole) (types.OrgMemberRole, bool, error) {
	return types.OrgRoleViewer, f.visible[id][tenant], f.err
}

type researchAgentAccessFixture struct {
	interfaces.AgentShareService
	visible map[string]map[uint64]bool
}

func (f researchAgentAccessFixture) TenantCanAccessKBViaSomeSharedAgent(_ context.Context, tenant uint64, _ types.TenantRole, kb *types.KnowledgeBase) (bool, error) {
	return f.visible[kb.ID][tenant], nil
}

func TestResearchSourcesEnforceDestinationAudience(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:research-source-access?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("CREATE TABLE IF NOT EXISTS tenants (id INTEGER PRIMARY KEY, status TEXT, deleted_at DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2, 3} {
		if err = db.Exec("INSERT OR REPLACE INTO tenants (id,status) VALUES (?, 'active')", id).Error; err != nil {
			t.Fatal(err)
		}
	}
	kbs := researchKBAccessFixture{kbs: map[string]*types.KnowledgeBase{
		"drafts":  {ID: "drafts", TenantID: 1},
		"sources": {ID: "sources", TenantID: 2},
	}}
	knowledge := researchKnowledgeAccessFixture{items: map[string]*types.Knowledge{
		"evidence": {ID: "evidence", TenantID: 2, KnowledgeBaseID: "sources"},
	}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "author", Role: types.TenantRoleOwner})
	cases := []struct {
		name    string
		shares  map[string]map[uint64]bool
		agents  map[string]map[uint64]bool
		allowed bool
	}{
		{"cross-workspace shared evidence", map[string]map[uint64]bool{"sources": {1: true}, "drafts": {2: true}}, nil, true},
		{"private evidence is not accessible to author", nil, nil, false},
		{"another destination organization widens audience", map[string]map[uint64]bool{"sources": {1: true}, "drafts": {2: true, 3: true}}, nil, false},
		{"agent grant also counts as destination audience", map[string]map[uint64]bool{"sources": {1: true}, "drafts": {2: true}}, map[string]map[uint64]bool{"drafts": {3: true}}, false},
		{"agent-granted evidence covers the same audience", map[string]map[uint64]bool{"sources": {1: true}, "drafts": {2: true, 3: true}}, map[string]map[uint64]bool{"sources": {3: true}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var agents interfaces.AgentShareService
			if tc.agents != nil {
				agents = researchAgentAccessFixture{visible: tc.agents}
			}
			items, err := ValidateResearchSources(ctx, db, "drafts", []string{"evidence", "evidence"}, kbs,
				researchShareAccessFixture{visible: tc.shares}, agents, knowledge)
			if tc.allowed {
				if err != nil || len(items) != 1 {
					t.Fatalf("want one deduplicated authorized source, got %d: %v", len(items), err)
				}
			} else {
				var appErr *apperrors.AppError
				if !errors.As(err, &appErr) || appErr.HTTPCode != 403 {
					t.Fatalf("want forbidden, got %v", err)
				}
			}
		})
	}
	t.Run("authorization dependency failure fails closed", func(t *testing.T) {
		_, err := ValidateResearchSources(ctx, db, "drafts", []string{"evidence"}, kbs,
			researchShareAccessFixture{err: fmt.Errorf("database unavailable")}, nil, knowledge)
		if err == nil {
			t.Fatal("a failed authorization lookup must never publish evidence")
		}
	})
}
