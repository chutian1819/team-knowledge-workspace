package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// This reserved key lives in the existing immutable source-version JSON. UUID
// source IDs cannot collide with it. No client-supplied result body is accepted.
const researchRetrievalKey = "_retrieval"
const researchRetrievalLimit = 8

type researchRetrievalHit struct {
	KnowledgeID string          `json:"knowledge_id"`
	ChunkID     string          `json:"chunk_id"`
	ChunkType   string          `json:"chunk_type"`
	Content     string          `json:"content"`
	ContentHash string          `json:"content_sha256"`
	Rank        int             `json:"rank"`
	Score       float64         `json:"score"`
	MatchType   types.MatchType `json:"match_type"`
}

type researchRetrievalSnapshot struct {
	Version          int                    `json:"version"`
	Query            string                 `json:"query"`
	KnowledgeBaseID  string                 `json:"knowledge_base_id"`
	EmbeddingModelID string                 `json:"embedding_model_id"`
	MatchCount       int                    `json:"match_count"`
	VectorThreshold  float64                `json:"vector_threshold"`
	Mode             string                 `json:"mode"`
	CapturedAt       time.Time              `json:"captured_at"`
	Results          []researchRetrievalHit `json:"results"`
	ExcludedChunkIDs []string               `json:"excluded_chunk_ids,omitempty"`
}

func researchChunkHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}
func readableResearchChunk(kind string) bool {
	return kind == types.ChunkTypeText || kind == types.ChunkTypeImageOCR || kind == types.ChunkTypeImageCaption
}
func researchRetrievalFromVersions(raw types.JSON) (*researchRetrievalSnapshot, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var versions map[string]json.RawMessage
	if err := json.Unmarshal(raw, &versions); err != nil {
		return nil, researchInput("invalid stored source versions")
	}
	data, ok := versions[researchRetrievalKey]
	if !ok {
		return nil, nil
	}
	var snapshot researchRetrievalSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil || snapshot.Version != 1 || snapshot.Mode != "vector_only" || len(snapshot.Results) == 0 || len(snapshot.Results) > researchRetrievalLimit {
		return nil, researchInput("invalid stored retrieval evidence")
	}
	return &snapshot, nil
}
func researchPageRetrieval(page *types.WikiPage) (*researchRetrievalSnapshot, error) {
	var metadata struct {
		SourceVersions types.JSON `json:"source_versions"`
	}
	if len(page.PageMetadata) > 0 {
		if err := json.Unmarshal(page.PageMetadata, &metadata); err != nil {
			return nil, researchInput("invalid stored draft metadata")
		}
	}
	return researchRetrievalFromVersions(metadata.SourceVersions)
}
func researchVersionsWithRetrieval(versions map[string]any, snapshot *researchRetrievalSnapshot) types.JSON {
	if snapshot != nil {
		versions[researchRetrievalKey] = snapshot
	}
	raw, _ := json.Marshal(versions)
	return raw
}
func researchSourceVersionsEqual(current map[string]any, stored types.JSON) bool {
	var previous map[string]json.RawMessage
	if json.Unmarshal(stored, &previous) != nil {
		return false
	}
	delete(previous, researchRetrievalKey)
	before, _ := json.Marshal(previous)
	after, _ := json.Marshal(current)
	// Decode through the same representation so nested key ordering is stable.
	var normalized any
	_ = json.Unmarshal(before, &normalized)
	before, _ = json.Marshal(normalized)
	return string(before) == string(after)
}

// Search uses the existing engine in vector-only mode. Missing configuration,
// failed/empty vector recall and unauthorized hits never fall back to keywords
// or to reading complete documents.
func (s *ResearchDraftService) retrieveResearchEvidence(ctx context.Context, destinationKB, sourceKB, query string, allowedIDs []string) (resultSnapshot *researchRetrievalSnapshot, resultSources []*types.Knowledge, resultErr error) {
	query = strings.TrimSpace(query)
	ctx, observation := startResearchObservationWithInput(ctx, "retrieval", destinationKB, "", map[string]any{"source_kb_id": sourceKB, "retrieval_query_sha256": researchChunkHash(query), "retrieval_query_bytes": len(query)}, map[string]any{"query_text": query})
	defer func() {
		if resultErr == nil {
			observation.With(ctx, researchRetrievalTraceMetadata(resultSnapshot))
		}
		observation.Finish(resultErr)
	}()
	if query == "" || len(query) > 4000 || sourceKB == "" {
		return nil, nil, researchInput("语义检索需要来源资料集和 1–4000 字节的检索问题")
	}
	kb, err := s.kb.GetKnowledgeBaseByIDOnly(ctx, sourceKB)
	if err != nil || kb == nil {
		return nil, nil, access.ErrNotFound
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.OrgRoleViewer, s.shares, s.agents)
	if err != nil {
		return nil, nil, err
	}
	if !kb.IsVectorEnabled() || kb.EmbeddingModelID == "" {
		return nil, nil, researchInput("来源资料集尚未启用向量索引或配置 Embedding 模型；不能进行语义检索写作")
	}
	ctx = observation.With(ctx, map[string]any{"embedding_model_id": kb.EmbeddingModelID})
	allowed := map[string]bool{}
	if len(allowedIDs) > 0 {
		sources, err := s.sources(ctx, destinationKB, allowedIDs)
		if err != nil {
			return nil, nil, err
		}
		for _, source := range sources {
			if source.KnowledgeBaseID == sourceKB {
				allowed[source.ID] = true
			}
		}
		if len(allowed) == 0 {
			return nil, nil, researchInput("检索资料集不在本稿已关联的来源范围内")
		}
	}
	scopedIDs := make([]string, 0, len(allowed))
	for _, id := range allowedIDs {
		if allowed[id] {
			scopedIDs = append(scopedIDs, id)
		}
	}
	params := types.SearchParams{QueryText: query, MatchCount: researchRetrievalLimit, VectorThreshold: 0.2,
		DisableKeywordsMatch: true, DisableVectorMatch: false, KnowledgeIDs: scopedIDs, SkipContextEnrichment: true}
	hits, err := s.kb.HybridSearch(grant.Context(ctx), sourceKB, params)
	if err != nil {
		return nil, nil, fmt.Errorf("semantic retrieval failed: %w", err)
	}
	snapshot := &researchRetrievalSnapshot{Version: 1, Query: query, KnowledgeBaseID: sourceKB, EmbeddingModelID: kb.EmbeddingModelID,
		MatchCount: params.MatchCount, VectorThreshold: params.VectorThreshold, Mode: "vector_only", CapturedAt: time.Now().UTC(), Results: []researchRetrievalHit{}}
	ids, seen := []string{}, map[string]bool{}
	total := 0
	for rank, hit := range hits {
		if hit == nil || hit.ID == "" || hit.KnowledgeID == "" {
			return nil, nil, researchInput("检索返回了无效的证据定位")
		}
		if hit.MatchType != types.MatchTypeEmbedding {
			return nil, nil, researchInput("检索结果不是向量命中，未将其用于语义检索写作")
		}
		if len(allowed) > 0 && !allowed[hit.KnowledgeID] {
			return nil, nil, access.ErrForbidden
		}
		source, err := s.knowledge.GetKnowledgeByIDOnly(ctx, hit.KnowledgeID)
		if err != nil || source == nil || source.KnowledgeBaseID != sourceKB || source.TenantID != kb.TenantID {
			return nil, nil, access.ErrNotFound
		}
		if source.ParseStatus != types.ParseStatusCompleted && source.ParseStatus != "ready" {
			return nil, nil, researchInput("检索来源尚未完成解析，请等待索引完成后重试")
		}
		if source.EnableStatus != "enabled" || source.EmbeddingModelID != kb.EmbeddingModelID {
			return nil, nil, researchInput("检索来源索引尚未就绪或模型已变化，请完成重建索引后重试")
		}
		chunk, err := s.chunks.GetChunkByID(grant.Context(ctx), source.TenantID, hit.ID)
		if err != nil || chunk == nil || chunk.KnowledgeID != source.ID || chunk.KnowledgeBaseID != sourceKB {
			return nil, nil, access.ErrNotFound
		}
		if chunk.IndexStatus != "ready" || !chunk.IsEnabled {
			return nil, nil, researchInput("检索片段的向量索引尚未就绪，请等待索引完成后重试")
		}
		if !readableResearchChunk(chunk.ChunkType) || realTextRuneCount(chunk.Content) == 0 {
			snapshot.ExcludedChunkIDs = append(snapshot.ExcludedChunkIDs, chunk.ID)
			continue
		}
		if seen["chunk:"+chunk.ID] {
			continue
		}
		seen["chunk:"+chunk.ID] = true
		if !seen[source.ID] {
			ids = append(ids, source.ID)
			seen[source.ID] = true
		}
		total += len(chunk.Content)
		if len(snapshot.Results) >= researchRetrievalLimit || total > 100000 {
			return nil, nil, researchInput("检索证据超过写作预算；未截断证据，请缩小检索问题")
		}
		snapshot.Results = append(snapshot.Results, researchRetrievalHit{KnowledgeID: source.ID, ChunkID: chunk.ID, ChunkType: chunk.ChunkType,
			Content: chunk.Content, ContentHash: researchChunkHash(chunk.Content), Rank: rank + 1, Score: hit.Score, MatchType: hit.MatchType})
	}
	if len(snapshot.Results) == 0 {
		return nil, nil, researchInput("没有检索到可用于写作的文字证据；请确认向量索引完成或调整问题，未降级为全文或关键词写作")
	}
	sources, err := s.sources(ctx, destinationKB, ids)
	if err != nil {
		return nil, nil, err
	}
	return snapshot, sources, nil
}

// Re-read exact canonical chunks before inference, after inference, and before
// acceptance. A changed chunk fails even if its parent file hash is unchanged.
func (s *ResearchDraftService) validateResearchRetrieval(ctx context.Context, sources []*types.Knowledge, snapshot *researchRetrievalSnapshot) error {
	if snapshot == nil {
		return nil
	}
	byID := map[string]*types.Knowledge{}
	for _, source := range sources {
		byID[source.ID] = source
	}
	for _, hit := range snapshot.Results {
		source := byID[hit.KnowledgeID]
		if source == nil || source.KnowledgeBaseID != snapshot.KnowledgeBaseID {
			return access.ErrForbidden
		}
		if source.EnableStatus != "enabled" || (source.ParseStatus != types.ParseStatusCompleted && source.ParseStatus != "ready") {
			return fmt.Errorf("%w: retrieval source is no longer readable or enabled", ErrResearchConflict)
		}
		chunk, err := s.chunks.GetChunkByID(ctx, source.TenantID, hit.ChunkID)
		if err != nil || chunk == nil || chunk.KnowledgeID != source.ID || chunk.KnowledgeBaseID != source.KnowledgeBaseID ||
			!chunk.IsEnabled || !readableResearchChunk(chunk.ChunkType) || chunk.ChunkType != hit.ChunkType || chunk.Content != hit.Content || researchChunkHash(chunk.Content) != hit.ContentHash {
			return fmt.Errorf("%w: retrieval evidence changed; search again before generating or accepting", ErrResearchConflict)
		}
	}
	return nil
}

func (s *ResearchDraftService) prepareProposalRetrieval(ctx context.Context, page *types.WikiPage, sources []*types.Knowledge, req *ResearchProposeRequest) error {
	if req.RetrievalQuery != "" || req.RetrievalKBID != "" {
		if req.Content != nil {
			return researchInput("manual content cannot claim a model retrieval")
		}
		if len(sources) == 0 {
			return researchInput("请先关联来源资料，再进行语义检索写作")
		}
		snapshot, _, err := s.retrieveResearchEvidence(ctx, page.KnowledgeBaseID, req.RetrievalKBID, req.RetrievalQuery, researchSources(page))
		if err != nil {
			return err
		}
		req.retrieval = snapshot
	} else {
		snapshot, err := researchPageRetrieval(page)
		if err != nil {
			return err
		}
		req.retrieval = snapshot
	}
	return s.validateResearchRetrieval(ctx, sources, req.retrieval)
}

func appendResearchRetrievedEvidence(evidence *strings.Builder, allowed map[string]bool, snapshot *researchRetrievalSnapshot, sources []*types.Knowledge, bodyBytes int) error {
	titles := map[string]string{}
	for _, source := range sources {
		titles[source.ID] = source.Title
	}
	for _, hit := range snapshot.Results {
		fmt.Fprintf(evidence, "\nSOURCE %s: %s\n\nCHUNK %s\n%s\n", hit.KnowledgeID, titles[hit.KnowledgeID], hit.ChunkID, hit.Content)
		allowed["knowledge://"+hit.KnowledgeID+"/chunks/"+hit.ChunkID] = true
		if evidence.Len()+bodyBytes > 120000 {
			return researchInput("retrieved evidence exceeds 120,000 bytes; no partial reading was performed")
		}
	}
	return nil
}
