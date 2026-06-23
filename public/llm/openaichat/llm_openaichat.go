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
// MaxTokens to cover reasoning + answer (typical floor: 2048-4096); when the
// budget is too small the provider returns finish_reason="length" and Complete
// reports ErrIncompleteCompletion rather than silently handing back a
// truncated answer. Reasoning text is surfaced separately in
// CompletionResponse.Reasoning so downstream parsing can ignore it.
//
// NumCtx is an opt-in Ollama extension (options.num_ctx). Leave it zero for
// OpenAI / Gemini endpoints, which reject the unknown options field.
//
// Transport: Complete and ListModels each perform exactly one round-trip —
// retries and backoff (e.g. on HTTP 429 / 503) are the caller's
// responsibility. The default *http.Client has no timeout, so request lifetime
// is bounded by the caller's context; inject a custom client via WithHTTPClient
// for proxy / TLS / transport tuning.
package openaichat

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

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

// IsValid reports whether inst is one of the known chat roles. The zero value
// is not valid; Complete rejects messages carrying an unknown role rather than
// emitting an empty "role" the provider would reject.
func (inst ChatRoleE) IsValid() (ok bool) {
	switch inst {
	case ChatRoleSystem, ChatRoleUser, ChatRoleAssistant:
		ok = true
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
//
// Temperature is a pointer so the caller can request deterministic sampling: a
// nil Temperature lets the provider apply its own default, while a non-nil
// value — including a pointer to 0 — is sent verbatim. (A bare float32 could
// not distinguish "unset" from "0", and 0 is the value greedy decoding needs.)
type CompletionRequest struct {
	ModelId        string
	Messages       []Message
	Temperature    *float32
	MaxTokens      int32
	NumCtx         int32
	EnableThinking bool
}

// CompletionResponse is the decoded result of a single completion. FinishReason
// is the provider's verbatim stop reason ("stop", "length", "content_filter",
// …). When it marks an incomplete answer Complete returns ErrIncompleteCompletion
// with this response still populated, so a caller may inspect the partial
// Content if it chooses.
type CompletionResponse struct {
	Content      string // visible assistant message
	Reasoning    string // raw reasoning trace (reasoning_content / inline tags); empty for non-reasoning models
	FinishReason string // provider stop reason, verbatim
	InputTokens  int32
	OutputTokens int32 // includes reasoning tokens for reasoning models
}

// ErrIncompleteCompletion is returned by Complete when the provider's
// finish_reason indicates the answer was truncated (token budget exhausted) or
// withheld (content filter) rather than completed normally. The returned
// CompletionResponse is still populated; use errors.Is to detect this case.
var ErrIncompleteCompletion = errors.New("openaichat: completion did not finish normally")

type ClientI interface {
	Complete(ctx context.Context, req CompletionRequest) (resp CompletionResponse, err error)
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

// Option configures a Client at construction.
type Option func(*Client)

// WithHTTPClient injects a custom *http.Client — for a proxy, TLS settings, or
// a test transport. A nil client is ignored. The default client has no timeout;
// request lifetime is bounded by the caller's context.
func WithHTTPClient(hc *http.Client) Option {
	return func(inst *Client) {
		if hc != nil {
			inst.httpClient = hc
		}
	}
}

func NewClient(baseUrl string, apiKey string, opts ...Option) (inst *Client, err error) {
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
	for _, opt := range opts {
		opt(inst)
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

// wireRequest mirrors the OpenAI chat-completions request. MaxTokens uses
// omitzero because encoding/json/v2's omitempty does not drop a numeric zero
// (only empty strings / slices / maps): Gemini's OpenAI-compat shim rejects
// max_tokens=0 with "max_output_tokens must be positive". Temperature is a
// pointer with omitempty so a nil value is omitted (provider default) while a
// non-nil pointer — even to 0 — is sent verbatim; an omitzero numeric would
// instead swallow an intentional temperature=0.
type wireRequest struct {
	Model              string         `json:"model"`
	Messages           []wireMessage  `json:"messages"`
	Temperature        *float32       `json:"temperature,omitempty"`
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
	for i, m := range req.Messages {
		if !m.Role.IsValid() {
			err = eb.Build().Int("index", i).Errorf("openaichat: message %d has invalid role %d", i, uint8(m.Role))
			return
		}
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

	choice := wresp.Choices[0]
	cleanContent, inlineThought := extractInlineThought(choice.Message.Content)
	resp = CompletionResponse{
		Content:      cleanContent,
		Reasoning:    joinReasoning(choice.Message.ReasoningContent, inlineThought),
		FinishReason: choice.FinishReason,
		InputTokens:  wresp.Usage.PromptTokens,
		OutputTokens: wresp.Usage.CompletionTokens,
	}

	// A truncated or content-filtered answer must not masquerade as a complete
	// one. resp stays populated so the caller can inspect the partial content.
	if isIncompleteFinishReason(resp.FinishReason) {
		err = eb.Build().
			Str("finishReason", resp.FinishReason).
			Int("outputTokens", int(resp.OutputTokens)).
			Errorf("openaichat: completion finish_reason=%q: %w", resp.FinishReason, ErrIncompleteCompletion)
		return
	}
	return
}

// ListModels fetches available model IDs from the OpenAI-compatible /models
// endpoint, sorted. Useful for diagnosing a 404/400 on Complete: the caller can
// see what the server actually exposes (Complete already embeds this list in
// its error on 400/404). A non-2xx status returns an error;
// classifyHttpError calls this best-effort and ignores that error.
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

	// Read the body before inspecting status so the connection can be reused
	// (keep-alive needs the body drained) and the bytes are available for the
	// error snippet.
	var rawBody []byte
	rawBody, err = io.ReadAll(httpResp.Body)
	if err != nil {
		err = eh.Errorf("read body: %w", err)
		return
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		err = eb.Build().
			Str("url", url).
			Int("status", httpResp.StatusCode).
			Str("rawSnippet", snippet(string(rawBody), 256)).
			Errorf("openaichat: list models non-2xx (status=%d)", httpResp.StatusCode)
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

// inlineThoughtTags are the inline reasoning delimiters some chat models emit
// in the visible content instead of (or in addition to) the OpenAI
// reasoning_content extension: DeepSeek-R1 / Qwen use <think>, and some
// Google-served Gemma variants use <thought>.
var inlineThoughtTags = [...]struct{ open, close string }{
	{"<think>", "</think>"},
	{"<thought>", "</thought>"},
}

// extractInlineThought pulls inline reasoning blocks (see inlineThoughtTags)
// out of content and concatenates them into a single reasoning string. Both
// the OpenAI reasoning_content extension and these inline tags need to feed
// CompletionResponse.Reasoning. An opening tag with no matching close is left
// in place — better to keep it visible than drop trailing content.
func extractInlineThought(content string) (clean, thought string) {
	clean = content
	for _, tag := range inlineThoughtTags {
		var blocks string
		clean, blocks = stripTaggedBlocks(clean, tag.open, tag.close)
		thought = joinReasoning(thought, blocks)
	}
	return
}

// stripTaggedBlocks removes every openTag…closeTag delimited block from s,
// returning the remaining text and the joined block contents. An unmatched
// open tag terminates the scan and is left in place.
func stripTaggedBlocks(s, openTag, closeTag string) (clean, blocks string) {
	clean = s
	for {
		before, afterOpen, found := strings.Cut(clean, openTag)
		if !found {
			return
		}
		inner, afterClose, found := strings.Cut(afterOpen, closeTag)
		if !found {
			return
		}
		blocks = joinReasoning(blocks, inner)
		clean = before + afterClose
	}
}

// joinReasoning concatenates two reasoning fragments with a blank line,
// dropping empties. Used to merge the reasoning_content extension with any
// inline blocks, and to merge multiple inline blocks with one another.
func joinReasoning(a, b string) (out string) {
	switch {
	case a == "":
		out = b
	case b == "":
		out = a
	default:
		out = a + "\n\n" + b
	}
	return
}

// isIncompleteFinishReason reports whether a finish_reason marks an answer that
// was truncated (token budget exhausted) or withheld (content filter) rather
// than completed normally. Unknown or empty reasons are treated as complete to
// avoid false positives across providers that omit or rename the field.
func isIncompleteFinishReason(reason string) (incomplete bool) {
	switch strings.ToLower(reason) {
	case "length", "max_tokens", "content_filter":
		incomplete = true
	}
	return
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

// snippet trims s to at most n bytes — cut back to a UTF-8 rune boundary — for
// embedding in error payloads without overwhelming logs or emitting invalid
// UTF-8.
func snippet(s string, n int) (out string) {
	if len(s) <= n {
		out = s
		return
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	out = s[:cut] + "…"
	return
}
