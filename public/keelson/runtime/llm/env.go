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

	// Timeout bounds one completion on the service side. A client that
	// names no wait of its own learns it from llm.describe and waits that
	// long plus ReplyMargin, so this one value governs the apps too.
	Timeout = env.NewDuration(env.Spec{
		Name:        "BOXER_LLM_TIMEOUT",
		Default:     "120s",
		Description: "wall-clock bound on one llm.complete; the service cancels the provider call at it, and app clients that name no timeout of their own wait that long plus a few seconds for the reply",
		Category:    env.CategoryLLM,
	})

	// TrustedHosts are endpoint hosts the deployment declares under its own
	// control: the sensitivity wall (ADR-0254 §SD3) treats them as it treats
	// loopback, so confined content may reach them.
	TrustedHosts = env.NewString(env.Spec{
		Name:        "BOXER_LLM_TRUSTED_HOSTS",
		Description: "comma-separated endpoint host names or IPs, without port, that the llm service treats like loopback: confined (sealed) content may be sent to them; list only machines under your own control, and prefer an https endpoint",
		Category:    env.CategoryLLM,
	})

	// ContextTokens is the model's context size as the deployment states
	// it; unset, the service asks the endpoint's model list once.
	ContextTokens = env.NewInt(env.Spec{
		Name:        "BOXER_LLM_CONTEXT_TOKENS",
		Default:     "0",
		Description: "the model's context size in tokens, reported by llm.describe so a chat can say how full its conversation is; 0 asks the endpoint's model list once at start (context_length, max_model_len and the like), and a server that reports none leaves it unknown",
		Category:    env.CategoryLLM,
	})

	// Retain is the deployment's ceiling on keeping message text (ADR-0264
	// §SD1, replacing ADR-0254's BOXER_LLM_KEEP_MESSAGES): off keeps sizes
	// and counts only; ring keeps prompt and completion text on this
	// process's keelson('llm_calls') rows for every call; durable does that
	// and also keeps the messages of retained requests on boxer.facts.
	Retain = env.NewCategorialString(env.Spec{
		Name:        "BOXER_LLM_RETAIN",
		Default:     string(RetainOff),
		Description: "ceiling on keeping model message text: off (sizes and counts only), ring (text on this process's keelson('llm_calls') rows), durable (ring, plus the messages of llm.retain.* requests on boxer.facts, kept until removed by hand)",
		Category:    env.CategoryLLM,
	}, []string{string(RetainOff), string(RetainRing), string(RetainDurable)})
)

// RetainE is a BOXER_LLM_RETAIN level, ordered: each keeps what the one
// before it does.
type RetainE string

const (
	RetainOff     RetainE = "off"
	RetainRing    RetainE = "ring"
	RetainDurable RetainE = "durable"
)

// atLeast says level keeps what other keeps.
func (inst RetainE) atLeast(other RetainE) (yes bool) {
	return inst.rank() >= other.rank()
}

func (inst RetainE) rank() (r int) {
	switch inst {
	case RetainRing:
		return 1
	case RetainDurable:
		return 2
	default:
		return 0
	}
}
