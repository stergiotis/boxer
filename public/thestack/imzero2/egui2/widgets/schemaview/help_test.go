package schemaview_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
)

// TestHelpBook guards the embedded help corpus: it must be present, index the
// glyph reference, and conform to the documentation-standard front-matter that
// the runtime help library validates. Mirrors the "Schema inspector" book the
// carousel integration layer registers over the same fs.FS.
func TestHelpBook(t *testing.T) {
	b, err := help.NewBook(schemaHelpBookId, help.MustSub(schemaview.HelpFS, "help"))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	docs := b.Docs()
	if len(docs) == 0 {
		t.Fatalf("Docs(): empty — help corpus not embedded or not indexed")
	}
	if problems := b.Validate(); len(problems) != 0 {
		t.Fatalf("Validate(): front-matter problems: %+v", problems)
	}
	if _, _, ok := b.Doc("glyphs"); !ok {
		t.Errorf("Doc(%q): not indexed; indexed docs = %+v", "glyphs", docs)
	}
}

// schemaHelpBookId mirrors the carousel layer's book id; kept local so the
// widget test does not import the demo package.
const schemaHelpBookId = "Schema inspector"
