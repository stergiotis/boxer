package llm

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// contextSize is the model's context size and where it came from.
type contextSize struct {
	tokens int32
	source string
}

// probeWait bounds the one request the probe makes.
const probeWait = 5 * time.Second

// contextFields are the fields OpenAI-compatible servers put the context
// size in on their model list, best first: the size the model is loaded
// with before the size it supports. The model list of the OpenAI API
// itself carries none, and the size then stays unknown.
var contextFields = []string{
	"loaded_context_length", // LM Studio
	"max_model_len",         // vLLM
	"context_length",        // OpenRouter, LM Studio
	"context_window",
	"max_context_length",
	"n_ctx",
	"meta.n_ctx",
	"meta.n_ctx_train", // llama.cpp: the size it was trained with
}

// probeContext asks the endpoint once, off the caller's goroutine, and
// stores what it says; a failure leaves the size unknown and is logged.
func (inst *Service) probeContext() {
	ctx, cancel := context.WithTimeout(inst.base, probeWait)
	defer cancel()
	tokens, field, err := ProbeContextTokens(ctx, http.DefaultClient, inst.cfg.Endpoint, inst.cfg.ApiKey, inst.cfg.Model)
	switch {
	case err != nil:
		inst.log.Debug().Err(err).Msg("llm: the endpoint's model list did not answer; the context size is unknown")
	case tokens <= 0:
		inst.log.Debug().Msg("llm: the endpoint's model list names no context size; set BOXER_LLM_CONTEXT_TOKENS")
	default:
		inst.context.Store(&contextSize{tokens: tokens, source: "endpoint: " + field})
	}
}

// ProbeContextTokens reads model's context size from the model list of the
// OpenAI-compatible endpoint (GET <endpoint>/models): the entry whose id is
// model, or the only entry when there is one. It returns the size and the
// field it was read from; 0 and no error when the list names none.
func ProbeContextTokens(ctx context.Context, hc *http.Client, endpoint string, apiKey string, model string) (tokens int32, field string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/")+"/models", nil)
	if err != nil {
		err = eh.Errorf("llm: model list request: %w", err)
		return
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		err = eh.Errorf("llm: model list: %w", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = eb.Build().Int("status", resp.StatusCode).Errorf("llm: model list: the endpoint did not answer 200")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		err = eh.Errorf("llm: model list: %w", err)
		return
	}
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err = json.Unmarshal(body, &list); err != nil {
		err = eh.Errorf("llm: model list is not the expected JSON: %w", err)
		return
	}
	var entry map[string]any
	for _, e := range list.Data {
		if id, _ := e["id"].(string); id == model {
			entry = e
			break
		}
	}
	if entry == nil && len(list.Data) == 1 {
		entry = list.Data[0]
	}
	if entry == nil {
		return
	}
	for _, f := range contextFields {
		var v any = entry
		for _, part := range strings.Split(f, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				v = nil
				break
			}
			v = m[part]
		}
		if n, ok := v.(float64); ok && n > 0 && n < 1<<31 {
			return int32(n), f, nil
		}
	}
	return
}
