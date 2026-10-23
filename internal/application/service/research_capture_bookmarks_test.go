package service

import (
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestResearchBookmarksNetscapeFoldersAndDedup(t *testing.T) {
	html := `<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p><DT><H3>Research</H3><DL><p><DT><H3>AI4S</H3><DL><p><DT><A HREF="https://Example.com/paper?a=1&amp;b=2#section" ADD_DATE="1700000000">Science &amp; AI</A><DT><A HREF="javascript:alert(1)">Bad</A></DL><p><DT><A HREF="https://example.com/paper?a=1&amp;b=2#other">Duplicate</A><DT><A HREF="https://example.com/next">Next</A></DL></DL>`
	items, skipped, err := ParseResearchBookmarks(html)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, 2, skipped)
	require.Equal(t, "https://example.com/paper?a=1&b=2", items[0].URL)
	require.Equal(t, "Science & AI", items[0].Title)
	require.Equal(t, "Research / AI4S", items[0].Folder)
	require.Equal(t, int64(1700000000), items[0].AddedAt)
	require.Equal(t, "Research", items[1].Folder)
}
func TestResearchBookmarksRejectMalformedAndOversized(t *testing.T) {
	for _, value := range []string{"https://u:p@example.com/a", "file:///etc/passwd", "data:text/html,test", "https:///nohost"} {
		_, err := canonicalBookmarkURL(value)
		require.Error(t, err)
	}
	_, _, err := ParseResearchBookmarks(strings.Repeat("x", ResearchCaptureMaxHTML+1))
	require.Error(t, err)
	_, _, err = ParseResearchBookmarks("<html>not a bookmarks export</html>")
	require.Error(t, err)
}
func TestResearchSharingRequiresExactAudience(t *testing.T) {
	members := []*types.OrganizationTenantMember{{TenantID: 1}, {TenantID: 2}, {TenantID: 3}}
	require.True(t, exactResearchAudience(members, []uint64{3, 1, 2}))
	for _, selected := range [][]uint64{{1, 2}, {1, 2, 4}, {1, 1, 3}, {1, 2, 3, 4}, {}} {
		require.False(t, exactResearchAudience(members, selected))
	}
}
func TestResearchRequestHashBindsOperationAndScope(t *testing.T) {
	req := types.ResearchCaptureRequest{IdempotencyKey: "request-1", Items: []types.ResearchCaptureItem{{URL: "https://example.com"}}}
	h := captureRequestHash("bookmarks", "kb1", req)
	req.IdempotencyKey = "request-2"
	require.Equal(t, h, captureRequestHash("bookmarks", "kb1", req))
	require.NotEqual(t, h, captureRequestHash("bookmarks", "kb2", req))
	require.NotEqual(t, h, captureRequestHash("share", "kb1", req))
	req.Items[0].URL = "https://other.com"
	require.NotEqual(t, h, captureRequestHash("bookmarks", "kb1", req))
}

func TestResearchSharingDoesNotPublishUnselectedTargetContent(t *testing.T) {
	selected := []types.ResearchCaptureItem{{KnowledgeID: "selected"}}
	require.True(t, researchTargetContainsOnlySelection(nil, selected))
	require.True(t, researchTargetContainsOnlySelection([]*types.Knowledge{{ID: "selected"}}, selected))
	require.False(t, researchTargetContainsOnlySelection([]*types.Knowledge{{ID: "older-private"}}, selected))
	require.True(t, researchTargetPagesFromSelection(nil, selected))
	require.True(t, researchTargetPagesFromSelection([]types.WikiPage{{SourceRefs: types.StringArray{"selected|Title"}}}, selected))
	require.False(t, researchTargetPagesFromSelection([]types.WikiPage{{SourceRefs: types.StringArray{"older-private"}}}, selected))
	require.False(t, researchTargetPagesFromSelection([]types.WikiPage{{Content: "private authored analysis"}}, selected))
}
