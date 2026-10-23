package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
)

// Research tracing records identifiers and committed versions. Drafts, rule
// instructions, feedback prose and provider credentials are never metadata.
type researchObservation struct {
	span     *langfuse.Span
	metadata map[string]any
}

func startResearchObservation(ctx context.Context, operation, kbID, draftID string, metadata map[string]any) (context.Context, *researchObservation) {
	return startResearchObservationWithInput(ctx, operation, kbID, draftID, metadata, nil)
}

func startResearchObservationWithInput(ctx context.Context, operation, kbID, draftID string, metadata map[string]any, input any) (context.Context, *researchObservation) {
	values := map[string]any{"research_operation": operation}
	if kbID != "" {
		values["kb_id"] = kbID
	}
	if draftID != "" {
		values["draft_id"] = draftID
	}
	for key, value := range metadata {
		values[key] = value
	}
	session := ""
	if draftID != "" {
		session = "research:" + kbID + ":" + draftID
	}
	ctx = langfuse.WithObservationIdentity(ctx, researchWritingAuthor(ctx), session)
	ctx = langfuse.WithSafeObservationErrors(ctx)
	ctx = langfuse.WithObservationMetadata(ctx, values)
	ctx = types.WithLLMCallMetadata(ctx, "research."+operation, "")
	ctx, span := langfuse.GetManager().StartSpan(ctx, langfuse.SpanOptions{Name: "research." + operation, Input: input, Metadata: values})
	return ctx, &researchObservation{span: span, metadata: values}
}

func (o *researchObservation) With(ctx context.Context, metadata map[string]any) context.Context {
	for key, value := range metadata {
		o.metadata[key] = value
	}
	return langfuse.WithObservationMetadata(ctx, metadata)
}

func researchRuleVersions(raw []byte) map[string]int {
	var snapshots []types.ResearchWritingRuleSnapshot
	_ = json.Unmarshal(raw, &snapshots)
	versions := map[string]int{}
	for _, rule := range snapshots {
		versions[rule.ID] = rule.Version
	}
	return versions
}

func (o *researchObservation) Event(ctx context.Context, event *types.ResearchWritingEvent) context.Context {
	if event == nil {
		return ctx
	}
	hash := sha256.Sum256(event.SourceVersions)
	values := map[string]any{"draft_id": event.PageID, "event_id": event.ID, "event_kind": event.Kind,
		"base_version": event.BaseVersion, "result_version": event.ResultVersion, "event_status": event.Status,
		"source_versions_sha256": hex.EncodeToString(hash[:]), "source_count": len(event.SourceRefs),
		"rule_versions": researchRuleVersions(event.WritingRules)}
	if event.ProposalID != "" {
		values["proposal_id"] = event.ProposalID
	}
	if event.PromptEventID != "" {
		values["prompt_event_id"] = event.PromptEventID
	}
	if event.ParentEventID != "" {
		values["parent_event_id"] = event.ParentEventID
	}
	if event.ModelID != "" {
		values["model_id"] = event.ModelID
	}
	if snapshot, err := researchRetrievalFromVersions(event.SourceVersions); err == nil && snapshot != nil {
		ctx = o.With(ctx, researchRetrievalTraceMetadata(snapshot))
	}
	return o.With(ctx, values)
}

func researchRetrievalTraceMetadata(snapshot *researchRetrievalSnapshot) map[string]any {
	if snapshot == nil {
		return nil
	}
	raw, _ := json.Marshal(snapshot)
	hash := sha256.Sum256(raw)
	hits := make([]map[string]any, 0, len(snapshot.Results))
	for _, hit := range snapshot.Results {
		hits = append(hits, map[string]any{"knowledge_id": hit.KnowledgeID, "chunk_id": hit.ChunkID, "rank": hit.Rank,
			"score": hit.Score, "content_sha256": hit.ContentHash, "match_type": int(hit.MatchType)})
	}
	return map[string]any{"source_kb_id": snapshot.KnowledgeBaseID, "embedding_model_id": snapshot.EmbeddingModelID,
		"retrieval_snapshot_sha256": hex.EncodeToString(hash[:]), "retrieval_query_sha256": researchChunkHash(snapshot.Query),
		"retrieval_query_bytes": len(snapshot.Query), "retrieval_mode": snapshot.Mode, "retrieval_hit_count": len(hits), "retrieval_hits": hits}
}

func (o *researchObservation) Rules(ctx context.Context, rules []*types.ResearchWritingRule) context.Context {
	versions := map[string]int{}
	for _, rule := range rules {
		if rule != nil {
			versions[rule.ID] = rule.Version
		}
	}
	return o.With(ctx, map[string]any{"rule_versions": versions, "rule_count": len(versions)})
}

func (o *researchObservation) Finish(err error) {
	status := "completed"
	if err != nil {
		status = "failed"
	}
	o.metadata["outcome"] = status
	o.span.Finish(map[string]any{"outcome": status}, o.metadata, err)
}
