package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

var ErrResearchProtected = errors.New("research drafts can only be changed through the reviewed research workspace")

type researchWriteKey struct{}

func isResearchDraft(page *types.WikiPage) bool {
	if page == nil {
		return false
	}
	if strings.HasPrefix(page.Slug, "research/") {
		return true
	}
	var metadata struct {
		Research bool `json:"research_draft"`
	}
	return json.Unmarshal(page.PageMetadata, &metadata) == nil && metadata.Research
}

func guardResearchDraft(ctx context.Context, page *types.WikiPage) error {
	if isResearchDraft(page) && ctx.Value(researchWriteKey{}) != true {
		return ErrResearchProtected
	}
	return nil
}
