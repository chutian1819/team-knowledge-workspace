package langfuse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func observationAttributes(attrs []attribute.KeyValue) map[string]any {
	result := map[string]any{}
	for _, attr := range attrs {
		result[string(attr.Key)] = attr.Value.AsInterface()
	}
	return result
}

func TestObservationV4MetadataIdentityAndSafeErrors(t *testing.T) {
	mgr, exporter := newTestManager(t)
	ctx := WithSafeObservationErrors(WithObservationMetadata(context.Background(), map[string]any{"draft_id": "draft", "rule_versions": map[string]int{"rule": 2}}))
	ctx, root := mgr.StartTrace(ctx, TraceOptions{Name: "research-save", UserID: "user", SessionID: "research:kb:draft", Input: map[string]any{"base_version": 3}})
	child, operation := mgr.StartSpan(ctx, SpanOptions{Name: "research.rules.extract", Metadata: map[string]any{"event_id": "event"}})
	child = WithObservationMetadata(child, map[string]any{"event_id": "event"})
	_, generation := mgr.StartGeneration(child, GenerationOptions{Name: "chat.completion", Model: "model"})
	providerErr := errors.New("Authorization Bearer SECRET_SENTINEL https://provider/?api_key=SECRET_SENTINEL")
	generation.Finish(nil, nil, providerErr)
	operation.Finish(nil, map[string]any{"rule_count": 1}, providerErr)
	root.Finish(map[string]any{"result_version": 4}, nil)
	require.NotContains(t, ObservationMetadataFromContext(ctx), "event_id", "child metadata must not mutate sibling context")
	spans := exporter.GetSpans()
	require.Len(t, spans, 3)
	for _, span := range spans {
		attrs := observationAttributes(span.Attributes)
		require.Equal(t, "user", attrs[attrUserID])
		require.Equal(t, "research:kb:draft", attrs[attrSessionID])
		require.Equal(t, "draft", attrs[attrObsMetadata+".draft_id"])
		require.Equal(t, `{"rule":2}`, attrs[attrObsMetadata+".rule_versions"])
		encoded, err := json.Marshal(span)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "SECRET_SENTINEL")
		if span.Name == "research-save" {
			require.Equal(t, "span", attrs[attrObsType])
			require.Equal(t, `{"base_version":3}`, attrs[attrObsInput])
			require.Equal(t, `{"result_version":4}`, attrs[attrObsOutput])
		}
	}
}

func TestResearchSaveTaskPropagatesSameTraceAndSession(t *testing.T) {
	mgr, exporter := newTestManager(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(GinMiddleware())
	var queued []byte
	var originalTrace string
	router.PUT("/api/v1/knowledgebase/:kb_id/research/drafts/:draft_id", func(c *gin.Context) {
		ctx, save := mgr.StartSpan(c.Request.Context(), SpanOptions{Name: "research.draft.save"})
		payload := &dummyPayload{KnowledgeID: "event"}
		InjectTracing(ctx, payload)
		originalTrace = payload.LangfuseTraceID
		require.Equal(t, "research:kb:draft", payload.LangfuseSessionID)
		queued, _ = json.Marshal(payload)
		save.Finish(nil, nil, nil)
		c.Status(200)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PUT", "/api/v1/knowledgebase/kb/research/drafts/draft", nil))
	require.NotEmpty(t, originalTrace)
	middleware := AsynqMiddleware()(asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		_, generation := mgr.StartGeneration(ctx, GenerationOptions{Name: "chat.completion", Model: "model"})
		generation.Finish("rules", nil, nil)
		return nil
	}))
	require.NoError(t, middleware.ProcessTask(context.Background(), asynq.NewTask("research:writing-learning", queued)))
	spans := exporter.GetSpans()
	require.Len(t, spans, 4)
	for _, span := range spans {
		require.Equal(t, originalTrace, span.SpanContext.TraceID().String())
		require.Equal(t, "research:kb:draft", observationAttributes(span.Attributes)[attrSessionID])
	}
}
