package commitdigest

import "github.com/stergiotis/boxer/public/config/env"

// LLMAPIKey is the API key the summarize and synthesize-threads commands send
// to a non-Gemini provider; the --llm-apikey flag derives from it (ADR-0009).
var LLMAPIKey = env.NewString(env.Spec{
	Name:        "LLM_API_KEY",
	Description: "API key for non-Gemini LLM providers (LM Studio, generic OpenAI-compatible); prefer the variable to --llm-apikey, which exposes the secret in process argv",
	Category:    env.CategoryDev,
	Sensitive:   true,
	CliFlagName: "llm-apikey",
})
