// Package langfuse implements WeKnora's Langfuse/LiteFuse LLM tracing client on
// top of the OpenTelemetry Go SDK. It records chat/embedding/rerank/VLM/ASR
// generations plus the surrounding agent/HTTP/asynq spans as OTLP/HTTP spans
// to a Langfuse v3+ or LiteFuse backend (POST /api/public/otel/v1/traces).
//
// The integration is fully opt-in: when disabled (the default), every public
// entry point is a cheap no-op, so callers can wire them unconditionally. A
// W3C traceparent propagated from upstream callers (e.g. sop3) is inherited so
// WeKnora's trace shares the upstream trace id — correlating runs across
// services in the LiteFuse UI.
package langfuse

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config holds the runtime configuration for the Langfuse client.
//
// In practice users enable Langfuse purely through environment variables —
// Host / PublicKey / SecretKey — which matches every other Langfuse SDK and
// keeps WeKnora's YAML config free of secrets.
type Config struct {
	// Enabled is the master switch. If false the entire package is a no-op.
	Enabled bool
	// Host is the Langfuse base URL, e.g. https://cloud.langfuse.com or
	// https://us.cloud.langfuse.com or a self-hosted address.
	Host string
	// PublicKey / SecretKey are the project credentials used for Basic Auth.
	PublicKey string
	SecretKey string
	// FlushAt flushes the queued events once the buffer reaches this size.
	FlushAt int
	// FlushInterval is the maximum time between automatic flushes.
	FlushInterval time.Duration
	// QueueSize bounds the in-memory buffer to avoid unbounded growth if the
	// Langfuse endpoint is unreachable.
	QueueSize int
	// RequestTimeout bounds each HTTP attempt, not the complete retry window.
	RequestTimeout time.Duration
	// Retry policy belongs to the official OTLP exporter. The batch processor's
	// deadline is RetryMaxElapsedTime + RequestTimeout so it does not silently
	// truncate the exporter's retry policy with its unrelated 30s default.
	RetryInitialInterval time.Duration
	RetryMaxInterval     time.Duration
	RetryMaxElapsedTime  time.Duration
	// Release / Environment are attached to every trace for filtering in the
	// Langfuse UI (e.g. release="v0.4.2", environment="production").
	Release     string
	Environment string
	// SampleRate (0..1) controls trace sampling. 0 means "use 1.0".
	SampleRate float64
	// Debug enables verbose logging of batch send errors.
	Debug bool

	// testExporter, when non-nil, replaces the real OTLP exporter. Tests use
	// it to inject an in-memory span exporter (tracetest.InMemoryExporter) so
	// they can assert on exported spans deterministically without an HTTP
	// server. Init registers it with a SimpleSpanProcessor (synchronous
	// export on span End) when set. Unexported — production never sets it.
	testExporter sdktrace.SpanExporter
}

// LoadConfigFromEnv builds a Config by reading the LANGFUSE_* environment
// variables, mirroring the official Python / JS SDK conventions.
func LoadConfigFromEnv() Config {
	cfg := Config{
		Host:                 firstNonEmpty(os.Getenv("LANGFUSE_HOST"), "https://cloud.langfuse.com"),
		PublicKey:            strings.TrimSpace(os.Getenv("LANGFUSE_PUBLIC_KEY")),
		SecretKey:            strings.TrimSpace(os.Getenv("LANGFUSE_SECRET_KEY")),
		Release:              strings.TrimSpace(os.Getenv("LANGFUSE_RELEASE")),
		Environment:          strings.TrimSpace(os.Getenv("LANGFUSE_ENVIRONMENT")),
		FlushAt:              64,
		FlushInterval:        3 * time.Second,
		QueueSize:            2048,
		RequestTimeout:       10 * time.Second,
		SampleRate:           1.0,
		RetryInitialInterval: 5 * time.Second,
		RetryMaxInterval:     30 * time.Second,
		RetryMaxElapsedTime:  time.Minute,
	}

	if v := strings.TrimSpace(os.Getenv("LANGFUSE_ENABLED")); v != "" {
		cfg.Enabled = parseBool(v)
	} else if cfg.PublicKey != "" && cfg.SecretKey != "" {
		// Auto-enable when credentials are present — matches the Python SDK.
		cfg.Enabled = true
	}

	if v := strings.TrimSpace(os.Getenv("LANGFUSE_FLUSH_AT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.FlushAt = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("LANGFUSE_FLUSH_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.FlushInterval = d
		} else if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.FlushInterval = time.Duration(n) * time.Second
		}
	}
	if v := strings.TrimSpace(os.Getenv("LANGFUSE_QUEUE_SIZE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.QueueSize = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("LANGFUSE_REQUEST_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.RequestTimeout = d
		}
	}
	for name, target := range map[string]*time.Duration{
		"LANGFUSE_RETRY_INITIAL_INTERVAL": &cfg.RetryInitialInterval,
		"LANGFUSE_RETRY_MAX_INTERVAL":     &cfg.RetryMaxInterval,
		"LANGFUSE_RETRY_MAX_ELAPSED_TIME": &cfg.RetryMaxElapsedTime,
	} {
		if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv(name))); err == nil && d > 0 {
			*target = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("LANGFUSE_SAMPLE_RATE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			cfg.SampleRate = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("LANGFUSE_DEBUG")); v != "" {
		cfg.Debug = parseBool(v)
	}

	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}

	return cfg
}

// Validate verifies required fields are present when Langfuse is enabled.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("langfuse: host is required when enabled")
	}
	if c.PublicKey == "" || c.SecretKey == "" {
		return fmt.Errorf("langfuse: public_key and secret_key are required when enabled")
	}
	endpoint, err := url.Parse(c.Host)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("langfuse: host must be an http(s) URL without credentials, query or fragment")
	}
	if c.FlushAt < 0 || c.QueueSize < 0 || c.FlushInterval < 0 || c.RequestTimeout < 0 ||
		c.RetryInitialInterval < 0 || c.RetryMaxInterval < 0 || c.RetryMaxElapsedTime < 0 {
		return fmt.Errorf("langfuse: queue, batch and timing limits cannot be negative")
	}
	if c.RetryInitialInterval > 0 && c.RetryMaxInterval > 0 && c.RetryInitialInterval > c.RetryMaxInterval {
		return fmt.Errorf("langfuse: initial retry interval exceeds maximum retry interval")
	}
	if c.RetryMaxElapsedTime > time.Duration(1<<63-1)-c.RequestTimeout {
		return fmt.Errorf("langfuse: export time budget overflows")
	}
	return nil
}

func (c Config) withDefaults() Config {
	if c.FlushAt == 0 {
		c.FlushAt = 64
	}
	if c.QueueSize == 0 {
		c.QueueSize = 2048
	}
	if c.FlushInterval == 0 {
		c.FlushInterval = 3 * time.Second
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 10 * time.Second
	}
	if c.RetryInitialInterval == 0 {
		c.RetryInitialInterval = 5 * time.Second
	}
	if c.RetryMaxInterval == 0 {
		c.RetryMaxInterval = 30 * time.Second
	}
	if c.RetryMaxElapsedTime == 0 {
		c.RetryMaxElapsedTime = time.Minute
	}
	if c.FlushAt > c.QueueSize {
		c.FlushAt = c.QueueSize
	}
	return c
}

func (c Config) exportTimeout() time.Duration {
	return c.RetryMaxElapsedTime + c.RequestTimeout
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	}
	return false
}
