package transform

// The ADR-0009 env registry entries — mdedit's first. The endpoint is the
// registration GATE, the shape ADR-0120 §SD3 records for play's Ask panel:
// with it unset the transform surface does not render at all, so a host that
// never configured an LLM carries no dead control and probes nothing.

import (
	"github.com/stergiotis/boxer/public/config/env"
)

var (
	// Endpoint gates the whole feature: unset (or an unset Model) means the
	// transform surface is absent from the bar.
	Endpoint = env.NewString(env.Spec{
		Name:        "BOXER_MDEDIT_LLM_ENDPOINT",
		Description: "OpenAI-compatible chat-completions base URL for mdedit's text transformations (e.g. http://localhost:1234/v1); unset hides the transform surface entirely",
		Category:    env.CategoryE("boxer-mdedit"),
	})

	// Model has no default on purpose: a wrong default is worse than a
	// refusal, and the endpoint knows its own models.
	Model = env.NewString(env.Spec{
		Name:        "BOXER_MDEDIT_LLM_MODEL",
		Description: "model id for mdedit's text transformations; unset hides the transform surface even with the endpoint set",
		Category:    env.CategoryE("boxer-mdedit"),
	})

	// ApiKey is its own variable rather than a fallback chain over
	// LLM_API_KEY (a commitdigest CLI flag alias, not a registered spec) or
	// GEMINI_API_KEY (provider-specific): a sensitive value should have
	// exactly one name per consumer. Empty is valid — local endpoints
	// (LM Studio, llama.cpp, Ollama) take no key.
	ApiKey = env.NewString(env.Spec{
		Name:        "BOXER_MDEDIT_LLM_APIKEY",
		Description: "API key sent to the transformation endpoint; empty for local endpoints that take none",
		Category:    env.CategoryE("boxer-mdedit"),
		Sensitive:   true,
	})

	// MaxTokens must cover reasoning AND answer on models that think inline.
	MaxTokens = env.NewInt(env.Spec{
		Name:        "BOXER_MDEDIT_LLM_MAXTOKENS",
		Default:     "4096",
		Description: "completion token ceiling per transformation run; a prompt doc's own max-tokens frontmatter wins",
		Category:    env.CategoryE("boxer-mdedit"),
	})

	// Timeout is generous because local models are slow; the run is
	// cancellable from the progress row throughout.
	Timeout = env.NewDuration(env.Spec{
		Name:        "BOXER_MDEDIT_LLM_TIMEOUT",
		Default:     "120s",
		Description: "wall-clock bound on one transformation run; the client has no timeout of its own",
		Category:    env.CategoryE("boxer-mdedit"),
	})
)
