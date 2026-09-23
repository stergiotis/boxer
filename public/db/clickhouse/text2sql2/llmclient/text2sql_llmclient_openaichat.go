package llmclient

import (
	"context"
	"encoding/json/jsontext"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// OpenAIChatClient adapts openaichat.Client to orchestrator.LLMClientI.
// Use it to drive text2sql2 against any OpenAI-compatible endpoint — LM
// Studio, Gemini's OpenAI shim, or a litellm bridge — while keeping the
// existing OllamaClient for /api/chat servers.
//
// Zero-value usage is invalid; construct via NewOpenAIChatClient.
type OpenAIChatClient struct {
	client         openaichat.ClientI
	temperature    float32
	numCtx         int32
	seed           *int64
	responseFormat *openaichat.ResponseFormat
}

var _ orchestrator.LLMClientI = (*OpenAIChatClient)(nil)

// OpenAIChatOption configures an OpenAIChatClient.
type OpenAIChatOption func(*OpenAIChatClient)

// WithOpenAIChatTemperature sets the sampling temperature (default: 0.1).
// Pass 0 for deterministic / greedy decoding — the adapter always forwards an
// explicit temperature, so 0 reaches the provider rather than being dropped.
func WithOpenAIChatTemperature(t float32) OpenAIChatOption {
	return func(inst *OpenAIChatClient) { inst.temperature = t }
}

// WithOpenAIChatNumCtx forwards options.num_ctx to Ollama. Leave zero (the
// default) for OpenAI / Gemini endpoints, which reject the field.
func WithOpenAIChatNumCtx(n int32) OpenAIChatOption {
	return func(inst *OpenAIChatClient) { inst.numCtx = n }
}

// WithOpenAIChatSeed sets the sampling seed for reproducible output where the
// provider honors it. Pair with WithOpenAIChatTemperature(0) for deterministic
// SQL generation. Unset by default (the provider seeds randomly).
func WithOpenAIChatSeed(seed int64) OpenAIChatOption {
	return func(inst *OpenAIChatClient) { inst.seed = &seed }
}

// WithOpenAIChatResponseFormat constrains the model's output — e.g.
// openaichat.JSONObjectFormat() to force valid JSON, or JSONSchemaFormat(...)
// to pin a schema. Unset by default (free-form text). The orchestrator's
// prompt must request a shape consistent with the format.
func WithOpenAIChatResponseFormat(rf *openaichat.ResponseFormat) OpenAIChatOption {
	return func(inst *OpenAIChatClient) { inst.responseFormat = rf }
}

// NewOpenAIChatClient wraps an existing openaichat.ClientI for text2sql2.
// The caller owns the underlying client's lifetime; Close is not delegated
// here so multiple wrappers can share one transport pool.
func NewOpenAIChatClient(client openaichat.ClientI, opts ...OpenAIChatOption) (inst *OpenAIChatClient, err error) {
	if client == nil {
		err = eh.Errorf("openaichat client is nil")
		return
	}
	inst = &OpenAIChatClient{
		client:      client,
		temperature: 0.1,
	}
	for _, opt := range opts {
		opt(inst)
	}
	return
}

func (inst *OpenAIChatClient) Chat(ctx context.Context, model string, messages []orchestrator.Message) (response string, err error) {
	response, _, err = inst.ChatTools(ctx, model, messages, nil)
	return
}

var _ orchestrator.ToolClientI = (*OpenAIChatClient)(nil)

// ChatTools is Chat with tools offered (ADR-0139 §SD9): the definitions
// ride the request, the model's calls come back, and a replayed tool turn
// keeps its call id.
func (inst *OpenAIChatClient) ChatTools(ctx context.Context, model string, messages []orchestrator.Message, tools []orchestrator.Tool) (response string, calls []orchestrator.ToolCall, err error) {
	wireMessages := make([]openaichat.Message, 0, len(messages))
	for _, m := range messages {
		wireMessages = append(wireMessages, WireMessage(m))
	}
	var resp openaichat.CompletionResponse
	resp, err = inst.client.Complete(ctx, openaichat.CompletionRequest{
		ModelId:        model,
		Messages:       wireMessages,
		Temperature:    &inst.temperature,
		NumCtx:         inst.numCtx,
		Seed:           inst.seed,
		ResponseFormat: inst.responseFormat,
		Tools:          WireTools(tools),
	})
	if err != nil {
		err = eh.Errorf("openaichat complete: %w", err)
		return
	}
	response = resp.Content
	calls = CallsOf(resp.ToolCalls)
	return
}

// WireMessage maps an orchestrator message onto the client's, tool turns
// and tool calls included.
func WireMessage(m orchestrator.Message) (out openaichat.Message) {
	out = openaichat.Message{Role: translateRole(m.Role), Content: m.Content, ToolCallId: m.ToolCallId}
	for _, c := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, openaichat.ToolCall{Id: c.Id, Name: c.Name, Arguments: c.Arguments})
	}
	return
}

// WireTools maps tool definitions onto the client's.
func WireTools(tools []orchestrator.Tool) (out []openaichat.Tool) {
	for _, t := range tools {
		out = append(out, openaichat.Tool{Name: t.Name, Description: t.Description, Parameters: jsontext.Value(t.Parameters)})
	}
	return
}

// CallsOf maps the model's calls back onto the orchestrator's.
func CallsOf(calls []openaichat.ToolCall) (out []orchestrator.ToolCall) {
	for _, c := range calls {
		out = append(out, orchestrator.ToolCall{Id: c.Id, Name: c.Name, Arguments: c.Arguments})
	}
	return
}

// translateRole maps text2sql2's stringly-typed orchestrator.Message.Role
// onto openaichat.ChatRoleE. Unknown strings fall through to "user" — the
// safer default when the orchestrator emits a role we have not yet
// catalogued (it would otherwise be silently dropped).
func translateRole(role string) (out openaichat.ChatRoleE) {
	switch role {
	case "system":
		out = openaichat.ChatRoleSystem
	case "assistant":
		out = openaichat.ChatRoleAssistant
	case "tool":
		out = openaichat.ChatRoleTool
	default:
		out = openaichat.ChatRoleUser
	}
	return
}
