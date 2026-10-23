package langfuse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func deliveryTestConfig(host string) Config {
	return Config{Enabled: true, Host: host, PublicKey: "test-public", SecretKey: "test-secret",
		FlushAt: 1, FlushInterval: time.Second, QueueSize: 8, SampleRate: 1,
		RequestTimeout: 100 * time.Millisecond, RetryInitialInterval: 5 * time.Millisecond,
		RetryMaxInterval: 10 * time.Millisecond, RetryMaxElapsedTime: 150 * time.Millisecond}
}

func deliveryWireIDs(t *testing.T, request *http.Request) []string {
	t.Helper()
	data, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	var payload collectortrace.ExportTraceServiceRequest
	require.NoError(t, proto.Unmarshal(data, &payload))
	var ids []string
	for _, resource := range payload.ResourceSpans {
		for _, scope := range resource.ScopeSpans {
			for _, span := range scope.Spans {
				ids = append(ids, string(span.SpanId))
			}
		}
	}
	return ids
}

func TestDeliveryOfficialRetryRecoversWithoutCountingSpansTwice(t *testing.T) {
	for _, mode := range []string{"slow", "503", "attempt_timeout"} {
		t.Run(mode, func(t *testing.T) {
			// An inherited BSP env deadline must not truncate the explicit OTLP
			// budget. All cases take longer than this ten-millisecond deadline.
			t.Setenv("OTEL_BSP_EXPORT_TIMEOUT", "10")
			var requests atomic.Int32
			var mu sync.Mutex
			var observed [][]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := requests.Add(1)
				ids := deliveryWireIDs(t, r)
				mu.Lock()
				observed = append(observed, ids)
				mu.Unlock()
				switch {
				case mode == "slow":
					time.Sleep(40 * time.Millisecond)
				case mode == "503" && attempt < 3:
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, "sensitive-response-marker")
					return
				case mode == "attempt_timeout" && attempt == 1:
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			defer server.Close()
			cfg := deliveryTestConfig(server.URL)
			if mode == "attempt_timeout" {
				cfg.RequestTimeout = 25 * time.Millisecond
			}
			mgr, err := Init(cfg)
			require.NoError(t, err)
			defer func() { _ = mgr.Shutdown(context.Background()); _, _ = Init(Config{}) }()
			_, span := mgr.StartTrace(context.Background(), TraceOptions{Name: "delivery-test"})
			span.Finish("small test output", nil)
			require.NoError(t, mgr.Shutdown(context.Background()))
			stats := mgr.Stats()
			require.Equal(t, uint64(1), stats.AdmittedSpans)
			require.Equal(t, uint64(1), stats.OTLPAcceptedSpans)
			require.Equal(t, uint64(1), stats.OTLPAcceptedBatches)
			require.Zero(t, stats.QueueDroppedSpans)
			require.Zero(t, stats.UnconfirmedSpans)
			require.Zero(t, stats.PendingSpans)
			wanted := int32(1)
			if mode == "503" {
				wanted = 3
			} else if mode == "attempt_timeout" {
				wanted = 2
			}
			require.Equal(t, wanted, requests.Load())
			mu.Lock()
			defer mu.Unlock()
			for _, ids := range observed {
				require.Equal(t, observed[0], ids, "OTLP retries retain span identity")
			}
		})
	}
}

func TestDeliveryTerminalFailureIsCountedAndRedacted(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(status)
				if status == http.StatusOK {
					data, _ := proto.Marshal(&collectortrace.ExportTraceServiceResponse{PartialSuccess: &collectortrace.ExportTracePartialSuccess{
						RejectedSpans: 1, ErrorMessage: "sensitive-response-marker"}})
					_, _ = w.Write(data)
				} else {
					_, _ = io.WriteString(w, "sensitive-response-marker test-secret")
				}
			}))
			defer server.Close()
			cfg := deliveryTestConfig(server.URL)
			cfg.RetryMaxElapsedTime = 25 * time.Millisecond
			inner, err := newExporter(context.Background(), cfg)
			require.NoError(t, err)
			state := newDeliveryState(cfg)
			state.slots <- struct{}{}
			state.admitted.Add(1)
			exporter := &deliveryExporter{inner: inner, state: state}
			defer func() { _ = exporter.Shutdown(context.Background()) }()
			ctx, cancel := context.WithTimeout(context.Background(), cfg.exportTimeout())
			defer cancel()
			err = exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{tracetest.SpanStub{Name: "test"}.Snapshot()})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive-response-marker")
			require.NotContains(t, err.Error(), "test-secret")
			require.NotContains(t, err.Error(), server.URL)
			stats := state.snapshot()
			require.Equal(t, uint64(1), stats.UnconfirmedSpans)
			require.Equal(t, uint64(1), stats.FailedBatches)
			require.Zero(t, stats.OTLPAcceptedSpans)
			require.Zero(t, stats.QueueDroppedSpans)
			require.Zero(t, stats.PendingSpans)
			if status == http.StatusServiceUnavailable {
				require.Greater(t, requests.Load(), int32(1))
			} else {
				require.Equal(t, int32(1), requests.Load(), "permanent 4xx/partial success must not be retried")
			}
		})
	}
}

func TestDeliveryQueuePressureNeverBlocksBusinessAndCountsOnlyDroppedSpans(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	cfg := deliveryTestConfig(server.URL)
	cfg.QueueSize = 2
	cfg.RequestTimeout = 2 * time.Second
	mgr, err := Init(cfg)
	require.NoError(t, err)
	defer func() { unblock(); _ = mgr.Shutdown(context.Background()); _, _ = Init(Config{}) }()
	emit := func() {
		_, span := mgr.StartTrace(context.Background(), TraceOptions{Name: "pressure"})
		span.Finish(nil, nil)
	}
	emit()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("export did not start")
	}
	started := time.Now()
	for range 100 {
		emit()
	}
	require.Less(t, time.Since(started), 500*time.Millisecond)
	stats := mgr.Stats()
	require.Equal(t, 2, stats.PendingSpans)
	require.Equal(t, int64(1), stats.InFlightSpans)
	require.Equal(t, uint64(2), stats.AdmittedSpans)
	require.Equal(t, uint64(99), stats.QueueDroppedSpans)
	unblock()
	require.NoError(t, mgr.Shutdown(context.Background()))
	stats = mgr.Stats()
	require.Equal(t, uint64(2), stats.OTLPAcceptedSpans)
	require.Zero(t, stats.PendingSpans)
	require.Zero(t, stats.UnconfirmedSpans)
	require.Equal(t, uint64(99), stats.QueueDroppedSpans)
}

func TestDeliveryConcurrentFlushDoesNotLoseAdmittedSpans(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Millisecond)
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	cfg := deliveryTestConfig(server.URL)
	cfg.QueueSize = 16
	cfg.FlushAt = 4
	mgr, err := Init(cfg)
	require.NoError(t, err)
	defer func() { _ = mgr.Shutdown(context.Background()); _, _ = Init(Config{}) }()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, span := mgr.StartTrace(context.Background(), TraceOptions{Name: "flush"})
				span.Finish(nil, nil)
				require.NoError(t, mgr.tp.ForceFlush(context.Background()))
			}
		}()
	}
	wg.Wait()
	require.NoError(t, mgr.Shutdown(context.Background()))
	stats := mgr.Stats()
	require.Equal(t, uint64(80), stats.AdmittedSpans+stats.QueueDroppedSpans)
	require.Equal(t, stats.AdmittedSpans, stats.OTLPAcceptedSpans)
	require.Zero(t, stats.UnconfirmedSpans)
	require.Zero(t, stats.PendingSpans)
}

func TestDeliveryCanceledFlushKeepsOnlyOneSDKMarker(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	cfg := deliveryTestConfig(server.URL)
	cfg.QueueSize = 8
	cfg.RequestTimeout = 2 * time.Second
	mgr, err := Init(cfg)
	require.NoError(t, err)
	defer func() { unblock(); _ = mgr.Shutdown(context.Background()); _, _ = Init(Config{}) }()
	emit := func() {
		_, span := mgr.StartTrace(context.Background(), TraceOptions{Name: "canceled-flush"})
		span.Finish(nil, nil)
	}
	emit()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("export did not start")
	}
	for range 20 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Millisecond)
		started := time.Now()
		err := mgr.tp.ForceFlush(ctx)
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Less(t, time.Since(started), 200*time.Millisecond, "a previous flush must not delay cancellation")
	}
	for range 7 {
		emit()
	}
	require.Equal(t, uint64(8), mgr.Stats().AdmittedSpans)
	require.Zero(t, mgr.Stats().QueueDroppedSpans)
	unblock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, mgr.tp.ForceFlush(ctx))
	require.NoError(t, mgr.Shutdown(ctx))
	stats := mgr.Stats()
	require.Equal(t, stats.AdmittedSpans, stats.OTLPAcceptedSpans+stats.UnconfirmedSpans+uint64(stats.PendingSpans))
	require.Equal(t, uint64(8), stats.OTLPAcceptedSpans)
	require.Zero(t, stats.PendingSpans)
	require.Zero(t, stats.UnconfirmedSpans)
}
