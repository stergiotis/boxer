package transform

// The in-tree prompt corpus, embedded whole. help.MustSub panics at init if
// the embed directive and the directory name disagree — the guard against
// silently registering an empty corpus (the sqlapplet book shape). The
// corpus gate test (transform_corpus_test.go) is what holds the documents
// themselves to zero parse errors.

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

//go:embed prompts
var promptsFS embed.FS

func init() {
	if err := RegisterPromptBook("mdedit", help.MustSub(promptsFS, "prompts")); err != nil {
		log.Warn().Err(err).Msg("transform: failed to register the starter prompt book")
	}
}
