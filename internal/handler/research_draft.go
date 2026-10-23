package handler

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type ResearchDraftHandler struct{ service *service.ResearchDraftService }

func NewResearchDraftHandler(s *service.ResearchDraftService) *ResearchDraftHandler {
	return &ResearchDraftHandler{service: s}
}

func researchResponse(c *gin.Context, result any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"data": result})
		return
	}
	code := http.StatusInternalServerError
	var app *apperrors.AppError
	switch {
	case errors.Is(err, service.ErrResearchConflict):
		code = http.StatusConflict
	case errors.Is(err, service.ErrResearchInput):
		code = http.StatusBadRequest
	case errors.Is(err, access.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, access.ErrUnauthorized):
		code = http.StatusUnauthorized
	case errors.Is(err, access.ErrForbidden), errors.Is(err, service.ErrResearchProtected):
		code = http.StatusForbidden
	case errors.As(err, &app):
		code = app.HTTPCode
	}
	c.JSON(code, gin.H{"error": err.Error(), "message": err.Error()})
}

func (h *ResearchDraftHandler) Templates(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": types.ResearchTemplates()})
}

func (h *ResearchDraftHandler) List(c *gin.Context) {
	result, err := h.service.List(c.Request.Context(), c.Param("kb_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Get(c *gin.Context) {
	result, err := h.service.Get(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Create(c *gin.Context) {
	var req service.ResearchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.Create(c.Request.Context(), c.Param("kb_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Save(c *gin.Context) {
	var req service.ResearchSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.Save(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Propose(c *gin.Context) {
	var req service.ResearchProposeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.Propose(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), req)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Proposals(c *gin.Context) {
	result, err := h.service.Proposals(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Accept(c *gin.Context) {
	result, err := h.service.Decide(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), c.Param("proposal_id"), true)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Reject(c *gin.Context) {
	result, err := h.service.Decide(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), c.Param("proposal_id"), false)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Revisions(c *gin.Context) {
	result, err := h.service.Revisions(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Revert(c *gin.Context) {
	var req struct {
		Version     int `json:"version"`
		BaseVersion int `json:"base_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.Revert(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), req.Version, req.BaseVersion)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Export(c *gin.Context) {
	var req struct {
		Format string `json:"format"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	result, err := h.service.RequestExport(c.Request.Context(), c.Param("kb_id"), c.Param("draft_id"), req.Format)
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) AssetExport(c *gin.Context) {
	result, err := h.service.RequestAssetExport(c.Request.Context(), c.Param("kb_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) ExportStatus(c *gin.Context) {
	result, err := h.service.GetExport(c.Request.Context(), c.Param("kb_id"), c.Param("export_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Exports(c *gin.Context) {
	result, err := h.service.ListExports(c.Request.Context(), c.Param("kb_id"), c.Query("page_id"))
	researchResponse(c, result, err)
}

func (h *ResearchDraftHandler) Download(c *gin.Context) {
	reader, export, err := h.service.DownloadExport(c.Request.Context(), c.Param("kb_id"), c.Param("export_id"))
	if err != nil {
		researchResponse(c, nil, err)
		return
	}
	defer reader.Close()
	mime := map[string]string{"md": "text/markdown; charset=utf-8", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "pdf": "application/pdf", "zip": "application/zip"}[export.Format]
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(200, -1, mime, reader, map[string]string{
		"Content-Disposition": fmt.Sprintf("attachment; filename=%s", strconv.Quote(export.FileName)),
	})
}

func (h *ResearchDraftHandler) SourceVersionAsset(c *gin.Context) {
	reader, name, err := h.service.SourceVersionAsset(c.Request.Context(), c.Param("kb_id"),
		c.Param("knowledge_id"), c.Param("version_id"), c.Param("kind"))
	if err != nil {
		researchResponse(c, nil, err)
		return
	}
	defer reader.Close()
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	contentType := "text/plain; charset=utf-8"
	if c.Param("kind") == "original" {
		contentType = "application/octet-stream"
	}
	c.DataFromReader(http.StatusOK, -1, contentType, reader, map[string]string{
		"Content-Disposition": mime.FormatMediaType("attachment", map[string]string{"filename": name}),
	})
}
