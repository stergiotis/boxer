package play

// play's prompt book (ADR-0254 §SD6): explain, fix this error, ask — three
// documents in ADR-0216 §SD2's shape, registered into the shared promptbook
// mechanism under the id the Model tab reads them by. The corpus gate test
// (play_model_panel_test.go) holds them to zero parse errors.

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
)

// modelBookId is the registered id of play's prompt book.
const modelBookId = "play"

//go:embed prompts
var modelPromptsFS embed.FS

func init() {
	if err := promptbook.Register(modelBookId, help.MustSub(modelPromptsFS, "prompts")); err != nil {
		log.Warn().Err(err).Msg("play: failed to register the model prompt book")
	}
}
