//go:build llm_generated_opus46

package commitdigest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"encoding/json/v2"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var ErrLlmRequestFailed = errors.New("LLM request failed")

type LlmClient struct {
	Endpoint   string
	Model      string
	TimeoutSec int32
	NumCtx     int32
	ApiKey     string
	client     http.Client
}

func (inst *LlmClient) Init() {
	timeout := inst.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	inst.client = http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	NumCtx int32 `json:"num_ctx,omitempty"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Options  *chatOptions  `json:"options,omitempty"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

func (inst *LlmClient) Summarize(ctx context.Context, systemPrompt string, userMessage string) (summary string, err error) {
	reqBody := chatRequest{
		Model: inst.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
	}
	if inst.NumCtx > 0 {
		reqBody.Options = &chatOptions{NumCtx: inst.NumCtx}
	}

	var buf bytes.Buffer
	buf.Grow(len(userMessage) + len(systemPrompt) + 256)
	err = json.MarshalWrite(&buf, reqBody)
	if err != nil {
		err = eh.Errorf("unable to marshal chat request: %w", err)
		return
	}

	endpoint := inst.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434/v1"
	}
	url := endpoint + "/chat/completions"

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		err = eh.Errorf("unable to create HTTP request: %w", err)
		return
	}
	if inst.ApiKey != "" {
		req.Header.Set("X-Api-Key", inst.ApiKey)
		req.Header.Set("Authorization", "Bearer "+inst.ApiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	resp, err = inst.client.Do(req)
	if err != nil {
		err = eh.Errorf("LLM request failed: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		err = eh.Errorf("unable to read LLM response: %w", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
			if models, listErr := inst.ListModels(ctx); listErr == nil && len(models) > 0 {
				err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", string(body)).Strs("availableModels", models).Errorf("LLM request failed: %w", ErrLlmRequestFailed)
				return
			}
		}
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", string(body)).Errorf("LLM request failed: %w", ErrLlmRequestFailed)
		return
	}

	var chatResp chatResponse
	err = json.Unmarshal(body, &chatResp)
	if err != nil {
		err = eh.Errorf("unable to parse LLM response: %w", err)
		return
	}

	if len(chatResp.Choices) == 0 {
		err = eh.Errorf("LLM returned no choices: %w", ErrLlmRequestFailed)
		return
	}

	summary = chatResp.Choices[0].Message.Content

	log.Debug().
		Int64("promptTokens", chatResp.Usage.PromptTokens).
		Int64("completionTokens", chatResp.Usage.CompletionTokens).
		Msg("LLM usage")
	return
}

type modelEntry struct {
	Id string `json:"id"`
}

type modelsResponse struct {
	Data []modelEntry `json:"data"`
}

// ListModels fetches available model IDs from the OpenAI-compatible /models endpoint.
func (inst *LlmClient) ListModels(ctx context.Context) (models []string, err error) {
	endpoint := inst.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434/v1"
	}
	url := endpoint + "/models"

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	if inst.ApiKey != "" {
		req.Header.Set("Authorization", "Bearer "+inst.ApiKey)
	}

	var resp *http.Response
	resp, err = inst.client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var parsed modelsResponse
	err = json.Unmarshal(body, &parsed)
	if err != nil {
		return
	}

	models = make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		models = append(models, m.Id)
	}
	sort.Strings(models)
	return
}
