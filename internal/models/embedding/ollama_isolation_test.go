package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	ollamautil "github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestOllamaBackgroundEmbeddingIsolation(t *testing.T) {
	var foreground, background atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var fail atomic.Bool
	handler := func(counter *atomic.Int32, blocked bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/":
				_, _ = w.Write([]byte("OK"))
			case "/api/tags":
				_, _ = w.Write([]byte(`{"models":[{"name":"embeddinggemma:300m"}]}`))
			case "/api/embed":
				counter.Add(1)
				var body struct {
					Model string   `json:"model"`
					Input []string `json:"input"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "embeddinggemma:300m" || len(body.Input) != 1 {
					http.Error(w, "bad embedding request", 400)
					return
				}
				if blocked {
					if fail.Load() {
						http.Error(w, "background unavailable", 503)
						return
					}
					close(entered)
					<-release
				}
				_, _ = w.Write([]byte(`{"embeddings":[[1,2,3]]}`))
			default:
				http.Error(w, "unexpected endpoint", 400)
			}
		}
	}
	primary := httptest.NewServer(handler(&foreground, false))
	defer primary.Close()
	indexing := httptest.NewServer(handler(&background, true))
	defer indexing.Close()
	t.Setenv("OLLAMA_BASE_URL", primary.URL)
	t.Setenv("OLLAMA_EMBEDDING_BACKGROUND_BASE_URL", indexing.URL)
	t.Setenv("OLLAMA_OPTIONAL", "false")
	service, err := ollamautil.GetOllamaService()
	require.NoError(t, err)
	embedder, err := NewOllamaEmbedder(primary.URL, "embeddinggemma:300m", 2048, 3, "test-model", nil, service)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := embedder.Embed(types.WithBackgroundTask(context.Background()), "document")
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background did not reach isolated runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	vector, err := embedder.Embed(ctx, "question")
	close(release)
	require.NoError(t, err, "blocked indexing must not hold the query service mutex or queue")
	require.Equal(t, []float32{1, 2, 3}, vector)
	require.NoError(t, <-done)
	fail.Store(true)
	_, err = embedder.Embed(types.WithBackgroundTask(ctx), "another document")
	require.Error(t, err)
	require.Equal(t, int32(1), foreground.Load(), "failed background work must not fall back to the foreground queue")
	require.Equal(t, int32(2), background.Load())
}

func TestOllamaEmbeddingIsolationDefaultsAndValidation(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://localhost:11434")
	t.Setenv("OLLAMA_EMBEDDING_BACKGROUND_BASE_URL", "")
	service, err := ollamautil.GetOllamaService()
	require.NoError(t, err)
	require.Same(t, service, service.ForEmbedding(types.WithBackgroundTask(context.Background())))
	t.Setenv("OLLAMA_EMBEDDING_BACKGROUND_BASE_URL", "http://secret:credential@localhost:11435")
	_, err = ollamautil.GetOllamaService()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "credential@")
}
