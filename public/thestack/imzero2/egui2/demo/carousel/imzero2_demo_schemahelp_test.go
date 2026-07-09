package demo

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// TestSchemaHelpRegistered verifies the package init() wired the
// schema-inspector glyph reference into the runtime help library, so the Help
// app surfaces it. Mirrors the video-output book's implicit contract.
func TestSchemaHelpRegistered(t *testing.T) {
	b, ok := help.Book(schemaHelpAppId)
	if !ok {
		t.Fatalf("help.Book(%q): not registered by init()", schemaHelpAppId)
	}
	if _, _, ok := b.Doc("glyphs"); !ok {
		t.Errorf("book %q: glyphs doc not indexed", schemaHelpAppId)
	}
	if problems := b.Validate(); len(problems) != 0 {
		t.Errorf("book %q: front-matter problems: %+v", schemaHelpAppId, problems)
	}
}
