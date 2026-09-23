package llm

import "github.com/stergiotis/boxer/public/config/env"

// The ADR-0009 env registry entries: the host's one provider (ADR-0254
// §SD2), lifted from mdedit's `BOXER_MDEDIT_LLM_*` (ADR-0216 §SD3) and
// renamed. Neither the endpoint nor the model has a default: a wrong
// default model is worse than a refusal, and the service refuses with the
// reason when either is unset.
var (
	// Endpoint is the OpenAI-compatible base URL. Unset means no model:
	// `llm.describe` says so and `llm.complete` refuses.
	Endpoint = env.NewString(env.Spec{
		Name:        "BOXER_LLM_ENDPOINT",
		Description: "OpenAI-compatible chat-completions base URL the host's llm service talks to (e.g. http://localhost:1234/v1); unset means no model is offered to apps",
		Category:    env.CategoryLLM,
	})

	// Model has no default on purpose; the endpoint knows its own models.
	Model = env.NewString(env.Spec{
		Name:        "BOXER_LLM_MODEL",
		Description: "model id the host's llm service completes with; unset means no model is offered even with the endpoint set",
		Category:    env.CategoryLLM,
	})

	// ApiKey is its own variable rather than a fallback chain over
	// provider-specific names: a sensitive value has exactly one name per
	// consumer. Empty is valid — local endpoints take no key.
	ApiKey = env.NewString(env.Spec{
		Name:        "BOXER_LLM_APIKEY",
		Description: "API key the host's llm service sends to the endpoint; empty for local endpoints that take none",
		Category:    env.CategoryLLM,
		Sensitive:   true,
	})

	// MaxTokens must cover reasoning AND answer on models that think inline.
	MaxTokens = env.NewInt(env.Spec{
		Name:        "BOXER_LLM_MAXTOKENS",
		Default:     "4096",
		Description: "completion token ceiling per llm.complete when the request names none",
		Category:    env.CategoryLLM,
	})

	// Timeout bounds one completion on the service side; the requester's
	// own wait is the client's.
	Timeout = env.NewDuration(env.Spec{
		Name:        "BOXER_LLM_TIMEOUT",
		Default:     "120s",
		Description: "wall-clock bound on one llm.complete on the service side",
		Category:    env.CategoryLLM,
	})

	// KeepMessages opts a deployment into keeping prompt and completion
	// text on the keelson('llm_calls') rows (ADR-0254 §SD4); off, the rows
	// carry sizes and counts only.
	KeepMessages = env.NewBool(env.Spec{
		Name:        "BOXER_LLM_KEEP_MESSAGES",
		Default:     "false",
		Description: "keep prompt and completion text on keelson('llm_calls') rows; off keeps sizes and token counts only",
		Category:    env.CategoryLLM,
	})
)
