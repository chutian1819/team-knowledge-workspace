package docparser

import (
	"context"
	"errors"
	"fmt"

	"github.com/Tencent/WeKnora/docreader/proto"
)

// ExportResearch is an optional DocumentReader capability. Keeping it separate
// preserves existing parser implementations and mocks without a new service.
func (p *GRPCDocumentReader) ExportResearch(ctx context.Context, title, markdown, format string, images map[string][]byte) ([]byte, error) {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()
	if client == nil {
		return nil, errors.New("document service is disconnected")
	}
	request := &proto.ExportResearchRequest{Title: title, Markdown: markdown, Format: format}
	for ref, content := range images {
		request.Images = append(request.Images, &proto.ImageRef{OriginalRef: ref, ImageData: content})
	}
	result, err := client.ExportResearch(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("research conversion failed: %w", err)
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	if len(result.Content) == 0 {
		return nil, errors.New("converter returned an empty file")
	}
	return result.Content, nil
}
