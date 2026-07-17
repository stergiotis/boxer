package openaichat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func f32(v float32) *float32 { return &v }

// newServerClient starts an httptest server with handler and returns a Client
// pointed at it. The server is torn down on test cleanup.
func newServerClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL+"/", "")
	require.NoError(t, err)
	return c
}

func userReq(model string) CompletionRequest {
	return CompletionRequest{
		ModelId:  model,
		Messages: []Message{{Role: ChatRoleUser, Content: "hi"}},
	}
}

// --- request encoding -------------------------------------------------------

// TestEncodeRequestOmitsZeroNumerics guards against a Gemini-specific
// regression: encoding/json/v2's omitempty does NOT drop numeric zero, so
// MaxTokens needs omitzero and Temperature (a pointer) must stay nil to keep
// them out of the wire body. Gemini's OpenAI-compat shim rejects max_tokens=0
// with "max_output_tokens must be positive".
func TestEncodeRequestOmitsZeroNumerics(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	body, err := inst.encodeRequest(userReq("test-model"))
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "max_tokens", "max_tokens must be absent when zero")
	assert.NotContains(t, s, "temperature", "temperature must be absent when unset (nil)")
	assert.NotContains(t, s, "options", "options block must be absent when num_ctx is zero")
	assert.NotContains(t, s, "chat_template_kwargs", "chat_template_kwargs must be absent unless thinking is enabled")
}

func TestEncodeRequestEmitsPositiveNumerics(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	body, err := inst.encodeRequest(CompletionRequest{
		ModelId:     "test-model",
		Messages:    []Message{{Role: ChatRoleUser, Content: "hi"}},
		Temperature: f32(0.5),
		MaxTokens:   2048,
		NumCtx:      8192,
	})
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, `"max_tokens":2048`)
	assert.Contains(t, s, `"temperature":0.5`)
	assert.Contains(t, s, `"num_ctx":8192`)
}

// TestEncodeRequestEmitsExplicitZeroTemperature is the whole reason Temperature
// is a *float32: an explicit 0 (greedy / deterministic decoding) must reach the
// wire, whereas a bare float32 with omitzero would silently drop it.
func TestEncodeRequestEmitsExplicitZeroTemperature(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	body, err := inst.encodeRequest(CompletionRequest{
		ModelId:     "test-model",
		Messages:    []Message{{Role: ChatRoleUser, Content: "hi"}},
		Temperature: f32(0),
	})
	require.NoError(t, err)
	assert.Contains(t, string(body), `"temperature":0`)
}

func TestEncodeRequestThinkingFlag(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	body, err := inst.encodeRequest(CompletionRequest{
		ModelId:        "test-model",
		Messages:       []Message{{Role: ChatRoleUser, Content: "hi"}},
		EnableThinking: true,
	})
	require.NoError(t, err)
	s := string(body)
	assert.True(t, strings.Contains(s, `"chat_template_kwargs"`) && strings.Contains(s, `"enable_thinking":true`),
		"chat_template_kwargs.enable_thinking=true must round-trip; got %s", s)
}

// TestEncodeRequestInlinesExtra proves Extra members land at the top level of
// the wire object (llama.cpp-style sampler passthrough) and that a nil map
// leaves the body untouched.
func TestEncodeRequestInlinesExtra(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	body, err := inst.encodeRequest(CompletionRequest{
		ModelId:  "test-model",
		Messages: []Message{{Role: ChatRoleUser, Content: "hi"}},
		Extra:    map[string]any{"dry_multiplier": 0.0, "top_k": 1},
	})
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, `"dry_multiplier":0`)
	assert.Contains(t, s, `"top_k":1`)

	body, err = inst.encodeRequest(userReq("test-model"))
	require.NoError(t, err)
	assert.NotContains(t, string(body), "dry_multiplier", "nil Extra must add nothing")
}

// TestEncodeRequestExtraCollisionFails pins the collision guarantee: an Extra
// key duplicating an emitted member must fail the encode, not override it.
func TestEncodeRequestExtraCollisionFails(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	req := userReq("test-model")
	req.Extra = map[string]any{"model": "other-model"}
	_, err = inst.encodeRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

// --- inline thought extraction ----------------------------------------------

func TestExtractInlineThought(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantClean   string
		wantThought string
	}{
		{"no thought", "just answer", "just answer", ""},
		{"single thought", "before<thought>inner</thought>after", "beforeafter", "inner"},
		{"two thoughts", "<thought>a</thought>mid<thought>b</thought>", "mid", "a\n\nb"},
		{"think tag", "<think>cot</think>answer", "answer", "cot"},
		{"unclosed thought left in place", "ok<thought>oops", "ok<thought>oops", ""},
		{"unclosed think left in place", "ok<think>oops", "ok<think>oops", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotClean, gotThought := extractInlineThought(tc.in)
			assert.Equal(t, tc.wantClean, gotClean)
			assert.Equal(t, tc.wantThought, gotThought)
		})
	}
}

func TestIsIncompleteFinishReason(t *testing.T) {
	for reason, want := range map[string]bool{
		"stop":           false,
		"":               false,
		"tool_calls":     false,
		"length":         true,
		"LENGTH":         true,
		"max_tokens":     true,
		"content_filter": true,
	} {
		assert.Equal(t, want, isIncompleteFinishReason(reason), "reason=%q", reason)
	}
}

// --- Complete round-trip ----------------------------------------------------

func TestCompleteSuccess(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)
	})
	resp, err := c.Complete(context.Background(), userReq("m"))
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Content)
	assert.Equal(t, "stop", resp.FinishReason)
	assert.Equal(t, int32(3), resp.InputTokens)
	assert.Equal(t, int32(5), resp.OutputTokens)
	assert.Empty(t, resp.Reasoning)
}

// TestCompleteCombinesReasoning checks the merge of the reasoning_content
// extension with an inline <think> block — the path that feeds
// CompletionResponse.Reasoning and was previously untested.
func TestCompleteCombinesReasoning(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"<think>inline</think>answer","reasoning_content":"api"},"finish_reason":"stop"}]}`)
	})
	resp, err := c.Complete(context.Background(), userReq("m"))
	require.NoError(t, err)
	assert.Equal(t, "answer", resp.Content)
	assert.Equal(t, "api\n\ninline", resp.Reasoning)
}

// TestCompleteTruncatedIsError is the core finish_reason fix: a "length" stop
// reason surfaces as ErrIncompleteCompletion, with the partial answer still
// available for inspection.
func TestCompleteTruncatedIsError(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}],"usage":{"completion_tokens":42}}`)
	})
	resp, err := c.Complete(context.Background(), userReq("m"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIncompleteCompletion), "want ErrIncompleteCompletion, got %v", err)
	assert.Equal(t, "partial", resp.Content, "partial content stays available")
	assert.Equal(t, "length", resp.FinishReason)
	assert.Equal(t, int32(42), resp.OutputTokens)
}

func TestCompleteSendsExplicitZeroTemperature(t *testing.T) {
	var gotBody []byte
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	req := userReq("m")
	req.Temperature = f32(0)
	_, err := c.Complete(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, string(gotBody), `"temperature":0`)
}

func TestCompleteNoChoices(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	})
	_, err := c.Complete(context.Background(), userReq("m"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

func TestCompleteRejectsInvalidRole(t *testing.T) {
	c, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	_, err = c.Complete(context.Background(), CompletionRequest{
		ModelId:  "m",
		Messages: []Message{{Content: "hi"}}, // zero-value Role is invalid
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid role")
}

// TestCompleteHTTPErrorProbesModels checks that a 404 yields a structured
// error carrying the API message and that classifyHttpError probes /models.
func TestCompleteHTTPErrorProbesModels(t *testing.T) {
	var modelsHit bool
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"model not found","type":"invalid_request_error"}}`)
		case "/models":
			modelsHit = true
			_, _ = io.WriteString(w, `{"data":[{"id":"real-model"}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
	_, err := c.Complete(context.Background(), userReq("typo-model"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model not found")
	assert.Contains(t, err.Error(), "404")
	assert.True(t, modelsHit, "classifyHttpError must probe /models on 404")
}

func TestCompleteHTTPErrorNonJSON(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `<html>upstream exploded</html>`)
	})
	_, err := c.Complete(context.Background(), userReq("m"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// --- ListModels -------------------------------------------------------------

func TestListModelsSorted(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/models", r.URL.Path)
		_, _ = io.WriteString(w, `{"data":[{"id":"zeta"},{"id":"alpha"}]}`)
	})
	models, err := c.ListModels(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta"}, models)
}

// TestListModelsNonOKIsError pins the contract change: a direct caller now sees
// an error on non-2xx instead of a silent (nil, nil).
func TestListModelsNonOKIsError(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key"}}`)
	})
	models, err := c.ListModels(context.Background())
	require.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "401")
}

// --- LoadGeminiApiKey -------------------------------------------------------

func TestLoadGeminiApiKeyFromEnv(t *testing.T) {
	GeminiApiKey.SetForTest(t, "env-key")
	key, err := LoadGeminiApiKey()
	require.NoError(t, err)
	assert.Equal(t, "env-key", key)
}

func TestLoadGeminiApiKeyFromFile(t *testing.T) {
	GeminiApiKey.SetForTest(t, "") // unset + reset the resolution cache
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "gemini")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "api_key"), []byte("file-key\n"), 0o600))
	key, err := LoadGeminiApiKey()
	require.NoError(t, err)
	assert.Equal(t, "file-key", key, "trailing newline must be stripped")
}

func TestLoadGeminiApiKeyMissing(t *testing.T) {
	GeminiApiKey.SetForTest(t, "")
	t.Setenv("HOME", t.TempDir()) // empty dir, no api_key file
	_, err := LoadGeminiApiKey()
	require.Error(t, err)
}
