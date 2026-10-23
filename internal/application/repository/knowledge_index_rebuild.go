package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpdateSourceIndexRebuildState publishes readiness only for the exact content
// that was indexed. UpdateColumns deliberately leaves updated_at alone:
// refreshing a derived index must not change frozen writing provenance.
func (r *knowledgeRepository) UpdateSourceIndexRebuildState(ctx context.Context, source *types.Knowledge, chunks []*types.Chunk, embeddingModelID, indexStatus string) (bool, error) {
	if source == nil || source.ID == "" || source.TenantID == 0 || source.KnowledgeBaseID == "" {
		return false, fmt.Errorf("invalid index rebuild source")
	}
	if indexStatus != "processing" && indexStatus != "ready" && indexStatus != "failed" {
		return false, fmt.Errorf("invalid rebuild index status")
	}
	snapshot := func(s *types.Knowledge, list []*types.Chunk) []byte {
		ordered := append([]*types.Chunk(nil), list...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
		raw, _ := json.Marshal([]any{s, ordered})
		return raw
	}
	committed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.Knowledge
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", source.TenantID, source.KnowledgeBaseID, source.ID)
		if err := query.First(&current).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}
		var currentChunks []*types.Chunk
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND knowledge_id = ? AND chunk_type IN ?", source.TenantID, source.KnowledgeBaseID, source.ID,
			[]string{types.ChunkTypeText, types.ChunkTypeImageOCR, types.ChunkTypeImageCaption}).Order("id").Find(&currentChunks).Error; err != nil {
			return err
		}
		if !bytes.Equal(snapshot(source, chunks), snapshot(&current, currentChunks)) {
			return nil
		}
		ids := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			if strings.TrimSpace(chunk.Content) != "" {
				ids = append(ids, chunk.ID)
			}
		}
		fields := map[string]any{"index_status": indexStatus}
		if indexStatus == "ready" {
			fields["status"] = types.ChunkStatusIndexed
		}
		if len(ids) > 0 {
			if err := tx.Model(&types.Chunk{}).Where("tenant_id = ? AND knowledge_base_id = ? AND knowledge_id = ? AND id IN ?", source.TenantID, source.KnowledgeBaseID, source.ID, ids).
				UpdateColumns(fields).Error; err != nil {
				return err
			}
		}
		if indexStatus == "ready" {
			if err := tx.Model(&types.Knowledge{}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", source.TenantID, source.KnowledgeBaseID, source.ID).
				UpdateColumn("embedding_model_id", embeddingModelID).Error; err != nil {
				return err
			}
		}
		committed = true
		return nil
	})
	return committed, err
}
