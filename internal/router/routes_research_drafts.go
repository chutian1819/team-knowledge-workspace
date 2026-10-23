package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterResearchDraftRoutes(r *gin.RouterGroup, h *handler.ResearchDraftHandler, g *rbacGuards) {
	research := g.apiKeyGroup(r.Group("/knowledgebase/:kb_id/research"), apiKeyIngest(apiKeyFullAccess()))
	read := research.With(apiKeyRetrieve(apiKeyFullAccess()))
	read.GET("/templates", g.Viewer(), g.KBAccessRead("kb_id"), h.Templates)
	read.GET("/drafts", g.Viewer(), g.KBAccessRead("kb_id"), h.List)
	read.GET("/drafts/:draft_id", g.Viewer(), g.KBAccessRead("kb_id"), h.Get)
	read.GET("/drafts/:draft_id/writing-events", g.Viewer(), g.KBAccessRead("kb_id"), h.WritingEvents)
	read.GET("/drafts/:draft_id/writing-events/:event_id", g.Viewer(), g.KBAccessRead("kb_id"), h.WritingEvent)
	read.GET("/drafts/:draft_id/writing-learning", g.Viewer(), g.KBAccessRead("kb_id"), h.WritingLearning)
	research.POST("/drafts/:draft_id/writing-learning", g.Contributor(), g.KBAccessWrite("kb_id"), h.SetWritingLearning)
	research.POST("/drafts/:draft_id/writing-events/:event_id/extract", g.Contributor(), g.KBAccessWrite("kb_id"), h.ExtractWritingEventRules)
	read.GET("/writing-rules", g.Viewer(), g.KBAccessRead("kb_id"), h.WritingRules)
	research.POST("/writing-rules", g.Contributor(), g.KBAccessWrite("kb_id"), h.CreateWritingRule)
	research.POST("/writing-rules/:rule_id/decide", g.Contributor(), g.KBAccessWrite("kb_id"), h.DecideWritingRule)
	research.POST("/drafts/:draft_id/writing-rules/extract", g.Contributor(), g.KBAccessWrite("kb_id"), h.ExtractWritingRules)
	read.GET("/drafts/:draft_id/proposals/:proposal_id/writing-feedback", g.Viewer(), g.KBAccessRead("kb_id"), h.WritingFeedback)
	research.POST("/drafts/:draft_id/proposals/:proposal_id/writing-feedback", g.Contributor(), g.KBAccessWrite("kb_id"), h.SaveWritingFeedback)
	research.POST("/drafts", g.Contributor(), g.KBAccessWrite("kb_id"), h.Create)
	research.PUT("/drafts/:draft_id", g.Contributor(), g.KBAccessWrite("kb_id"), h.Save)
	research.POST("/drafts/:draft_id/proposals", g.Contributor(), g.KBAccessWrite("kb_id"), h.Propose)
	read.GET("/drafts/:draft_id/proposals", g.Viewer(), g.KBAccessRead("kb_id"), h.Proposals)
	research.POST("/drafts/:draft_id/proposals/:proposal_id/accept", g.Contributor(), g.KBAccessWrite("kb_id"), h.Accept)
	research.POST("/drafts/:draft_id/proposals/:proposal_id/reject", g.Contributor(), g.KBAccessWrite("kb_id"), h.Reject)
	read.GET("/drafts/:draft_id/revisions", g.Viewer(), g.KBAccessRead("kb_id"), h.Revisions)
	research.POST("/drafts/:draft_id/revert", g.Contributor(), g.KBAccessWrite("kb_id"), h.Revert)
	research.POST("/drafts/:draft_id/exports", g.Viewer(), g.KBAccessRead("kb_id"), h.Export)
	research.POST("/asset-exports", g.Contributor(), g.KBAccessWrite("kb_id"), h.AssetExport)
	read.GET("/exports", g.Viewer(), g.KBAccessRead("kb_id"), h.Exports)
	read.GET("/exports/:export_id", g.Viewer(), g.KBAccessRead("kb_id"), h.ExportStatus)
	read.GET("/exports/:export_id/download", g.Viewer(), g.KBAccessRead("kb_id"), h.Download)
	read.GET("/sources/:knowledge_id/versions/:version_id/assets/:kind", g.Viewer(), g.KBAccessRead("kb_id"), h.SourceVersionAsset)
}
