package openaichat

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// GeminiApiKey is the GEMINI_API_KEY env-var spec (Google AI Studio / Gemini).
// Marked Sensitive so the doc generator and `env list` subcommand redact the
// live value.
var GeminiApiKey = env.NewString(env.Spec{
	Name:        "GEMINI_API_KEY",
	Description: "Google AI Studio / Gemini API key",
	Category:    env.CategoryLLM,
	Sensitive:   true,
})

// LoadGeminiApiKey resolves the Google AI Studio / Gemini API key from a
// fixed precedence chain. Most callers thread it behind a CLI flag whose
// non-empty value short-circuits the lookup.
//
// Precedence (first non-empty wins):
//  1. GEMINI_API_KEY env var
//  2. ~/.config/gemini/api_key (single-line plaintext)
//
// The file fallback follows the same convention the gemini CLI uses; the
// trailing newline (if any) is stripped.
func LoadGeminiApiKey() (key string, err error) {
	key = GeminiApiKey.Get()
	if key != "" {
		return
	}

	var homeDir string
	homeDir, err = os.UserHomeDir()
	if err != nil {
		err = eh.Errorf("resolve home dir: %w", err)
		return
	}
	keyPath := filepath.Join(homeDir, ".config", "gemini", "api_key")
	var raw []byte
	raw, err = os.ReadFile(keyPath)
	if err != nil {
		err = eb.Build().
			Str("path", keyPath).
			Errorf("gemini api key not found in env (GEMINI_API_KEY) nor in file: %w", err)
		return
	}
	key = strings.TrimSpace(string(raw))
	if key == "" {
		err = eb.Build().Str("path", keyPath).Errorf("gemini api key file is empty")
		return
	}
	return
}
