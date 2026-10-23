package langfuse

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResearchTracingIncludesMutationsButExcludesPolling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/proposals", true},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/writing-rules/extract", true},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/writing-events/:event_id/extract", true},
		{"GET", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/writing-events", false},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/writing-learning", true},
		{"GET", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/proposals", false},
		{"GET", "/api/v1/knowledgebase/:kb_id/research/writing-rules", false},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/writing-rules/:rule_id/decide", true},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/proposals/:proposal_id/accept", true},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/proposals/:proposal_id/writing-feedback", true},
		{"PUT", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id", true},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts", true},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/revert", true},
		{"POST", "/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id/exports", false},
		{"POST", "/api/v1/knowledge-bases/:id/rebuild-index", true},
		{"GET", "/api/v1/knowledge-bases/:id/rebuild-index", false},
		{"POST", "/api/v1/knowledge-bases/:id/hybrid-search", true},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			router := gin.New()
			router.Handle(tc.method, tc.path, func(c *gin.Context) { require.Equal(t, tc.want, shouldTrace(c)) })
			req := httptest.NewRequest(tc.method, tc.path, nil)
			router.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}
