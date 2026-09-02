package transform

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

const goodDoc = `---
title: Improve style
summary: Tighten the prose.
icon: "X"
scope: selection
temperature: 0.3
max-tokens: 2048
---

You are a copy editor.
`

func TestParseDocSource_ReadsTheWholeShape(t *testing.T) {
	def, err := ParseDocSource("b", "sub/improve-style.md", []byte(goodDoc))
	require.NoError(t, err)
	assert.Equal(t, "b", def.BookId)
	assert.Equal(t, "improve-style", def.Slug, "the filename base is the slug, directories stripped")
	assert.Equal(t, "Improve style", def.Title)
	assert.Equal(t, "Tighten the prose.", def.Summary)
	assert.Equal(t, "X", def.Icon)
	assert.Equal(t, ScopeSelection, def.Scope)
	require.NotNil(t, def.Temperature)
	assert.InDelta(t, 0.3, float64(*def.Temperature), 1e-6)
	assert.Equal(t, int32(2048), def.MaxTokens)
	assert.Equal(t, "You are a copy editor.", def.System, "the whole body after frontmatter, trimmed")
}

func TestParseDocSource_Errors(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
	}{
		{"missing title", "a.md", "---\nsummary: s\n---\nbody\n"},
		{"missing summary", "a.md", "---\ntitle: t\n---\nbody\n"},
		{"unknown scope", "a.md", "---\ntitle: t\nsummary: s\nscope: paragraph\n---\nbody\n"},
		{"empty body", "a.md", "---\ntitle: t\nsummary: s\n---\n\n"},
		{"no frontmatter at all", "a.md", "just a prompt\n"},
		{"bad slug", "Not_A_Slug.md", "---\ntitle: t\nsummary: s\n---\nbody\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDocSource("b", tc.path, []byte(tc.src))
			assert.Error(t, err)
		})
	}
}

func TestParseDocSource_DefaultsAndCRLF(t *testing.T) {
	src := "---\r\ntitle: t\r\nsummary: s\r\n---\r\nbody line\r\n"
	def, err := ParseDocSource("b", "a.md", []byte(src))
	require.NoError(t, err)
	assert.Equal(t, ScopeSelection, def.Scope, "selection is the default scope")
	assert.Nil(t, def.Temperature)
	assert.Zero(t, def.MaxTokens)
	assert.Equal(t, "body line", def.System)
}

func TestParseBook_WalksOnlyMarkdown(t *testing.T) {
	fsys := fstest.MapFS{
		"one.md":    {Data: []byte("---\ntitle: One\nsummary: s\n---\nbody\n")},
		"two.md":    {Data: []byte("---\ntitle: Two\nsummary: s\n---\nbody\n")},
		"README":    {Data: []byte("not a prompt")},
		"broken.md": {Data: []byte("---\ntitle: t\n---\nbody\n")},
	}
	defs, errs := ParseBook("b", fsys)
	assert.Len(t, defs, 2, "the broken sibling costs itself, not the book")
	assert.Len(t, errs, 1)
}

// ---------------------------------------------------------------------------
// Running
// ---------------------------------------------------------------------------

// fakeClient scripts one Complete answer and records the request it saw.
type fakeClient struct {
	req  openaichat.CompletionRequest
	resp openaichat.CompletionResponse
	err  error
}

func (f *fakeClient) Complete(_ context.Context, req openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	f.req = req
	return f.resp, f.err
}
func (f *fakeClient) Close() (err error) { return }

func runCfg() Config {
	return Config{Endpoint: "http://localhost:9/v1", Model: "m", MaxTokens: 1024, Timeout: time.Second}
}

func TestRun_ComposesTheRoundTrip(t *testing.T) {
	temp := float32(0.5)
	fc := &fakeClient{resp: openaichat.CompletionResponse{Content: "out", InputTokens: 10, OutputTokens: 20}}
	def := PromptDef{Slug: "p", System: "sys", Temperature: &temp}

	res, err := Run(context.Background(), fc, runCfg(), def, "input text")
	require.NoError(t, err)

	require.Len(t, fc.req.Messages, 2)
	assert.Equal(t, openaichat.ChatRoleSystem, fc.req.Messages[0].Role)
	assert.Equal(t, "sys", fc.req.Messages[0].Content)
	assert.Equal(t, openaichat.ChatRoleUser, fc.req.Messages[1].Role)
	assert.Equal(t, "input text", fc.req.Messages[1].Content)
	assert.Equal(t, "m", fc.req.ModelId)
	assert.Equal(t, &temp, fc.req.Temperature)
	assert.Equal(t, int32(1024), fc.req.MaxTokens, "the config ceiling when the prompt states none")

	assert.Equal(t, "out", res.Content)
	assert.Equal(t, int32(10), res.InputTokens)
	assert.Equal(t, int32(20), res.OutputTokens)
	assert.False(t, res.Truncated)
}

func TestRun_PromptMaxTokensWins(t *testing.T) {
	fc := &fakeClient{resp: openaichat.CompletionResponse{Content: "out"}}
	_, err := Run(context.Background(), fc, runCfg(), PromptDef{System: "sys", MaxTokens: 64}, "in")
	require.NoError(t, err)
	assert.Equal(t, int32(64), fc.req.MaxTokens)
}

func TestRun_TruncationIsAResultNotAnError(t *testing.T) {
	fc := &fakeClient{
		resp: openaichat.CompletionResponse{Content: "partial"},
		err:  openaichat.ErrIncompleteCompletion,
	}
	res, err := Run(context.Background(), fc, runCfg(), PromptDef{System: "sys"}, "in")
	require.NoError(t, err, "content the provider already produced is handed over, marked")
	assert.True(t, res.Truncated)
	assert.Equal(t, "partial", res.Content)
}

func TestRun_TruncationWithNothingIsAnError(t *testing.T) {
	fc := &fakeClient{err: openaichat.ErrIncompleteCompletion}
	_, err := Run(context.Background(), fc, runCfg(), PromptDef{System: "sys"}, "in")
	assert.Error(t, err)
}

func TestRun_EmptyCompletionIsAnError(t *testing.T) {
	fc := &fakeClient{resp: openaichat.CompletionResponse{Content: ""}}
	_, err := Run(context.Background(), fc, runCfg(), PromptDef{System: "sys"}, "in")
	assert.Error(t, err)
}

func TestRun_HonoursTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := &blockingClient{}
	_, err := Run(ctx, blocked, runCfg(), PromptDef{System: "sys"}, "in")
	assert.Error(t, err)
}

// blockingClient waits for its context, the shape of a stalled provider.
type blockingClient struct{}

func (b *blockingClient) Complete(ctx context.Context, _ openaichat.CompletionRequest) (resp openaichat.CompletionResponse, err error) {
	<-ctx.Done()
	err = ctx.Err()
	return
}
func (b *blockingClient) Close() (err error) { return }

func TestFailureLine_NamesTheSentinels(t *testing.T) {
	assert.Equal(t, "", FailureLine(nil))
	assert.Contains(t, FailureLine(context.DeadlineExceeded), "timeout")
	assert.Contains(t, FailureLine(openaichat.ErrAuth), "key")
	assert.Contains(t, FailureLine(openaichat.ErrModelNotFound), "model")
	assert.Equal(t, "the request failed", FailureLine(errors.New("weird")))
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

func TestConfigFromEnv_GatesOnEndpointAndModel(t *testing.T) {
	Endpoint.SetForTest(t, "")
	Model.SetForTest(t, "")
	_, enabled := ConfigFromEnv()
	assert.False(t, enabled, "no endpoint, no surface")

	Endpoint.SetForTest(t, "http://localhost:1234/v1")
	_, enabled = ConfigFromEnv()
	assert.False(t, enabled, "an endpoint without a model is still not enabled — a wrong default model is worse than a refusal")

	Model.SetForTest(t, "qwen3")
	cfg, enabled := ConfigFromEnv()
	assert.True(t, enabled)
	assert.Equal(t, "http://localhost:1234/v1", cfg.Endpoint)
	assert.Equal(t, "qwen3", cfg.Model)
	assert.Equal(t, int32(4096), cfg.MaxTokens)
	assert.Equal(t, 120*time.Second, cfg.Timeout)
}

func TestEndpointHost(t *testing.T) {
	assert.Equal(t, "localhost:1234", EndpointHost("http://localhost:1234/v1"))
	assert.Equal(t, "api.example.com", EndpointHost("https://api.example.com/v1"))
	assert.Equal(t, "not a url", EndpointHost("not a url"))
}

// ---------------------------------------------------------------------------
// The registry
// ---------------------------------------------------------------------------

func TestRegisterPromptBook_RefusesDuplicatesAndEmpties(t *testing.T) {
	assert.Error(t, RegisterPromptBook("", fstest.MapFS{}))
	assert.Error(t, RegisterPromptBook("x", nil))
	assert.Error(t, RegisterPromptBook("mdedit", fstest.MapFS{}), "the embedded book already claimed the id")
}
