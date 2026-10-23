package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

// RegisterResearchCaptureRoutes uses the native RBAC and KB grants. The service
// additionally requires exact creator ownership because these operations expose
// documents to a new audience; read/edit access alone must not authorize sharing.
func RegisterResearchCaptureRoutes(r *gin.RouterGroup, h *handler.ResearchCaptureHandler, g *rbacGuards) {
	group := g.apiKeyGroup(r.Group("/research/capture"), apiKeyFullAccess())
	group.GET("/inbox", g.Viewer(), h.CurrentInbox)
	group.POST("/inbox", g.Contributor(), h.Inbox)
	group.POST("/bookmarks/preview", g.Contributor(), h.Preview)
	group.POST("/:kb_id/bookmarks/import", g.Contributor(), g.KBAccessWrite("kb_id"), h.Import)
	group.POST("/:kb_id/share", g.Contributor(), g.KBAccessWrite("kb_id"), h.Share)
	group.GET("/:kb_id/jobs", g.Viewer(), g.KBAccessRead("kb_id"), h.Jobs)
	group.GET("/:kb_id/jobs/:job_id", g.Viewer(), g.KBAccessRead("kb_id"), h.Job)
	group.GET("/:kb_id/sources/:knowledge_id", g.Viewer(), g.KBAccessRead("kb_id"), h.SourceVersions)
	group.PUT("/:kb_id/sources/:knowledge_id/file", g.Contributor(), g.KBAccessWrite("kb_id"), h.ReplaceSourceFile)
	group.POST("/:kb_id/jobs/:job_id/retry", g.Contributor(), g.KBAccessWrite("kb_id"), h.Retry)
}
