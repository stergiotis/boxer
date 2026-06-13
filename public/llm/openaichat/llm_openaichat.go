// Package openaichat is an OpenAI-compatible chat-completions client. It
// targets any backend that speaks the /v1/chat/completions wire shape — LM
// Studio (local), Ollama (local), Google AI Studio / Gemini (remote), and
// litellm-bridged providers. Callers should depend on ClientI rather than
// the concrete Client.
//
// EnableThinking maps to the Qwen-style chat_template_kwargs.enable_thinking
// passthrough. LM Studio's bundled Qwen 3.x chat template currently ignores
// this kwarg: reasoning always runs, and the visible answer only appears
// after the model finishes thinking. Callers targeting Qwen must size
// MaxTokens to cover reasoning + answer (typical floor: 2048-4096).
// Reasoning text is surfaced separately in CompletionResponse.Reasoning so
// downstream parsing can ignore it.
//
// NumCtx is an opt-in Ollama extension (options.num_ctx). Leave it zero for
// OpenAI / Gemini endpoints, which reject the unknown options field.
package openaichat

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ChatRoleE labels one message in the OpenAI chat-completions schema.
type ChatRoleE uint8

const (
	ChatRoleSystem    ChatRoleE = 1
	ChatRoleUser      ChatRoleE = 2
	ChatRoleAssistant ChatRoleE = 3
)

var AllChatRoles = []ChatRoleE{ChatRoleSystem, ChatRoleUser, ChatRoleAssistant}

func (inst ChatRoleE) String() (s string) {
	switch inst {
	case ChatRoleSystem:
		s = "system"
	case ChatRoleUser:
		s = "user"
	case ChatRoleAssistant:
		s = "assistant"
	}
	return
}

type Message struct {
	Role    ChatRoleE
	Content string
}

// CompletionRequest is the chat-completions input. EnableThinking toggles
// reasoning mode on models that support it (Qwen 3.x via the chat template);
// it is silently ignored elsewhere. NumCtx forwards Ollama's options.num_ctx
// when non-zero; OpenAI / Gemini callers must leave it zero.
type CompletionRequest struct {
	ModelId        string
	Messages       []Message
	Temperature    float32
	MaxTokens      int32
	NumCtx         int32
	EnableThinking bool
}

type CompletionResponse struct {
	Content      string // visible assistant message
	Reasoning    string // raw reasoning trace (Qwen 3.x); empty for non-reasoning models
	InputTokens  int32
	OutputTokens int32 // includes reasoning tokens for reasoning models
}

type ClientI interface {
	Complete(ctx context.Context, req CompletionRequest) (resp CompletionResponse, err error)
	ListModels(ctx context.Context) (models []string, err error)
	Close() (err error)
}

// Client speaks OpenAI's /v1/chat/completions over HTTP. Zero-value usage is
// invalid; construct via NewClient.
type Client struct {
	baseUrl    string
	apiKey     string
	httpClient *http.Client
}

var _ ClientI = (*Client)(nil)

func NewClient(baseUrl string, apiKey string) (inst *Client, err error) {
	if baseUrl == "" {
		err = eh.Errorf("openaichat: baseUrl is empty")
		return
	}
	if !strings.HasSuffix(baseUrl, "/") {
		baseUrl = baseUrl + "/"
	}
	inst = &Client{
		baseUrl: baseUrl,
		apiKey:  apiKey,
		// No timeout — request lifetime is bounded by the caller's context.
		httpClient: &http.Client{},
	}
	return
}

// wireMessage / wireRequest / wireResponse / wireUsage mirror the OpenAI
// chat-completions JSON shape that LM Studio / Ollama / Gemini all expose.
// Wire types stay internal so callers depend on Message / CompletionRequest
// / CompletionResponse instead.

type wireMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type wireOptions struct {
	NumCtx int32 `json:"num_ctx,omitzero"`
}

// wireRequest uses omitzero on numeric fields because encoding/json/v2's
// omitempty does not omit numeric zero (only zero-length strings / slices
// / maps). Gemini's OpenAI-compat shim rejects max_tokens=0 with
// "max_output_tokens must be positive", and we want Temperature=0 to mean
// "let the provider default" rather than forcing deterministic sampling.
type wireRequest struct {
	Model              string         `json:"model"`
	Messages           []wireMessage  `json:"messages"`
	Temperature        float32        `json:"temperature,omitzero"`
	MaxTokens          int32          `json:"max_tokens,omitzero"`
	Stream             bool           `json:"stream"`
	Options            *wireOptions   `json:"options,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type wireChoice struct {
	Index        int32       `json:"index"`
	Message      wireMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type wireUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	TotalTokens      int32 `json:"total_tokens"`
}

type wireResponse struct {
	Id      string       `json:"id"`
	Object  string       `json:"object"`
	Choices []wireChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
}

type wireErrorBlock struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type wireErrorEnvelope struct {
	Error wireErrorBlock `json:"error"`
}

type wireModelEntry struct {
	Id string `json:"id"`
}

type wireModelsResponse struct {
	Data []wireModelEntry `json:"data"`
}

func (inst *Client) Complete(ctx context.Context, req CompletionRequest) (resp CompletionResponse, err error) {
	if req.ModelId == "" {
		err = eh.Errorf("openaichat: ModelId is empty")
		return
	}
	if len(req.Messages) == 0 {
		err = eh.Errorf("openaichat: Messages is empty")
		return
	}

	var body []byte
	body, err = inst.encodeRequest(req)
	if err != nil {
		err = eh.Errorf("encode request: %w", err)
		return
	}

	url := inst.baseUrl + "chat/completions"

	var httpReq *http.Request
	httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		err = eh.Errorf("new request: %w", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if inst.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+inst.apiKey)
	}

	var httpResp *http.Response
	httpResp, err = inst.httpClient.Do(httpReq)
	if err != nil {
		err = eb.Build().Str("url", url).Errorf("http do: %w", err)
		return
	}
	defer func() {
		closeErr := httpResp.Body.Close()
		if closeErr != nil && err == nil {
			err = eh.Errorf("close body: %w", closeErr)
		}
	}()

	var rawBody []byte
	rawBody, err = io.ReadAll(httpResp.Body)
	if err != nil {
		err = eh.Errorf("read body: %w", err)
		return
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		err = inst.classifyHttpError(ctx, url, httpResp.StatusCode, rawBody)
		return
	}

	var wresp wireResponse
	err = json.Unmarshal(rawBody, &wresp)
	if err != nil {
		err = eb.Build().Str("rawSnippet", snippet(string(rawBody), 256)).Errorf("decode response: %w", err)
		return
	}

	if len(wresp.Choices) == 0 {
		err = eb.Build().Str("rawSnippet", snippet(string(rawBody), 256)).Errorf("openaichat: response has no choices")
		return
	}

	cleanContent, inlineThought := extractInlineThought(wresp.Choices[0].Message.Content)
	combinedReasoning := wresp.Choices[0].Message.ReasoningContent
	if inlineThought != "" {
		if combinedReasoning != "" {
			combinedReasoning = combinedReasoning + "\n\n" + inlineThought
		} else {
			combinedReasoning = inlineThought
		}
	}

	resp = CompletionResponse{
		Content:      cleanContent,
		Reasoning:    combinedReasoning,
		InputTokens:  wresp.Usage.PromptTokens,
		OutputTokens: wresp.Usage.CompletionTokens,
	}
	return
}

// ListModels fetches available model IDs from the OpenAI-compatible /models
// endpoint. Useful for diagnosing a 404/400 on Complete: the caller can list
// what the server actually exposes. Returns nil on any non-200 so callers
// can use it from inside an error path without nesting another failure.
func (inst *Client) ListModels(ctx context.Context) (models []string, err error) {
	url := inst.baseUrl + "models"

	var httpReq *http.Request
	httpReq, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		err = eh.Errorf("new request: %w", err)
		return
	}
	if inst.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+inst.apiKey)
	}

	var httpResp *http.Response
	httpResp, err = inst.httpClient.Do(httpReq)
	if err != nil {
		err = eb.Build().Str("url", url).Errorf("http do: %w", err)
		return
	}
	defer func() {
		closeErr := httpResp.Body.Close()
		if closeErr != nil && err == nil {
			err = eh.Errorf("close body: %w", closeErr)
		}
	}()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return
	}

	var rawBody []byte
	rawBody, err = io.ReadAll(httpResp.Body)
	if err != nil {
		err = eh.Errorf("read body: %w", err)
		return
	}

	var parsed wireModelsResponse
	err = json.Unmarshal(rawBody, &parsed)
	if err != nil {
		err = eb.Build().Str("rawSnippet", snippet(string(rawBody), 256)).Errorf("decode models: %w", err)
		return
	}

	models = make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		models = append(models, m.Id)
	}
	sort.Strings(models)
	return
}

// extractInlineThought pulls <thought>...</thought> blocks out of content
// and concatenates them into a single reasoning string. Some Google-served
// Gemma 4 variants emit reasoning this way (inline tagged blocks) rather
// than via the OpenAI reasoning_content extension; both shapes need to feed
// CompletionResponse.Reasoning. Unmatched <thought> without a closing tag
// is left in place — better to have it visible than drop trailing content.
func extractInlineThought(content string) (clean, thought string) {
	clean = content
	for {
		openIdx := strings.Index(clean, "<thought>")
		if openIdx < 0 {
			return
		}
		rest := clean[openIdx+len("<thought>"):]
		closeRel := strings.Index(rest, "</thought>")
		if closeRel < 0 {
			return
		}
		inner := rest[:closeRel]
		if thought != "" {
			thought = thought + "\n" + inner
		} else {
			thought = inner
		}
		clean = clean[:openIdx] + rest[closeRel+len("</thought>"):]
	}
}

func (inst *Client) encodeRequest(req CompletionRequest) (body []byte, err error) {
	wreq := wireRequest{
		Model:       req.ModelId,
		Messages:    make([]wireMessage, 0, len(req.Messages)),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
	}
	if req.NumCtx > 0 {
		wreq.Options = &wireOptions{NumCtx: req.NumCtx}
	}
	// Only send chat_template_kwargs when the caller explicitly requests
	// thinking. LM Studio's bundled Qwen 3.x template ignores
	// enable_thinking=false anyway (reasoning is always on, see package
	// doc), and Google's OpenAI-compat shim may reject unknown fields.
	// Omitting the field is the safest portable shape.
	if req.EnableThinking {
		wreq.ChatTemplateKwargs = map[string]any{"enable_thinking": true}
	}
	for _, m := range req.Messages {
		wreq.Messages = append(wreq.Messages, wireMessage{
			Role:    m.Role.String(),
			Content: m.Content,
		})
	}
	body, err = json.Marshal(wreq)
	if err != nil {
		err = eh.Errorf("marshal: %w", err)
		return
	}
	return
}

func (inst *Client) classifyHttpError(ctx context.Context, url string, status int, rawBody []byte) (err error) {
	var env wireErrorEnvelope
	// Best-effort: error responses are usually JSON, but truncated /
	// non-JSON bodies should not mask the HTTP status. On 404 / 400 we
	// additionally probe /models so callers see what is actually exposed
	// — typical cause is a typo'd ModelId.
	unmarshalErr := json.Unmarshal(rawBody, &env)
	bld := eb.Build().
		Str("url", url).
		Int("status", status)
	if status == http.StatusNotFound || status == http.StatusBadRequest {
		models, listErr := inst.ListModels(ctx)
		if listErr == nil && len(models) > 0 {
			bld = bld.Strs("availableModels", models)
		}
	}
	if unmarshalErr != nil {
		err = bld.
			Str("rawSnippet", snippet(string(rawBody), 256)).
			Errorf("openaichat: non-2xx response (status=%d)", status)
		return
	}
	err = bld.
		Str("apiErrorType", env.Error.Type).
		Str("apiErrorMessage", env.Error.Message).
		Errorf("openaichat: non-2xx response (status=%d, message=%q)", status, env.Error.Message)
	return
}

func (inst *Client) Close() (err error) {
	inst.httpClient.CloseIdleConnections()
	return
}

// snippet trims s to at most n bytes for embedding in error payloads without
// overwhelming logs.
func snippet(s string, n int) (out string) {
	if len(s) <= n {
		out = s
		return
	}
	out = s[:n] + "…"
	return
}
