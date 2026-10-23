package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// This is a wire/behavior regression test, not a real Langfuse acceptance.
// A local OTLP receiver lets us inspect actual exported business spans without
// granting tests production keys or making provider requests.
func researchTraceReceiver(t *testing.T, reject bool) (*langfuse.Manager, func() []*tracepb.Span, *atomic.Int32) {
	t.Helper()
	var mu sync.Mutex
	var spans []*tracepb.Span
	requests := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if reject {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		var request collectortrace.ExportTraceServiceRequest
		if proto.Unmarshal(data, &request) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				spans = append(spans, scope.Spans...)
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	mgr, err := langfuse.Init(langfuse.Config{Enabled: true, Host: server.URL, PublicKey: "test-public", SecretKey: "test-secret",
		FlushAt: 32, FlushInterval: 10 * time.Millisecond, QueueSize: 512, RequestTimeout: 200 * time.Millisecond, SampleRate: 1})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mgr.Shutdown(context.Background())
		server.Close()
		_, _ = langfuse.Init(langfuse.Config{})
	})
	return mgr, func() []*tracepb.Span { mu.Lock(); defer mu.Unlock(); return append([]*tracepb.Span{}, spans...) }, requests
}

type researchTracingChat struct {
	researchChatFixture
	contexts []map[string]any
}

func (m *researchTracingChat) Chat(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	m.contexts = append(m.contexts, langfuse.ObservationMetadataFromContext(ctx))
	purpose, _ := types.LLMCallMetadataFromContext(ctx)
	_, generation := langfuse.GetManager().StartGeneration(ctx, langfuse.GenerationOptions{Name: "chat.completion", Model: "test-model", Metadata: map[string]any{"model_id": "model", "call_purpose": purpose}})
	generation.Finish(m.response.Content, &langfuse.TokenUsage{Input: 8, Output: 4, Total: 12}, nil)
	return m.response, nil
}

func tracedResearchMetadata(t *testing.T, span *tracepb.Span) map[string]any {
	t.Helper()
	for _, attr := range span.Attributes {
		if attr.Key == "langfuse.observation.metadata" {
			if attr.Value.GetStringValue() == "" {
				return nil
			}
			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(attr.Value.GetStringValue()), &result))
			return result
		}
	}
	return nil
}

func TestResearchTracingConnectsSavedRevisionLearningAndRuleReuse(t *testing.T) {
	mgr, exported, _ := researchTraceReceiver(t, false)
	ctx, s := researchWritingHarness(t)
	prepareWritingWorkerAccount(t, s)
	queue := &researchQueueFixture{}
	s.task = queue
	model := &researchTracingChat{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: `{"rules":[{"name":"短段落","instruction":"每段只陈述一个判断。"}]}`, FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Trace regression", Content: "原稿"})
	require.NoError(t, err)
	_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: true, ModelID: "model"})
	require.NoError(t, err)
	saveCtx, request := mgr.StartTrace(ctx, langfuse.TraceOptions{Name: "save-request"})
	page, err = s.Save(saveCtx, "research-kb", page.ID, ResearchSaveRequest{Title: page.Title, Content: "人工修改", BaseVersion: page.Version})
	require.NoError(t, err)
	request.Finish(nil, nil)
	require.NotNil(t, queue.task)
	var payload researchWritingLearningPayload
	require.NoError(t, json.Unmarshal(queue.task.Payload(), &payload))
	require.NotEmpty(t, payload.LangfuseTraceparent)
	require.Equal(t, request.ID, payload.LangfuseTraceID)
	require.Equal(t, "research:research-kb:"+page.ID, payload.LangfuseSessionID)
	worker := langfuse.AsynqMiddleware()(asynq.HandlerFunc(s.HandleResearchWritingLearning))
	require.NoError(t, worker.ProcessTask(context.Background(), queue.task))
	require.Len(t, model.contexts, 1)
	require.Equal(t, page.ID, model.contexts[0]["draft_id"])
	require.Equal(t, payload.EventID, model.contexts[0]["event_id"])
	require.Equal(t, "model", model.contexts[0]["model_id"])
	require.Equal(t, "research-kb", model.contexts[0]["kb_id"])
	rules, err := s.WritingRules(ctx, "research-kb")
	require.NoError(t, err)
	require.Len(t, rules, 1)
	rule, err := s.DecideWritingRule(ctx, "research-kb", rules[0].ID, ResearchWritingRuleDecision{BaseVersion: 1, Status: "active"})
	require.NoError(t, err)
	_, err = s.SetWritingLearning(ctx, "research-kb", page.ID, ResearchWritingLearningRequest{Enabled: false})
	require.NoError(t, err)
	model.response = &types.ChatResponse{Content: "待验证：先陈述判断。", FinishReason: "stop"}
	proposal, err := s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: page.Version, Instruction: "改成短段落", ModelID: "model", WritingRuleIDs: []string{rule.ID}})
	require.NoError(t, err)
	require.Equal(t, rule.Version, model.contexts[1]["rule_versions"].(map[string]int)[rule.ID])
	_, err = s.Decide(ctx, "research-kb", page.ID, proposal.ID, true)
	require.NoError(t, err)
	_, err = s.SaveWritingFeedback(ctx, "research-kb", page.ID, proposal.ID, ResearchWritingFeedbackRequest{RuleID: rule.ID, Rating: "useful"})
	require.NoError(t, err)
	current, err := s.Get(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	_, err = s.Revert(ctx, "research-kb", page.ID, 1, current.Version)
	require.NoError(t, err)
	require.NoError(t, mgr.Shutdown(context.Background()))
	seen := map[string]bool{}
	for _, span := range exported() {
		metadata := tracedResearchMetadata(t, span)
		if span.Name == "research.learning.worker" || span.Name == "research.rules.extract_event" || span.Name == "research.rules.infer" {
			require.Equal(t, request.ID, hex.EncodeToString(span.TraceId))
			require.Equal(t, payload.EventID, metadata["event_id"])
			require.Equal(t, page.ID, metadata["draft_id"])
		}
		if span.Name == "research.draft.accept" || span.Name == "research.rules.feedback" {
			require.Equal(t, proposal.ID, metadata["proposal_id"])
		}
		seen[span.Name] = true
	}
	for _, name := range []string{"research.draft.save", "research.learning.enqueue", "research.learning.worker", "research.rules.extract_event", "research.rules.decide", "research.draft.propose", "research.draft.accept", "research.rules.feedback", "research.draft.restore", "chat.completion"} {
		require.True(t, seen[name], "missing exported operation %s", name)
	}
}

func TestResearchTracingExportFailureDoesNotRollbackSave(t *testing.T) {
	mgr, _, requests := researchTraceReceiver(t, true)
	ctx, s := researchWritingHarness(t)
	page, err := s.Create(ctx, "research-kb", ResearchCreateRequest{Title: "Trace failure", Content: "原稿"})
	require.NoError(t, err)
	page, err = s.Save(ctx, "research-kb", page.ID, ResearchSaveRequest{BaseVersion: page.Version, Title: page.Title, Content: "仍需保留"})
	require.NoError(t, err)
	_ = mgr.Shutdown(context.Background())
	require.Positive(t, requests.Load(), "export must actually have been attempted")
	stored, err := s.Get(ctx, "research-kb", page.ID)
	require.NoError(t, err)
	require.Equal(t, "仍需保留", stored.Content)
	require.Equal(t, 2, stored.Version)
}

type researchTracingRetrievalKB struct{ *researchRetrievalKBFixture }

func (kb researchTracingRetrievalKB) HybridSearch(ctx context.Context, id string, params types.SearchParams) ([]*types.SearchResult, error) {
	// Instrument the fixture boundary to prove the context passed to native
	// HybridSearch places query embedding beneath research.retrieval.
	_, generation := langfuse.GetManager().StartGeneration(ctx, langfuse.GenerationOptions{Name: "embedding.embed", Model: "test-embedding", Input: params.QueryText, Metadata: map[string]any{"model_id": "embedding"}})
	generation.Finish(map[string]any{"dimensions": 2}, &langfuse.TokenUsage{Input: 3, Total: 3}, nil)
	return kb.researchRetrievalKBFixture.HybridSearch(ctx, id, params)
}

func TestResearchTracingLinksSelectedRetrievalSnapshotToGeneration(t *testing.T) {
	mgr, exported, _ := researchTraceReceiver(t, false)
	ctx, s, kb, _, _ := researchRetrievalHarness(t)
	s.kb = researchTracingRetrievalKB{kb}
	model := &researchTracingChat{researchChatFixture: researchChatFixture{response: &types.ChatResponse{Content: "[证据](knowledge://source/chunks/text)", FinishReason: "stop"}}}
	s.models = researchModelFixture{model: model}
	page := createRetrievedDraft(t, ctx, s)
	snapshot, err := researchPageRetrieval(page)
	require.NoError(t, err)
	want := researchRetrievalTraceMetadata(snapshot)
	_, err = s.Propose(ctx, "research-kb", page.ID, ResearchProposeRequest{BaseVersion: 1, ModelID: "model", Instruction: "据检索片段起草"})
	require.NoError(t, err)
	require.Equal(t, want["retrieval_snapshot_sha256"], model.contexts[0]["retrieval_snapshot_sha256"])
	require.NoError(t, mgr.Shutdown(context.Background()))
	var retrieval, query, created *tracepb.Span
	for _, span := range exported() {
		switch span.Name {
		case "research.retrieval":
			retrieval = span
		case "embedding.embed":
			query = span
		case "research.draft.create":
			if tracedResearchMetadata(t, span)["draft_id"] != nil {
				created = span
			}
		}
	}
	require.NotNil(t, retrieval)
	require.NotNil(t, query)
	require.NotNil(t, created)
	require.Equal(t, retrieval.SpanId, query.ParentSpanId)
	require.Equal(t, created.TraceId, query.TraceId)
	meta := tracedResearchMetadata(t, retrieval)
	require.Equal(t, want["retrieval_snapshot_sha256"], meta["retrieval_snapshot_sha256"])
	require.Equal(t, float64(3), meta["retrieval_hit_count"])
	require.Equal(t, page.ID, tracedResearchMetadata(t, created)["draft_id"])
	encoded, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "CANONICAL_", "metadata must not duplicate source content")
	require.NotContains(t, string(encoded), "image_vector", "only selected readable evidence belongs to this snapshot")
}
