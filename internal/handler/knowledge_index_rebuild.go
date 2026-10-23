package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// RebuildKnowledgeBaseIndex refreshes derived indexes without re-parsing or
// replacing source/chunk/history records. KB settings rights are required
// because the task consumes the owner's embedding model and storage.
func (h *KnowledgeHandler) RebuildKnowledgeBaseIndex(c *gin.Context) {
	kb, _, _, _, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if _, err = kbSettingsAccess(c, kb); err != nil {
		_ = c.Error(err)
		return
	}
	progress, err := h.kgService.RebuildKnowledgeBaseIndex(c.Request.Context(), kb.ID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": progress})
}

func (h *KnowledgeHandler) GetKnowledgeBaseIndexRebuild(c *gin.Context) {
	kb, _, _, permission, err := h.validateKnowledgeBaseAccessWithKBID(c, c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if !permission.HasPermission(types.OrgRoleViewer) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	progress, err := h.kgService.GetKnowledgeBaseIndexRebuild(c.Request.Context(), kb.ID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": progress})
}
