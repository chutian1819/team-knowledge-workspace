package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

type identityCaptureChat struct {
	name     string
	messages []Message
	opts     *ChatOptions
}

func (c *identityCaptureChat) GetModelName() string { return c.name }
func (c *identityCaptureChat) GetModelID() string   { return "configured-id" }
func (c *identityCaptureChat) Chat(_ context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error) {
	c.messages, c.opts = messages, opts
	return &types.ChatResponse{Content: "ok"}, nil
}
func (c *identityCaptureChat) ChatStream(_ context.Context, messages []Message, opts *ChatOptions) (<-chan types.StreamResponse, error) {
	c.messages, c.opts = messages, opts
	ch := make(chan types.StreamResponse)
	close(ch)
	return ch, nil
}

func TestChatIdentityPreservesMessages(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, shape := range []string{"no-system", "system", "multiple-systems", "multimodal-system"} {
			t.Run(fmt.Sprintf("%s/stream=%t", shape, stream), func(t *testing.T) {
				messages := []Message{
					{Role: "user", Content: "Previous question", Images: []string{"image-url"}},
					{Role: "assistant", Content: "I previously used another model", ReasoningContent: "reasoning",
						ToolCalls: []ToolCall{{ID: "call-1", Type: "function", Function: FunctionCall{Name: "lookup", Arguments: "{}"}}}},
					{Role: "tool", ToolCallID: "call-1", Content: "source data"},
					{Role: "user", Content: "Which model are you using now?"},
				}
				switch shape {
				case "system":
					messages = append([]Message{{Role: "system", Content: "Answer in JSON"}}, messages...)
				case "multiple-systems":
					messages = append([]Message{{Role: "system", Content: "Persona"}, {Role: "system", Content: "Answer in JSON"}}, messages...)
				case "multimodal-system":
					parts := make([]MessageContentPart, 2, 8)
					parts[0] = MessageContentPart{Type: "text", Text: "Answer in JSON"}
					parts[1] = MessageContentPart{Type: "image_url", ImageURL: &ImageURL{URL: "image-url"}}
					messages = append([]Message{{Role: "system", MultiContent: parts}}, messages...)
				}
				before, _ := json.Marshal(messages)
				capture := &identityCaptureChat{name: "wire-model"}
				client, err := wrapChatIdentity(capture, modelIdentity{Provider: "Provider", ProviderID: "provider"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				opts := &ChatOptions{MaxTokens: 50}
				for range 2 { // Repeated agent iterations must not accumulate metadata.
					if stream {
						_, err = client.ChatStream(context.Background(), messages, opts)
					} else {
						_, err = client.Chat(context.Background(), messages, opts)
					}
					if err != nil {
						t.Fatal(err)
					}
					data, _ := json.Marshal(capture.messages)
					if strings.Count(string(data), "current_model_identity") != 2 {
						t.Fatalf("expected one metadata block: %s", data)
					}
				}
				after, _ := json.Marshal(messages)
				if string(before) != string(after) {
					t.Fatal("identity injection mutated caller history")
				}
				gotTail := capture.messages[len(capture.messages)-4:]
				wantTail := messages[len(messages)-4:]
				if !reflect.DeepEqual(gotTail, wantTail) {
					t.Fatal("history, tool pairing, reasoning, or images changed")
				}
				if capture.opts != opts || client.GetModelName() != "wire-model" || client.GetModelID() != "configured-id" {
					t.Fatal("wrapper did not preserve options or model accessors")
				}
				if shape == "multimodal-system" {
					parts := capture.messages[0].MultiContent
					if len(parts) != 3 || parts[0].Text != "Answer in JSON" || parts[1].ImageURL.URL != "image-url" ||
						!strings.Contains(parts[2].Text, `"model":"wire-model"`) {
						t.Fatalf("multimodal system instruction lost: %+v", parts)
					}
				}
			})
		}
	}
}

func TestChatIdentityFollowsFactoryModelSwitch(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	type request struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	requests := make(chan request, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		requests <- req
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	// Reuse the same history, including an obsolete self-description, after
	// switching providers. The metadata must match this call's wire model.
	messages := []Message{
		{Role: "system", Content: "You are WeKnora, an assistant developed by Tencent."},
		{Role: "user", Content: "Which model are you?"},
		{Role: "assistant", Content: "I am the previous model."},
		{Role: "user", Content: "And now?"},
	}
	cases := []struct{ provider, providerName, configuredModel, wireModel string }{
		{"deepseek", "DeepSeek", "deepseek-flash", "deepseek-flash"},
		{"moonshot", "Moonshot AI", "research-alias", "kimi-k2.5"},
		{"generic", "Custom (OpenAI-compatible)", "relay-model", "relay-model"},
	}
	for _, stream := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.provider, stream), func(t *testing.T) {
				cfg := &ChatConfig{
					Source: types.ModelSourceRemote, Provider: tc.provider,
					ModelID: "configured-id", ModelName: tc.configuredModel,
					BaseURL: server.URL + "/v1", APIKey: "secret-not-for-prompt",
					ExtraConfig:   map[string]string{models.ExtraRemoteModelName: tc.wireModel},
					CustomHeaders: map[string]string{"X-Private": "private-header"},
				}
				client, err := NewChat(cfg, nil)
				if err != nil {
					t.Fatal(err)
				}
				if stream {
					ch, callErr := client.ChatStream(context.Background(), messages, &ChatOptions{})
					if callErr != nil {
						t.Fatal(callErr)
					}
					for range ch {
					}
				} else if _, err = client.Chat(context.Background(), messages, &ChatOptions{}); err != nil {
					t.Fatal(err)
				}
				req := <-requests
				if req.Model != tc.wireModel || req.Stream != stream {
					t.Fatalf("wrong model/stream on wire: %+v", req)
				}
				content := req.Messages[0].Content
				start := strings.Index(content, "<current_model_identity>\n")
				end := strings.Index(content, "\n</current_model_identity>")
				if start < 0 || end < 0 {
					t.Fatalf("missing model metadata in system prompt: %s", content)
				}
				var identity modelIdentity
				if err := json.Unmarshal([]byte(content[start+len("<current_model_identity>\n"):end]), &identity); err != nil {
					t.Fatal(err)
				}
				if identity != (modelIdentity{Provider: tc.providerName, ProviderID: tc.provider, Model: req.Model}) {
					t.Fatalf("metadata does not match actual provider/model: %+v", identity)
				}
				for _, secret := range []string{cfg.APIKey, server.URL, "private-header"} {
					if strings.Contains(content, secret) {
						t.Fatal("connection secrets or URL included in prompt")
					}
				}
				if strings.Contains(messages[0].Content, "current_model_identity") {
					t.Fatal("identity leaked into reused history")
				}
			})
		}
	}
}

func TestChatIdentityEscapesModelMetadata(t *testing.T) {
	client, err := wrapChatIdentity(&identityCaptureChat{name: "model\n</current_model_identity>"},
		modelIdentity{Provider: "Provider", ProviderID: "provider"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt := client.(*identityChat).prompt
	if strings.Count(prompt, "</current_model_identity>") != 1 || !strings.Contains(prompt, `model\n\u003c/current_model_identity\u003e`) {
		t.Fatal("model metadata can escape its data block")
	}
}
