package llmclient

import (
	"context"

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
	client      openaichat.ClientI
	temperature float32
	numCtx      int32
}

var _ orchestrator.LLMClientI = (*OpenAIChatClient)(nil)

// OpenAIChatOption configures an OpenAIChatClient.
type OpenAIChatOption func(*OpenAIChatClient)

// WithOpenAIChatTemperature sets the sampling temperature (default: 0.1).
func WithOpenAIChatTemperature(t float32) OpenAIChatOption {
	return func(inst *OpenAIChatClient) { inst.temperature = t }
}

// WithOpenAIChatNumCtx forwards options.num_ctx to Ollama. Leave zero (the
// default) for OpenAI / Gemini endpoints, which reject the field.
func WithOpenAIChatNumCtx(n int32) OpenAIChatOption {
	return func(inst *OpenAIChatClient) { inst.numCtx = n }
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
	wireMessages := make([]openaichat.Message, 0, len(messages))
	for _, m := range messages {
		role := translateRole(m.Role)
		wireMessages = append(wireMessages, openaichat.Message{
			Role:    role,
			Content: m.Content,
		})
	}
	var resp openaichat.CompletionResponse
	resp, err = inst.client.Complete(ctx, openaichat.CompletionRequest{
		ModelId:     model,
		Messages:    wireMessages,
		Temperature: inst.temperature,
		NumCtx:      inst.numCtx,
	})
	if err != nil {
		err = eh.Errorf("openaichat complete: %w", err)
		return
	}
	response = resp.Content
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
	default:
		out = openaichat.ChatRoleUser
	}
	return
}
