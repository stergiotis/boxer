//go:build llm_generated_opus47

package openaichat

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LoadGeminiApiKey resolves the Google AI Studio / Gemini API key from a
// fixed precedence chain. Most callers thread it behind a CLI flag whose
// non-empty value short-circuits the lookup.
//
// Precedence (first non-empty wins):
//  1. GEMINI_API_KEY env var
//  2. GOOGLE_API_KEY env var
//  3. ~/.config/gemini/api_key (single-line plaintext)
//
// The file fallback follows the same convention the gemini CLI uses; the
// trailing newline (if any) is stripped.
func LoadGeminiApiKey() (key string, err error) {
	key = os.Getenv("GEMINI_API_KEY")
	if key != "" {
		return
	}
	key = os.Getenv("GOOGLE_API_KEY")
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
			Errorf("gemini api key not found in env (GEMINI_API_KEY / GOOGLE_API_KEY) nor in file: %w", err)
		return
	}
	key = strings.TrimSpace(string(raw))
	if key == "" {
		err = eb.Build().Str("path", keyPath).Errorf("gemini api key file is empty")
		return
	}
	return
}
