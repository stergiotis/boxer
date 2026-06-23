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
// Tools / ToolChoice, ResponseFormat, Seed, and Stop map to the standard
// chat-completions fields and are all opt-in (a zero value is omitted from the
// wire). Tool calls come back on CompletionResponse.ToolCalls alongside
// FinishReason "tool_calls". ResponseFormat stays in the OpenAI response_format
// shape — Ollama's native top-level "format" field is out of scope.
//
// Transport: by default Complete and ListModels perform a single round-trip.
// WithRetry enables bounded exponential backoff with jitter (honoring
// Retry-After) on HTTP 429 / 5xx and network errors only — never on 4xx or
// caller-context cancellation. Non-2xx responses are classified into the Err*
// sentinels for errors.Is branching. WithMaxResponseBytes caps the response
// body (default 32 MiB; 0 = unlimited), WithHTTPClient overrides the transport
// (proxy / TLS), and WithObserver receives a per-call RequestStat. The default
// client has no timeout, so request lifetime is bounded by the caller's context.
package openaichat

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/json/jsontext"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
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
	ChatRoleTool      ChatRoleE = 4
)

func (inst ChatRoleE) String() (s string) {
	switch inst {
	case ChatRoleSystem:
		s = "system"
	case ChatRoleUser:
		s = "user"
	case ChatRoleAssistant:
		s = "assistant"
	case ChatRoleTool:
		s = "tool"
	}
	return
}

// IsValid reports whether inst is one of the known chat roles. The zero value
// is not valid; Complete rejects messages carrying an unknown role rather than
// emitting an empty "role" the provider would reject.
func (inst ChatRoleE) IsValid() (ok bool) {
	switch inst {
	case ChatRoleSystem, ChatRoleUser, ChatRoleAssistant, ChatRoleTool:
		ok = true
	}
	return
}

// Message is one chat turn. For a role=tool result, set ToolCallId to the id of
// the call being answered. For a role=assistant turn that requested tools,
// ToolCalls carries those calls so the conversation can be replayed verbatim on
// the next request.
type Message struct {
	Role       ChatRoleE
	Content    string
	ToolCallId string     // role=tool: the call this message answers
	ToolCalls  []ToolCall // role=assistant: tool calls to echo back
}

// Tool declares a function the model may call. Parameters is the function's
// argument schema as raw JSON (a JSON Schema object); leave it empty for a
// no-argument function.
type Tool struct {
	Name        string
	Description string
	Parameters  jsontext.Value
}

// ToolCall is a single function call the model decided to make. Arguments is
// the raw JSON string the model emitted; it is not validated here.
type ToolCall struct {
	Id        string
	Name      string
	Arguments string
}

// ResponseFormat constrains the model's output. Type is "json_object" (valid
// JSON, no schema) or "json_schema" (schema-constrained, which also requires
// Name and Schema). Construct via JSONObjectFormat / JSONSchemaFormat.
type ResponseFormat struct {
	Type   string
	Name   string
	Schema jsontext.Value
	Strict bool
}

// JSONObjectFormat requests plain JSON-object output (OpenAI "json mode").
func JSONObjectFormat() *ResponseFormat {
	return &ResponseFormat{Type: "json_object"}
}

// JSONSchemaFormat requests schema-constrained output. name labels the schema;
// schema is the JSON Schema as raw JSON; strict requests strict adherence.
func JSONSchemaFormat(name string, schema jsontext.Value, strict bool) *ResponseFormat {
	return &ResponseFormat{Type: "json_schema", Name: name, Schema: schema, Strict: strict}
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
// Seed pairs with Temperature=0 for reproducibility where the provider honors
// it. ToolChoice is "auto" / "none" / "required", or a function name to force
// that one tool.
type CompletionRequest struct {
	ModelId        string
	Messages       []Message
	Temperature    *float32
	MaxTokens      int32
	NumCtx         int32
	Seed           *int64
	Stop           []string
	EnableThinking bool
	Tools          []Tool
	ToolChoice     string
	ResponseFormat *ResponseFormat
}

// CompletionResponse is the decoded result of a single completion. FinishReason
// is the provider's verbatim stop reason ("stop", "length", "content_filter",
// "tool_calls", …). When it marks an incomplete answer Complete returns
// ErrIncompleteCompletion with this response still populated. ToolCalls is set
// when the model requested function calls (FinishReason "tool_calls").
type CompletionResponse struct {
	Content      string     // visible assistant message
	Reasoning    string     // raw reasoning trace (reasoning_content / inline tags)
	FinishReason string     // provider stop reason, verbatim
	ToolCalls    []ToolCall // function calls the model requested, if any
	InputTokens  int32
	OutputTokens int32 // includes reasoning tokens for reasoning models
}

// Error sentinels for errors.Is branching. Complete wraps the matching one
// based on the HTTP status; the wrapped error keeps the structured context
// (status, provider message, available models on 400/404).
var (
	// ErrIncompleteCompletion: finish_reason marked the answer truncated
	// (token budget) or withheld (content filter), not completed normally.
	ErrIncompleteCompletion = errors.New("openaichat: completion did not finish normally")
	// ErrRateLimited: HTTP 429.
	ErrRateLimited = errors.New("openaichat: rate limited")
	// ErrAuth: HTTP 401 / 403.
	ErrAuth = errors.New("openaichat: authentication failed")
	// ErrModelNotFound: HTTP 404 (typically a typo'd ModelId or wrong baseUrl).
	ErrModelNotFound = errors.New("openaichat: model or endpoint not found")
	// ErrBadRequest: HTTP 400 / 422 and other 4xx.
	ErrBadRequest = errors.New("openaichat: bad request")
	// ErrServer: HTTP 5xx.
	ErrServer = errors.New("openaichat: server error")
)

// RetryPolicy bounds the retry loop. MaxAttempts counts the first try, so <=1
// disables retries. Delays grow exponentially from BaseDelay (full jitter),
// capped at MaxDelay; a provider Retry-After header overrides the computed
// delay. Construct a sane default via DefaultRetryPolicy.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// DefaultRetryPolicy is 4 attempts, 500ms base, 30s cap.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 4, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
}

// RequestStat is delivered to a WithObserver callback once per Complete call,
// after the final attempt, regardless of outcome.
type RequestStat struct {
	Model        string
	FinishReason string
	Status       int // HTTP status of the final attempt; 0 if no response
	Attempts     int
	Elapsed      time.Duration
	InputTokens  int32
	OutputTokens int32
	Err          error
}

type ClientI interface {
	Complete(ctx context.Context, req CompletionRequest) (resp CompletionResponse, err error)
	Close() (err error)
}

// Client speaks OpenAI's /v1/chat/completions over HTTP. Zero-value usage is
// invalid; construct via NewClient.
type Client struct {
	baseUrl          string
	apiKey           string
	httpClient       *http.Client
	retry            RetryPolicy
	maxResponseBytes int64
	observer         func(RequestStat)
}

var _ ClientI = (*Client)(nil)

const defaultMaxResponseBytes = 32 << 20 // 32 MiB

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

// WithRetry enables bounded retries with exponential backoff. Retries apply
// only to HTTP 429 / 5xx and network errors; 4xx and caller-context
// cancellation are never retried. Disabled by default.
func WithRetry(p RetryPolicy) Option {
	return func(inst *Client) { inst.retry = p }
}

// WithMaxResponseBytes caps the response body read from the server. A value of
// 0 (or negative) means unlimited. Default: 32 MiB.
func WithMaxResponseBytes(n int64) Option {
	return func(inst *Client) { inst.maxResponseBytes = n }
}

// WithObserver registers a callback invoked once per Complete with timing,
// token, and outcome stats. Keep it cheap and non-blocking.
func WithObserver(fn func(RequestStat)) Option {
	return func(inst *Client) { inst.observer = fn }
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
		baseUrl:          baseUrl,
		apiKey:           apiKey,
		httpClient:       defaultHTTPClient(),
		retry:            RetryPolicy{MaxAttempts: 1}, // retries opt-in via WithRetry
		maxResponseBytes: defaultMaxResponseBytes,
	}
	for _, opt := range opts {
		opt(inst)
	}
	return
}

// defaultHTTPClient clones the stdlib default transport and widens the
// per-host idle-connection pool (default 2) so fan-out callers reuse
// keep-alive connections instead of churning them. No timeout — context bound.
func defaultHTTPClient() (c *http.Client) {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := tr.Clone()
		cloned.MaxIdleConnsPerHost = 32
		c = &http.Client{Transport: cloned}
		return
	}
	c = &http.Client{}
	return
}

// wireMessage / wireRequest / wireResponse / wireUsage mirror the OpenAI
// chat-completions JSON shape that LM Studio / Ollama / Gemini all expose.
// Wire types stay internal so callers depend on Message / CompletionRequest
// / CompletionResponse instead.

type wireMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallId       string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	Id       string               `json:"id"`
	Type     string               `json:"type"`
	Function wireToolCallFunction `json:"function"`
}

type wireToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

type wireToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  jsontext.Value `json:"parameters,omitempty"`
}

type wireResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *wireJSONSchema `json:"json_schema,omitempty"`
}

type wireJSONSchema struct {
	Name   string         `json:"name"`
	Schema jsontext.Value `json:"schema,omitempty"`
	Strict bool           `json:"strict,omitzero"`
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
	Model              string              `json:"model"`
	Messages           []wireMessage       `json:"messages"`
	Temperature        *float32            `json:"temperature,omitempty"`
	MaxTokens          int32               `json:"max_tokens,omitzero"`
	Seed               *int64              `json:"seed,omitempty"`
	Stop               []string            `json:"stop,omitempty"`
	Stream             bool                `json:"stream"`
	Options            *wireOptions        `json:"options,omitempty"`
	ChatTemplateKwargs map[string]any      `json:"chat_template_kwargs,omitempty"`
	ResponseFormat     *wireResponseFormat `json:"response_format,omitempty"`
	Tools              []wireTool          `json:"tools,omitempty"`
	ToolChoice         any                 `json:"tool_choice,omitempty"`
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
		if m.Role == ChatRoleTool && m.ToolCallId == "" {
			err = eb.Build().Int("index", i).Errorf("openaichat: tool message %d is missing ToolCallId", i)
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

	t0 := time.Now()
	var status, attempts int
	var rawBody []byte
	if inst.observer != nil {
		defer func() {
			inst.observer(RequestStat{
				Model:        req.ModelId,
				FinishReason: resp.FinishReason,
				Status:       status,
				Attempts:     attempts,
				Elapsed:      time.Since(t0),
				InputTokens:  resp.InputTokens,
				OutputTokens: resp.OutputTokens,
				Err:          err,
			})
		}()
	}

	status, rawBody, attempts, err = inst.sendRetrying(ctx, http.MethodPost, url, body)
	if err != nil {
		return
	}

	if status < 200 || status >= 300 {
		err = inst.classifyHttpError(ctx, url, status, rawBody)
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
		ToolCalls:    fromWireToolCalls(choice.Message.ToolCalls),
		InputTokens:  wresp.Usage.PromptTokens,
		OutputTokens: wresp.Usage.CompletionTokens,
	}

	// A truncated or content-filtered answer must not masquerade as a complete
	// one. resp stays populated so the caller can inspect the partial content.
	// tool_calls is a valid terminal state and is not flagged here.
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
// its error on 400/404). A non-2xx status returns an error; classifyHttpError
// calls this best-effort and ignores that error.
func (inst *Client) ListModels(ctx context.Context) (models []string, err error) {
	url := inst.baseUrl + "models"

	var status int
	var rawBody []byte
	status, rawBody, _, err = inst.sendRetrying(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	if status < 200 || status >= 300 {
		err = eb.Build().
			Str("url", url).
			Int("status", status).
			Str("rawSnippet", snippet(string(rawBody), 256)).
			Errorf("openaichat: list models non-2xx (status=%d)", status)
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

// sendRetrying performs send() up to RetryPolicy.MaxAttempts times, backing off
// between retryable failures. It returns the last attempt's status / body and
// the number of attempts made.
func (inst *Client) sendRetrying(ctx context.Context, method, url string, body []byte) (status int, raw []byte, attempts int, err error) {
	maxAttempts := max(inst.retry.MaxAttempts, 1)
	for attempt := 1; ; attempt++ {
		attempts = attempt
		var retryAfter time.Duration
		status, raw, retryAfter, err = inst.send(ctx, method, url, body)

		if attempt >= maxAttempts || !retryable(status, err) {
			return
		}
		delay := inst.backoff(attempt, retryAfter)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			if err == nil {
				err = eb.Build().Str("url", url).Errorf("openaichat: context done during retry backoff: %w", ctx.Err())
			}
			return
		case <-timer.C:
		}
	}
}

// send performs a single round-trip: build the request, set headers, execute,
// and read (up to maxResponseBytes) the body. A non-2xx status is returned in
// status with err == nil; err is set only for transport / read failures.
// retryAfter carries the parsed Retry-After header when present.
func (inst *Client) send(ctx context.Context, method, url string, body []byte) (status int, raw []byte, retryAfter time.Duration, err error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	var httpReq *http.Request
	httpReq, err = http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		err = eh.Errorf("new request: %w", err)
		return
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")
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

	status = httpResp.StatusCode
	retryAfter = parseRetryAfter(httpResp.Header.Get("Retry-After"))

	reader := io.Reader(httpResp.Body)
	if inst.maxResponseBytes > 0 {
		reader = io.LimitReader(httpResp.Body, inst.maxResponseBytes+1)
	}
	raw, err = io.ReadAll(reader)
	if err != nil {
		err = eh.Errorf("read body: %w", err)
		return
	}
	if inst.maxResponseBytes > 0 && int64(len(raw)) > inst.maxResponseBytes {
		err = eb.Build().Str("url", url).Int("status", status).
			Errorf("openaichat: response exceeds %d bytes", inst.maxResponseBytes)
		return
	}
	return
}

// retryable reports whether a failed attempt should be retried: a transport /
// read error (except caller-context cancellation), or a transient HTTP status.
func retryable(status int, err error) (ok bool) {
	if err != nil {
		ok = !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
		return
	}
	switch status {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		ok = true
	}
	return
}

// backoff returns the delay before the next attempt: the provider's Retry-After
// if present, otherwise exponential growth from BaseDelay with full jitter,
// both capped at MaxDelay.
func (inst *Client) backoff(attempt int, retryAfter time.Duration) (delay time.Duration) {
	maxD := inst.retry.MaxDelay
	if retryAfter > 0 {
		if maxD > 0 && retryAfter > maxD {
			return maxD
		}
		return retryAfter
	}
	base := inst.retry.BaseDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d <= 0 || (maxD > 0 && d >= maxD) {
			d = maxD
			break
		}
	}
	if maxD > 0 && d > maxD {
		d = maxD
	}
	if d <= 0 {
		d = base
	}
	// full jitter: uniform in [0, d]
	delay = time.Duration(rand.Int64N(int64(d) + 1))
	return
}

// parseRetryAfter parses a Retry-After header value (delta-seconds or HTTP-date)
// into a non-negative duration; an unparseable / past value yields 0.
func parseRetryAfter(v string) (d time.Duration) {
	if v == "" {
		return
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		if secs > 0 {
			d = time.Duration(secs) * time.Second
		}
		return
	}
	if t, err := http.ParseTime(v); err == nil {
		if until := time.Until(t); until > 0 {
			d = until
		}
	}
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
		Seed:        req.Seed,
		Stop:        req.Stop,
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
	if req.ResponseFormat != nil {
		err = validateResponseFormat(req.ResponseFormat)
		if err != nil {
			return
		}
		wreq.ResponseFormat = toWireResponseFormat(req.ResponseFormat)
	}
	if len(req.Tools) > 0 {
		wreq.Tools = toWireTools(req.Tools)
	}
	if req.ToolChoice != "" {
		if len(req.Tools) == 0 {
			err = eh.Errorf("openaichat: ToolChoice set without Tools")
			return
		}
		wreq.ToolChoice = toWireToolChoice(req.ToolChoice)
	}
	for _, m := range req.Messages {
		wreq.Messages = append(wreq.Messages, toWireMessage(m))
	}
	body, err = json.Marshal(wreq)
	if err != nil {
		err = eh.Errorf("marshal: %w", err)
		return
	}
	return
}

func validateResponseFormat(rf *ResponseFormat) (err error) {
	switch rf.Type {
	case "json_object":
	case "json_schema":
		if rf.Name == "" || len(rf.Schema) == 0 {
			err = eh.Errorf("openaichat: json_schema response format requires Name and Schema")
		}
	default:
		err = eb.Build().Str("type", rf.Type).Errorf("openaichat: unknown ResponseFormat.Type %q", rf.Type)
	}
	return
}

func toWireMessage(m Message) (wm wireMessage) {
	wm = wireMessage{
		Role:       m.Role.String(),
		Content:    m.Content,
		ToolCallId: m.ToolCallId,
	}
	if len(m.ToolCalls) > 0 {
		wm.ToolCalls = make([]wireToolCall, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				Id:       tc.Id,
				Type:     "function",
				Function: wireToolCallFunction{Name: tc.Name, Arguments: tc.Arguments},
			})
		}
	}
	return
}

func toWireTools(tools []Tool) (out []wireTool) {
	out = make([]wireTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, wireTool{
			Type: "function",
			Function: wireToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return
}

// toWireToolChoice maps the overloaded ToolChoice string: the keywords pass
// through as a bare string, anything else is treated as a function name to
// force ({"type":"function","function":{"name":…}}).
func toWireToolChoice(choice string) (out any) {
	switch choice {
	case "auto", "none", "required":
		out = choice
	default:
		out = map[string]any{
			"type":     "function",
			"function": map[string]any{"name": choice},
		}
	}
	return
}

func toWireResponseFormat(rf *ResponseFormat) (w *wireResponseFormat) {
	w = &wireResponseFormat{Type: rf.Type}
	if rf.Type == "json_schema" {
		w.JSONSchema = &wireJSONSchema{Name: rf.Name, Schema: rf.Schema, Strict: rf.Strict}
	}
	return
}

func fromWireToolCalls(wtc []wireToolCall) (out []ToolCall) {
	if len(wtc) == 0 {
		return
	}
	out = make([]ToolCall, 0, len(wtc))
	for _, w := range wtc {
		out = append(out, ToolCall{Id: w.Id, Name: w.Function.Name, Arguments: w.Function.Arguments})
	}
	return
}

func sentinelForStatus(status int) (sentinel error) {
	switch {
	case status == http.StatusTooManyRequests:
		sentinel = ErrRateLimited
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		sentinel = ErrAuth
	case status == http.StatusNotFound:
		sentinel = ErrModelNotFound
	case status >= 500:
		sentinel = ErrServer
	default:
		sentinel = ErrBadRequest // 400 / 422 and other 4xx
	}
	return
}

func (inst *Client) classifyHttpError(ctx context.Context, url string, status int, rawBody []byte) (err error) {
	sentinel := sentinelForStatus(status)
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
			Errorf("openaichat: non-2xx response (status=%d): %w", status, sentinel)
		return
	}
	err = bld.
		Str("apiErrorType", env.Error.Type).
		Str("apiErrorMessage", env.Error.Message).
		Errorf("openaichat: non-2xx response (status=%d, message=%q): %w", status, env.Error.Message, sentinel)
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
