//go:build llm_generated_opus46

// Package llmclient provides LLMClientI implementations for the orchestrator.
package llmclient

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// OllamaClient implements orchestrator.LLMClientI using the Ollama HTTP API.
type OllamaClient struct {
	endpoint    string
	httpCli     *http.Client
	temperature float64
	numCtx      int
	logger      zerolog.Logger
}

var _ orchestrator.LLMClientI = (*OllamaClient)(nil)

// OllamaOption configures an OllamaClient.
type OllamaOption func(*OllamaClient)

// WithTemperature sets the sampling temperature (default: 0.1).
func WithTemperature(t float64) OllamaOption {
	return func(c *OllamaClient) { c.temperature = t }
}

// WithNumCtx sets the context window size (default: 8192).
func WithNumCtx(n int) OllamaOption {
	return func(c *OllamaClient) { c.numCtx = n }
}

// WithTimeout sets the HTTP client timeout (default: 120s).
func WithTimeout(d time.Duration) OllamaOption {
	return func(c *OllamaClient) { c.httpCli.Timeout = d }
}

// WithLogger sets the zerolog logger for request/response logging.
func WithLogger(logger zerolog.Logger) OllamaOption {
	return func(c *OllamaClient) { c.logger = logger }
}

// NewOllamaClient creates an OllamaClient.
//
//	client := llmclient.NewOllamaClient("http://localhost:11434",
//	    llmclient.WithTemperature(0.1),
//	    llmclient.WithNumCtx(8192),
//	)
func NewOllamaClient(endpoint string, opts ...OllamaOption) *OllamaClient {
	c := &OllamaClient{
		endpoint:    endpoint,
		httpCli:     &http.Client{Timeout: 120 * time.Second},
		temperature: 0.1,
		numCtx:      8192,
		logger:      zerolog.Nop(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Chat sends a conversation to Ollama and returns the assistant's response text.
func (inst *OllamaClient) Chat(ctx context.Context, model string, messages []orchestrator.Message) (response string, err error) {
	ollamaMessages := make([]ollamaMessage, len(messages))
	for i, m := range messages {
		ollamaMessages[i] = ollamaMessage{Role: m.Role, Content: m.Content}
	}

	reqBody := ollamaRequest{
		Model:    model,
		Messages: ollamaMessages,
		Stream:   false,
		Options: ollamaOptions{
			Temperature: inst.temperature,
			NumCtx:      inst.numCtx,
		},
	}

	bodyBytes, marshalErr := json.Marshal(reqBody)
	if marshalErr != nil {
		err = eh.Errorf("marshal request: %w", marshalErr)
		return
	}

	inst.logger.Debug().
		Str("model", model).
		Int("messages", len(messages)).
		Int("body_bytes", len(bodyBytes)).
		Msg("ollama request")

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, inst.endpoint+"/api/chat", bytes.NewReader(bodyBytes))
	if reqErr != nil {
		err = eh.Errorf("create request: %w", reqErr)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	t0 := time.Now()
	httpResp, doErr := inst.httpCli.Do(req)
	elapsed := time.Since(t0)
	if doErr != nil {
		err = eb.Build().
			Str("endpoint", inst.endpoint).
			Str("model", model).
			Stringer("elapsed", elapsed).
			Errorf("ollama request failed: %w", doErr)
		return
	}
	defer httpResp.Body.Close()

	respBytes, readErr := io.ReadAll(httpResp.Body)
	if readErr != nil {
		err = eb.Build().
			Int("status", httpResp.StatusCode).
			Stringer("elapsed", elapsed).
			Errorf("read response: %w", readErr)
		return
	}

	if httpResp.StatusCode != http.StatusOK {
		err = eb.Build().
			Int("status", httpResp.StatusCode).
			Str("model", model).
			Int("response_bytes", len(respBytes)).
			Errorf("ollama error: %s", string(respBytes))
		return
	}

	var ollamaResp ollamaResponse
	if unmarshalErr := json.Unmarshal(respBytes, &ollamaResp); unmarshalErr != nil {
		err = eb.Build().
			Int("response_bytes", len(respBytes)).
			Errorf("unmarshal response: %w", unmarshalErr)
		return
	}

	response = ollamaResp.Message.Content

	inst.logger.Debug().
		Str("model", model).
		Dur("elapsed", elapsed).
		Dur("ollama_total", ollamaResp.TotalDuration).
		Int("prompt_tokens", ollamaResp.PromptEvalCount).
		Int("eval_tokens", ollamaResp.EvalCount).
		Int("response_len", len(response)).
		Msg("ollama response")

	return
}

// Ping checks connectivity to the Ollama endpoint.
func (inst *OllamaClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.endpoint+"/api/version", nil)
	if err != nil {
		return eh.Errorf("create ping request: %w", err)
	}
	resp, err := inst.httpCli.Do(req)
	if err != nil {
		return eb.Build().Str("endpoint", inst.endpoint).Errorf("ping failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return eb.Build().
			Str("endpoint", inst.endpoint).
			Int("status", resp.StatusCode).
			Errorf("ping returned non-200")
	}
	return nil
}

// ============================================================================
// Ollama HTTP API types
// ============================================================================

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
	NumCtx      int     `json:"num_ctx,omitempty"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options,omitempty"`
}

type ollamaResponse struct {
	Model              string        `json:"model"`
	Message            ollamaMessage `json:"message"`
	Done               bool          `json:"done"`
	TotalDuration      time.Duration `json:"total_duration"`
	LoadDuration       time.Duration `json:"load_duration"`
	PromptEvalCount    int           `json:"prompt_eval_count"`
	PromptEvalDuration time.Duration `json:"prompt_eval_duration"`
	EvalCount          int           `json:"eval_count"`
	EvalDuration       time.Duration `json:"eval_duration"`
}
