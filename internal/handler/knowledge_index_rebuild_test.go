package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type indexRebuildHandlerService struct {
	interfaces.KnowledgeService
	t      *testing.T
	tenant uint64
	called bool
}

func (s *indexRebuildHandlerService) RebuildKnowledgeBaseIndex(ctx context.Context, id string) (*types.KnowledgeIndexRebuildProgress, error) {
	require.True(s.t, access.HasKBGrant(ctx, id, s.tenant, types.OrgRoleEditor))
	s.called = true
	return &types.KnowledgeIndexRebuildProgress{TaskID: "queued-task", KnowledgeBaseID: id, Status: "queued", DocumentCount: 6, DocumentsTotal: 6}, nil
}
func (s *indexRebuildHandlerService) GetKnowledgeBaseIndexRebuild(ctx context.Context, id string) (*types.KnowledgeIndexRebuildProgress, error) {
	require.True(s.t, access.HasKBGrant(ctx, id, s.tenant, types.OrgRoleViewer))
	s.called = true
	return &types.KnowledgeIndexRebuildProgress{TaskID: "queued-task", KnowledgeBaseID: id, Status: "completed"}, nil
}

func TestIndexRebuildHTTPAccessAndAcceptedContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		method     string
		owner      uint64
		permission types.OrgMemberRole
		scope      *types.TenantAPIKeyScope
		want       int
	}{
		{"owner starts", http.MethodPost, 1, "", nil, http.StatusAccepted},
		{"shared admin starts", http.MethodPost, 2, types.OrgRoleAdmin, nil, http.StatusAccepted},
		{"shared editor cannot change indexes", http.MethodPost, 2, types.OrgRoleEditor, nil, http.StatusForbidden},
		{"shared viewer cannot start", http.MethodPost, 2, types.OrgRoleViewer, nil, http.StatusForbidden},
		{"shared viewer sees progress", http.MethodGet, 2, types.OrgRoleViewer, nil, http.StatusOK},
		{"unshared cannot see progress", http.MethodGet, 2, "", nil, http.StatusForbidden},
		{"retrieve key cannot start", http.MethodPost, 1, "", &types.TenantAPIKeyScope{Capabilities: types.StringArray{"retrieve"}, KnowledgeBaseIDs: types.StringArray{"kb-1"}}, http.StatusForbidden},
		{"ingest key cannot escape KB scope", http.MethodPost, 1, "", &types.TenantAPIKeyScope{Capabilities: types.StringArray{"ingest"}, KnowledgeBaseIDs: types.StringArray{"other"}}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			kb := &vlmUpdateKBService{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: tc.owner}}
			shares := &vlmUpdateShareStub{permission: tc.permission}
			svc := &indexRebuildHandlerService{t: t, tenant: tc.owner}
			h := &KnowledgeHandler{kgService: svc, kbService: kb, kbShareService: shares}
			r := gin.New()
			r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
				if tc.scope != nil {
					ctx = types.WithTenantAPIKeyScope(ctx, *tc.scope)
				}
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			enabled := true
			required := types.OrgRoleEditor
			handle := h.RebuildKnowledgeBaseIndex
			if tc.method == http.MethodGet {
				required = types.OrgRoleViewer
				handle = h.GetKnowledgeBaseIndexRebuild
			}
			r.Handle(tc.method, "/:id/rebuild-index", middleware.RequireKBAccess(middleware.KBIDFromParam("id"), required, kb, shares, nil,
				&config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}), handle)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, "/kb-1/rebuild-index", nil))
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Equal(t, tc.want < 300, svc.called)
			if tc.want == http.StatusAccepted {
				require.Contains(t, w.Body.String(), `"document_count":6`)
				require.Contains(t, w.Body.String(), `"status":"queued"`)
			}
		})
	}
}
