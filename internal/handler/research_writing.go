package handler

import (
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/gin-gonic/gin"
)

func (h *ResearchDraftHandler) WritingRules(c *gin.Context) {
	result, err := h.service.WritingRules(c.Request.Context(), c.Param("kb_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) CreateWritingRule(c *gin.Context) {
	var req service.ResearchWritingRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.CreateWritingRule(c.Request.Context(), c.Param("kb_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) DecideWritingRule(c *gin.Context) {
	var req service.ResearchWritingRuleDecision
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.DecideWritingRule(c.Request.Context(), c.Param("kb_id"), c.Param("rule_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) ExtractWritingRules(c *gin.Context) {
	var req service.ResearchWritingExtractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.ExtractWritingRules(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) WritingFeedback(c *gin.Context) {
	result, err := h.service.WritingFeedback(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), c.Param("proposal_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) SaveWritingFeedback(c *gin.Context) {
	var req service.ResearchWritingFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.SaveWritingFeedback(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), c.Param("proposal_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) WritingEvents(c *gin.Context) {
	result, err := h.service.WritingEventsPage(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), c.Query("before_id"))
	researchResponse(c, result, err)
}
func (h *ResearchDraftHandler) WritingLearning(c *gin.Context) {
	result, err := h.service.WritingLearning(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"))
	researchResponse(c, result, err)
}
func (h *ResearchDraftHandler) SetWritingLearning(c *gin.Context) {
	var req service.ResearchWritingLearningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.SetWritingLearning(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), req)
	researchResponse(c, result, err)
}
func (h *ResearchDraftHandler) ExtractWritingEventRules(c *gin.Context) {
	var req service.ResearchWritingExtractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.ExtractWritingEventRules(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), c.Param("event_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) WritingEvent(c *gin.Context) {
	result, err := h.service.WritingEvent(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), c.Param("event_id"))
	researchResponse(c, result, err)
}
