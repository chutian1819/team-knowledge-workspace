package handler

import (
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ResearchCaptureHandler struct {
	svc *service.ResearchCaptureService
}

func NewResearchCaptureHandler(svc *service.ResearchCaptureService) *ResearchCaptureHandler {
	return &ResearchCaptureHandler{svc}
}
func captureReply(c *gin.Context, data any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
		return
	}
	var app *werrors.AppError
	switch {
	case errors.As(err, &app):
		_ = c.Error(app)
	case errors.Is(err, access.ErrForbidden):
		_ = c.Error(werrors.NewForbiddenError("No permission to operate on this collection"))
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, repository.ErrKnowledgeBaseNotFound), errors.Is(err, repository.ErrKnowledgeNotFound):
		_ = c.Error(werrors.NewNotFoundError("Resource not found"))
	default:
		_ = c.Error(werrors.NewInternalServerError("Capture operation failed; retry or inspect server logs"))
	}
}
func (h *ResearchCaptureHandler) Inbox(c *gin.Context) {
	var req struct {
		Template string `json:"template_kb_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		captureReply(c, nil, werrors.NewBadRequestError(err.Error()))
		return
	}
	data, err := h.svc.Inbox(c.Request.Context(), req.Template)
	captureReply(c, data, err)
}
func (h *ResearchCaptureHandler) CurrentInbox(c *gin.Context) {
	data, err := h.svc.CurrentInbox(c.Request.Context())
	captureReply(c, data, err)
}
func (h *ResearchCaptureHandler) Preview(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
	var req struct {
		HTML string `json:"html" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		captureReply(c, nil, werrors.NewBadRequestError(err.Error()))
		return
	}
	items, skipped, err := service.ParseResearchBookmarks(req.HTML)
	if err != nil {
		err = werrors.NewBadRequestError(err.Error())
	}
	captureReply(c, gin.H{"items": items, "skipped": skipped}, err)
}
func (h *ResearchCaptureHandler) Import(c *gin.Context) { h.start(c, "bookmarks") }
func (h *ResearchCaptureHandler) Share(c *gin.Context)  { h.start(c, "share") }
func (h *ResearchCaptureHandler) start(c *gin.Context, kind string) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
	var req types.ResearchCaptureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		captureReply(c, nil, werrors.NewBadRequestError(err.Error()))
		return
	}
	data, err := h.svc.Start(c.Request.Context(), c.Param("kb_id"), kind, req)
	captureReply(c, data, err)
}
func (h *ResearchCaptureHandler) Job(c *gin.Context) {
	data, err := h.svc.Job(c.Request.Context(), c.Param("kb_id"), c.Param("job_id"))
	captureReply(c, data, err)
}
func (h *ResearchCaptureHandler) Jobs(c *gin.Context) {
	data, err := h.svc.Jobs(c.Request.Context(), c.Param("kb_id"))
	captureReply(c, data, err)
}
func (h *ResearchCaptureHandler) Retry(c *gin.Context) {
	data, err := h.svc.Retry(c.Request.Context(), c.Param("kb_id"), c.Param("job_id"))
	captureReply(c, data, err)
}

func (h *ResearchCaptureHandler) SourceVersions(c *gin.Context) {
	data, err := h.svc.SourceVersions(c.Request.Context(), c.Param("kb_id"), c.Param("knowledge_id"))
	captureReply(c, data, err)
}

func (h *ResearchCaptureHandler) ReplaceSourceFile(c *gin.Context) {
	maxSize := utils.GetMaxFileSizeMB() * 1024 * 1024
	limitUploadBody(c, maxSize)
	file, err := c.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil || file == nil {
		captureReply(c, nil, werrors.NewBadRequestError("Provide an original file within the configured upload limit"))
		return
	}
	if file.Size > maxSize {
		captureReply(c, nil, werrors.NewBadRequestError("File exceeds the configured upload limit"))
		return
	}
	data, err := h.svc.ReplaceSourceFile(c.Request.Context(), c.Param("kb_id"), c.Param("knowledge_id"), file, c.PostForm("file_name"))
	captureReply(c, data, err)
}
