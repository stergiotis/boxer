// Package workstatelets is the in-tree book of workstatelet documents
// (ADR-0369): each markdown file under book/ is one document, and
// a shell that links this package and mints the registered books lists each
// one in its launcher.
//
// A document's launch arguments are base64 of a launch config's bytes, which
// a reader of the file cannot check. The package's test decodes every
// committed config and compares it to the value written out in Go there, so
// what a document opens is stated somewhere a review can read.
package workstatelets

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/workstatelet"
)

// BookId names this package's book in keelson('workstatelets').
const BookId = "workstatelets"

//go:embed book
var bookFS embed.FS

func init() {
	if err := workstatelet.RegisterBook(BookId, help.MustSub(bookFS, "book"), []app.TopicT{app.TopicRuntime}); err != nil {
		log.Warn().Err(err).Msg("workstatelets: failed to register the book")
	}
}
