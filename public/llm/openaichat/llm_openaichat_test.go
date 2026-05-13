//go:build llm_generated_opus47

package openaichat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeRequestOmitsZeroNumerics guards against a Gemini-specific
// regression: encoding/json/v2's omitempty does NOT drop numeric zero, so
// MaxTokens / Temperature need omitzero to keep them out of the wire body.
// Gemini's OpenAI-compat shim rejects max_tokens=0 with
// "max_output_tokens must be positive".
func TestEncodeRequestOmitsZeroNumerics(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	body, err := inst.encodeRequest(CompletionRequest{
		ModelId: "test-model",
		Messages: []Message{
			{Role: ChatRoleUser, Content: "hi"},
		},
	})
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "max_tokens", "max_tokens must be absent when zero")
	assert.NotContains(t, s, "temperature", "temperature must be absent when zero")
	assert.NotContains(t, s, "options", "options block must be absent when num_ctx is zero")
	assert.NotContains(t, s, "chat_template_kwargs", "chat_template_kwargs must be absent unless thinking is enabled")
}

func TestEncodeRequestEmitsPositiveNumerics(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	body, err := inst.encodeRequest(CompletionRequest{
		ModelId:     "test-model",
		Messages:    []Message{{Role: ChatRoleUser, Content: "hi"}},
		Temperature: 0.5,
		MaxTokens:   2048,
		NumCtx:      8192,
	})
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, `"max_tokens":2048`)
	assert.Contains(t, s, `"temperature":0.5`)
	assert.Contains(t, s, `"num_ctx":8192`)
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

func TestExtractInlineThought(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantClean   string
		wantThought string
	}{
		{"no thought", "just answer", "just answer", ""},
		{"single thought", "before<thought>inner</thought>after", "beforeafter", "inner"},
		{"two thoughts", "<thought>a</thought>mid<thought>b</thought>", "mid", "a\nb"},
		{"unclosed thought left in place", "ok<thought>oops", "ok<thought>oops", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotClean, gotThought := extractInlineThought(tc.in)
			assert.Equal(t, tc.wantClean, gotClean)
			assert.Equal(t, tc.wantThought, gotThought)
		})
	}
}
