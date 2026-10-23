package embedding

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/limiter"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/panjf2000/ants/v2"
	"github.com/stretchr/testify/require"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Exercise NewEmbedder's actual protocol -> concurrency -> debug -> Langfuse
// stack, rather than a fake embedder that emits its own observation. The debug
// switch is read at package init, so isolated child processes cover both modes.
// All HTTP traffic stays on test servers; this is not live Langfuse acceptance.
func TestNewEmbedderPoolPreservesObservationsAndGate(t *testing.T) {
	const childEnv = "EMBEDDING_POOL_OBSERVABILITY_TEST_CHILD"
	if os.Getenv(childEnv) != "1" {
		for _, debug := range []bool{false, true} {
			name, debugDir := "without_debug", ""
			if debug {
				name, debugDir = "with_debug", t.TempDir()
			}
			t.Run(name, func(t *testing.T) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestNewEmbedderPoolPreservesObservationsAndGate$", "-test.timeout=30s")
				cmd.Env = append(os.Environ(), childEnv+"=1", "LLM_DEBUG_LOG="+debugDir)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
			})
		}
		return
	}

	allowLoopback(t)
	t.Setenv("BATCH_EMBED_SIZE", "1")
	limiter.SetGovernor(limiter.NewLocalLimiter(), 10)
	t.Cleanup(func() { limiter.SetGovernor(nil, 0) })
	var traceMu sync.Mutex
	var spans []*tracepb.Span
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		var request collectortrace.ExportTraceServiceRequest
		if err != nil || proto.Unmarshal(data, &request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		traceMu.Lock()
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				spans = append(spans, scope.Spans...)
			}
		}
		traceMu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(receiver.Close)
	mgr, err := langfuse.Init(langfuse.Config{Enabled: true, Host: receiver.URL,
		PublicKey: "test-public", SecretKey: "test-secret", SampleRate: 1,
		FlushAt: 32, FlushInterval: time.Second, QueueSize: 128, RequestTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()); _, _ = langfuse.Init(langfuse.Config{}) })

	entered := make(chan struct{}, 10)
	release := make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(release) }) }
	var inFlight, maxSeen, calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls.Add(1)
		current := inFlight.Add(1)
		for old := maxSeen.Load(); current > old && !maxSeen.CompareAndSwap(old, current); old = maxSeen.Load() {
		}
		defer inFlight.Add(-1)
		entered <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, answer(r.URL.Path, body))
	}))
	t.Cleanup(provider.Close)
	pool, err := ants.NewPool(8)
	require.NoError(t, err)
	t.Cleanup(pool.Release)
	// Registered last so failures release blocked handlers before server cleanup.
	t.Cleanup(unblock)
	embedder, err := NewEmbedder(Config{Source: types.ModelSourceRemote, Provider: "generic",
		BaseURL: provider.URL + "/v1", ModelName: "pool-test", ModelID: "pool-test-model",
		Dimensions: 1, MaxConcurrency: 2}, NewBatchEmbedder(pool), nil)
	require.NoError(t, err)
	outer, ok := embedder.(*langfuseEmbedder)
	require.True(t, ok)
	_, hasDebug := outer.inner.(*debugEmbedder)
	require.Equal(t, logger.LLMDebugEnabled(), hasDebug)

	ctx, trace := mgr.StartTrace(types.WithBackgroundTask(context.Background()), langfuse.TraceOptions{Name: "index-test"})
	ctx, worker := mgr.StartSpan(ctx, langfuse.SpanOptions{Name: "asynq.knowledge:index_rebuild"})
	type result struct {
		vectors [][]float32
		err     error
	}
	done := make(chan result, 1)
	go func() {
		vectors, err := embedder.BatchEmbedWithPool(ctx, embedder, []string{"a", "bb", "ccc", "dddd", "eeeee"})
		done <- result{vectors, err}
	}()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("two provider batches did not acquire the configured concurrency slots")
		}
	}
	select {
	case <-entered:
		t.Fatal("a third batch bypassed the model's concurrency limit of two")
	case <-time.After(150 * time.Millisecond):
	}
	unblock()
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Equal(t, [][]float32{{1}, {2}, {3}, {4}, {5}}, got.vectors)
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not finish after provider release")
	}
	// nil callers must still route through the full outer stack.
	vectors, err := embedder.BatchEmbedWithPool(ctx, nil, []string{"ffffff", "ggggggg"})
	require.NoError(t, err)
	require.Equal(t, [][]float32{{6}, {7}}, vectors)
	require.Equal(t, int32(7), calls.Load())
	require.Equal(t, int32(2), maxSeen.Load())
	worker.Finish(nil, nil, nil)
	trace.Finish(nil, nil)
	require.NoError(t, mgr.Shutdown(context.Background()))

	traceMu.Lock()
	defer traceMu.Unlock()
	generationCount := 0
	for _, span := range spans {
		if span.Name != "embedding.batch_embed" {
			continue
		}
		generationCount++
		require.Equal(t, trace.ID, hex.EncodeToString(span.TraceId))
		require.Equal(t, worker.ID, hex.EncodeToString(span.ParentSpanId))
		attrs := map[string]string{}
		for _, attr := range span.Attributes {
			attrs[attr.Key] = attr.Value.GetStringValue()
		}
		require.Equal(t, "pool-test", attrs["langfuse.observation.model.name"])
		var input map[string]any
		require.NoError(t, json.Unmarshal([]byte(attrs["langfuse.observation.input"]), &input))
		require.Equal(t, float64(1), input["count"])
	}
	require.Equal(t, 7, generationCount, "each actual provider batch needs exactly one observation")
	if logger.LLMDebugEnabled() {
		files, err := filepath.Glob(filepath.Join(os.Getenv("LLM_DEBUG_LOG"), "*.log"))
		require.NoError(t, err)
		records := 0
		for _, file := range files {
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			records += strings.Count(string(data), "Embedding")
		}
		require.Equal(t, 7, records, "debug wrapper must also observe every provider batch")
	}
}
