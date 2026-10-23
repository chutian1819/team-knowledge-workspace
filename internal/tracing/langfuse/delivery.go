package langfuse

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// DeliveryStats describes this process's bounded in-memory transport. OTLP
// acceptance is not proof of Langfuse persistence. Failed batches may have been
// partly received, so they are unconfirmed rather than definitely lost.
// Counters reset at restart; no prompts, credentials or endpoints are retained.
type DeliveryStats struct {
	QueueCapacity         int    `json:"queue_capacity"`
	BatchSize             int    `json:"batch_size"`
	PendingSpans          int    `json:"pending_spans"`
	InFlightSpans         int64  `json:"in_flight_spans"`
	AdmittedSpans         uint64 `json:"admitted_spans"`
	QueueDroppedSpans     uint64 `json:"queue_dropped_spans"`
	ShutdownDroppedSpans  uint64 `json:"shutdown_dropped_spans"`
	OTLPAcceptedSpans     uint64 `json:"otlp_accepted_spans"`
	OTLPAcceptedBatches   uint64 `json:"otlp_accepted_batches"`
	UnconfirmedSpans      uint64 `json:"unconfirmed_spans"`
	FailedBatches         uint64 `json:"failed_batches"`
	LastExportDurationMS  int64  `json:"last_export_duration_ms"`
	ExportDurationMSTotal uint64 `json:"export_duration_ms_total"`
	LastFailureClass      string `json:"last_failure_class,omitempty"`
}

type deliveryState struct {
	slots                                                      chan struct{}
	batchSize                                                  int
	inFlight                                                   atomic.Int64
	admitted, queueDropped, shutdownDropped                    atomic.Uint64
	acceptedSpans, acceptedBatches, unconfirmed, failedBatches atomic.Uint64
	lastDuration                                               atomic.Int64
	totalDuration                                              atomic.Uint64
	failure                                                    atomic.Value // fixed class string only
}

func newDeliveryState(cfg Config) *deliveryState {
	d := &deliveryState{slots: make(chan struct{}, cfg.QueueSize), batchSize: cfg.FlushAt}
	d.failure.Store("")
	return d
}

func (d *deliveryState) snapshot() DeliveryStats {
	if d == nil {
		return DeliveryStats{}
	}
	return DeliveryStats{
		QueueCapacity: cap(d.slots), BatchSize: d.batchSize, PendingSpans: len(d.slots), InFlightSpans: d.inFlight.Load(),
		AdmittedSpans: d.admitted.Load(), QueueDroppedSpans: d.queueDropped.Load(), ShutdownDroppedSpans: d.shutdownDropped.Load(),
		OTLPAcceptedSpans: d.acceptedSpans.Load(), OTLPAcceptedBatches: d.acceptedBatches.Load(),
		UnconfirmedSpans: d.unconfirmed.Load(), FailedBatches: d.failedBatches.Load(),
		LastExportDurationMS: d.lastDuration.Load(), ExportDurationMSTotal: d.totalDuration.Load(), LastFailureClass: d.failure.Load().(string),
	}
}

// Admission is non-blocking and bounds queued PLUS in-flight spans. The SDK
// still handles batching and OTLP retries. One extra SDK queue slot is reserved
// for a serialized ForceFlush marker, so no admitted span can silently overflow
// its channel. No WithBlocking is used on the application path.
type deliveryProcessor struct {
	inner   sdktrace.SpanProcessor
	state   *deliveryState
	mu      sync.Mutex
	stopped bool
	flushMu sync.Mutex
	flush   *deliveryFlush
}

type deliveryFlush struct {
	done chan struct{}
	err  error
}

func (p *deliveryProcessor) OnStart(ctx context.Context, span sdktrace.ReadWriteSpan) {
	p.inner.OnStart(ctx, span)
}

func (p *deliveryProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
	if !span.SpanContext().IsSampled() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		p.state.shutdownDropped.Add(1)
		return
	}
	select {
	case p.state.slots <- struct{}{}:
		p.state.admitted.Add(1)
		p.inner.OnEnd(span)
	default:
		p.state.queueDropped.Add(1)
	}
}

func (p *deliveryProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	return p.inner.Shutdown(ctx)
}

func (p *deliveryProcessor) ForceFlush(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.flushMu.Lock()
		run := p.flush
		started := run == nil
		if started {
			run = &deliveryFlush{done: make(chan struct{})}
			p.flush = run
			go func() {
				// A canceled SDK ForceFlush leaves its marker in the queue. Keep
				// this one operation alive until the marker is consumed, while
				// letting each caller stop waiting on its own deadline. Exports
				// retain their configured per-batch deadline; at most one flush
				// goroutine and one reserved marker exist at any time.
				run.err = p.inner.ForceFlush(context.Background())
				p.flushMu.Lock()
				p.flush = nil
				close(run.done)
				p.flushMu.Unlock()
			}()
		}
		p.flushMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-run.done:
			if started {
				return run.err
			}
			// The preceding operation may have inserted its marker before
			// this caller's spans ended. Start a new barrier after it finishes.
		}
	}
}

type deliveryExporter struct {
	inner sdktrace.SpanExporter
	state *deliveryState
}

func (e *deliveryExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	started := time.Now()
	e.state.inFlight.Store(int64(len(spans)))
	err := e.inner.ExportSpans(ctx, spans)
	duration := time.Since(started).Milliseconds()
	e.state.lastDuration.Store(duration)
	e.state.totalDuration.Add(uint64(duration))
	if err == nil {
		e.state.acceptedSpans.Add(uint64(len(spans)))
		e.state.acceptedBatches.Add(1)
	} else {
		e.state.unconfirmed.Add(uint64(len(spans)))
		e.state.failedBatches.Add(1)
	}
	for range spans {
		<-e.state.slots
	}
	e.state.inFlight.Store(0)
	if err != nil {
		// SDK errors may include response bodies or endpoint URLs. Its retries
		// have finished: pass only a fixed class to the BSP/global OTel handler.
		class := "export_failed"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			class = "deadline_exceeded"
		} else if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			class = "canceled"
		}
		e.state.failure.Store(class)
		return errors.New("langfuse: OTLP batch unconfirmed (" + class + "); inspect delivery counters")
	}
	return nil
}

func (e *deliveryExporter) Shutdown(ctx context.Context) error { return e.inner.Shutdown(ctx) }

// Stats is safe for local diagnostics without reading configuration or secrets.
func (m *Manager) Stats() DeliveryStats {
	if m == nil {
		return DeliveryStats{}
	}
	return m.delivery.snapshot()
}

func (m *Manager) reportDelivery(previous DeliveryStats) DeliveryStats {
	current := m.Stats()
	if current == previous {
		return current
	}
	encoded, _ := json.Marshal(current)
	if current.QueueDroppedSpans > previous.QueueDroppedSpans || current.UnconfirmedSpans > previous.UnconfirmedSpans || current.ShutdownDroppedSpans > previous.ShutdownDroppedSpans {
		logger.Warnf(context.Background(), "[Langfuse] delivery %s", encoded)
	} else {
		logger.Infof(context.Background(), "[Langfuse] delivery %s", encoded)
	}
	return current
}

func (m *Manager) monitorDelivery() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	previous := DeliveryStats{}
	for {
		select {
		case <-ticker.C:
			previous = m.reportDelivery(previous)
		case <-m.deliveryStop:
			return
		}
	}
}
