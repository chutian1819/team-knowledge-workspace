package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
)

const imageArchiveNotice = "原图已保存，尚未进行文字识别或图片理解；不可作为文字证据用于写作。配置视觉模型后可重新解析。"

// archiveImageOriginal deliberately stops before the parser, chunker and all
// model work. A reference to an image is not evidence of its contents.
func (s *knowledgeService) archiveImageOriginal(ctx context.Context, kb *types.KnowledgeBase, knowledge *types.Knowledge) error {
	if !IsImageType(normalizeFileExtension(knowledge.FileType)) || knowledge.FilePath == "" {
		return fmt.Errorf("original-image archival requires an image file")
	}
	if _, ok := s.repo.(researchSourceRepository); !ok {
		return fmt.Errorf("original-image archival requires source version storage")
	}
	markdown := fmt.Sprintf("%s\n\n![原图](%s)\n", imageArchiveNotice, knowledge.FilePath)
	result := &types.ReadResult{MarkdownContent: markdown}
	images := []docparser.StoredImage{{ServingURL: knowledge.FilePath}}
	if err := s.archiveResearchSource(ctx, kb, knowledge, result, markdown, "", images); err != nil {
		knowledge.ParseStatus = types.ParseStatusFailed
		knowledge.ErrorMessage = "原图归档失败，请重试；尚未进行图片识别。"
		knowledge.UpdatedAt = time.Now()
		_ = s.updateKnowledgeUnlessSourceReplaced(ctx, knowledge)
		return fmt.Errorf("archive original image: %w", err)
	}
	if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
		return abortRetryErr(ctx, knowledge.ID, status)
	}
	knowledge.ParseStatus = types.ParseStatusArchived
	knowledge.EnableStatus = "disabled"
	knowledge.ErrorMessage = imageArchiveNotice
	knowledge.PendingSubtasksCount = 0
	knowledge.UpdatedAt = time.Now()
	if err := s.updateKnowledgeUnlessSourceReplaced(ctx, knowledge); err != nil {
		return err
	}
	for _, stage := range []string{types.StageDocReader, types.StageChunking, types.StageEmbedding, types.StageMultimodal, types.StagePostProcess} {
		s.skipStage(ctx, knowledge.ID, stage, "original image archived without recognition")
	}
	return nil
}
