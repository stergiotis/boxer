package commitdigest

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// resolveLlmApiKey collapses the three flag inputs (--gemini-api-key,
// --llm-apikey + LLM_API_KEY env, and the Gemini precedence chain) into the
// single Authorization-Bearer string passed to openaichat.NewClient.
//
// Resolution order:
//  1. --gemini-api-key set to a non-empty value → use it
//  2. --gemini-api-key explicitly set (even to "") → walk the Gemini
//     precedence chain (GEMINI_API_KEY → ~/.config/gemini/api_key)
//  3. --llm-endpoint URL looks like Gemini → walk the chain (best-effort;
//     fall through to (4) if the chain yields nothing)
//  4. --llm-apikey (or LLM_API_KEY env) → use it
//
// Returns an empty key for local Ollama / LM Studio runs that need no auth.
func resolveLlmApiKey(c *cli.Context) (key string, err error) {
	if c.IsSet("gemini-api-key") {
		explicit := c.String("gemini-api-key")
		if explicit != "" {
			key = explicit
			return
		}
		key, err = openaichat.LoadGeminiApiKey()
		return
	}

	endpoint := c.String("llm-endpoint")
	if isGeminiEndpoint(endpoint) {
		var chainKey string
		var chainErr error
		chainKey, chainErr = openaichat.LoadGeminiApiKey()
		if chainErr == nil && chainKey != "" {
			key = chainKey
			return
		}
		// chain failed silently → fall through to generic flag
	}

	key = c.String("llm-apikey")
	return
}

// isGeminiEndpoint returns true when the URL points at Google AI Studio's
// OpenAI-compatible endpoint. URL-sniffing is brittle in general but keeps
// the CLI single-flag-per-concept; the explicit --gemini-api-key path
// remains available for users hitting a Gemini-shaped URL we do not yet
// recognise.
func isGeminiEndpoint(url string) (ok bool) {
	ok = strings.Contains(url, "generativelanguage.googleapis.com")
	return
}

// summarizeOnce runs a single system+user round-trip against the
// openaichat client and returns the visible content. timeoutSec wraps the
// call in a context.WithTimeout so a stalled provider does not block the
// whole chunked-summarisation loop.
func summarizeOnce(parent context.Context, llm openaichat.ClientI, model string, numCtx int32, timeoutSec int, system string, user string) (content string, err error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	var resp openaichat.CompletionResponse
	resp, err = llm.Complete(ctx, openaichat.CompletionRequest{
		ModelId: model,
		Messages: []openaichat.Message{
			{Role: openaichat.ChatRoleSystem, Content: system},
			{Role: openaichat.ChatRoleUser, Content: user},
		},
		NumCtx: numCtx,
	})
	if err != nil {
		err = eh.Errorf("openaichat complete: %w", err)
		return
	}
	content = resp.Content
	log.Debug().
		Int32("promptTokens", resp.InputTokens).
		Int32("completionTokens", resp.OutputTokens).
		Msg("LLM usage")
	return
}
