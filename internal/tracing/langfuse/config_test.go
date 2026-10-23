package langfuse

import (
	"testing"
	"time"
)

func TestLoadConfigFromEnv_AutoEnablesWithCredentials(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")
	t.Setenv("LANGFUSE_HOST", "https://example.langfuse.com")

	cfg := LoadConfigFromEnv()

	if !cfg.Enabled {
		t.Fatalf("expected Enabled=true when both keys are set, got false")
	}
	if cfg.Host != "https://example.langfuse.com" {
		t.Errorf("unexpected host: %q", cfg.Host)
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("expected default SampleRate=1.0, got %v", cfg.SampleRate)
	}
}

func TestLoadConfigFromEnv_DisabledWithoutKeys(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")
	t.Setenv("LANGFUSE_ENABLED", "")

	cfg := LoadConfigFromEnv()
	if cfg.Enabled {
		t.Fatalf("expected Enabled=false when no keys set")
	}
}

func TestLoadConfigFromEnv_ExplicitDisableOverridesKeys(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk")
	t.Setenv("LANGFUSE_ENABLED", "false")

	cfg := LoadConfigFromEnv()
	if cfg.Enabled {
		t.Fatalf("expected Enabled=false when LANGFUSE_ENABLED=false")
	}
}

func TestLoadConfigFromEnv_FlushIntervalAcceptsSecondsAndDuration(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk")

	t.Setenv("LANGFUSE_FLUSH_INTERVAL", "500ms")
	cfg := LoadConfigFromEnv()
	if cfg.FlushInterval != 500*time.Millisecond {
		t.Errorf("expected 500ms, got %v", cfg.FlushInterval)
	}

	t.Setenv("LANGFUSE_FLUSH_INTERVAL", "7")
	cfg = LoadConfigFromEnv()
	if cfg.FlushInterval != 7*time.Second {
		t.Errorf("expected 7s (bare integer), got %v", cfg.FlushInterval)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"disabled is always valid", Config{Enabled: false}, false},
		{"enabled without host fails", Config{Enabled: true, PublicKey: "pk", SecretKey: "sk"}, true},
		{"enabled without keys fails", Config{Enabled: true, Host: "https://x"}, true},
		{"enabled with all fields passes", Config{Enabled: true, Host: "https://x", PublicKey: "pk", SecretKey: "sk"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestDeliveryConfigDefaultsAndExplicitRetryBudget(t *testing.T) {
	for _, name := range []string{"LANGFUSE_FLUSH_AT", "LANGFUSE_QUEUE_SIZE", "LANGFUSE_REQUEST_TIMEOUT", "LANGFUSE_RETRY_INITIAL_INTERVAL", "LANGFUSE_RETRY_MAX_INTERVAL", "LANGFUSE_RETRY_MAX_ELAPSED_TIME"} {
		t.Setenv(name, "")
	}
	cfg := LoadConfigFromEnv()
	if cfg.FlushAt != 64 || cfg.QueueSize != 2048 || cfg.exportTimeout() != 70*time.Second {
		t.Fatalf("unexpected bounded delivery defaults: batch=%d queue=%d budget=%s", cfg.FlushAt, cfg.QueueSize, cfg.exportTimeout())
	}
	t.Setenv("LANGFUSE_RETRY_INITIAL_INTERVAL", "200ms")
	t.Setenv("LANGFUSE_RETRY_MAX_INTERVAL", "1s")
	t.Setenv("LANGFUSE_RETRY_MAX_ELAPSED_TIME", "4s")
	t.Setenv("LANGFUSE_REQUEST_TIMEOUT", "2s")
	cfg = LoadConfigFromEnv()
	if cfg.RetryInitialInterval != 200*time.Millisecond || cfg.RetryMaxInterval != time.Second || cfg.exportTimeout() != 6*time.Second {
		t.Fatal("explicit OTLP retry timing was not preserved")
	}
	limited := (Config{QueueSize: 2}).withDefaults()
	if limited.FlushAt != 2 {
		t.Fatal("a batch must fit the total pending bound")
	}
	for _, bad := range []Config{
		{Enabled: true, Host: "https://user:secret@example.com", PublicKey: "p", SecretKey: "s"},
		{Enabled: true, Host: "https://example.com?secret=value", PublicKey: "p", SecretKey: "s"},
		{Enabled: true, Host: "https://example.com", PublicKey: "p", SecretKey: "s", QueueSize: -1},
		{Enabled: true, Host: "https://example.com", PublicKey: "p", SecretKey: "s", RetryInitialInterval: time.Minute, RetryMaxInterval: time.Second},
	} {
		if bad.Validate() == nil {
			t.Fatal("invalid delivery configuration accepted")
		}
	}
}
