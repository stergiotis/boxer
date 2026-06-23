package play

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/require"
)

// TestHelpCorpusIndexes guards the wiring of the play app's inline-help corpus
// (apps/play/help/*.md → Manifest.Help via //go:embed + help.MustSub). A broken
// embed directive, a missing file, or a doc that fails to parse would silently
// drop the doc from the index; this asserts both pages are present, titled,
// typed, and parse cleanly.
func TestHelpCorpusIndexes(t *testing.T) {
	// play's init() registers the factory + manifest; sync picks it up.
	help.SyncFromRegistry()

	b, ok := help.Book("github.com/stergiotis/boxer/apps/play")
	require.True(t, ok, "play help book must be indexed from Manifest.Help")

	// The corpus must satisfy the documentation-standard front-matter
	// contract (type/status enums, operator-facing so no adr). This also
	// dogfoods help.BookI.Validate against a real shipped corpus.
	require.Empty(t, b.Validate(), "play help corpus front-matter must conform")

	want := map[string]bool{
		"overview":              false,
		"features":              false,
		"snippets":              false,
		"howto-example-queries": false,
	}
	for _, d := range b.Docs() {
		if _, expected := want[d.Path]; !expected {
			continue
		}
		want[d.Path] = true
		require.NotEmpty(t, d.Title, "doc %q: frontmatter title", d.Path)
		require.NotEmpty(t, d.Type, "doc %q: Diátaxis type", d.Path)
		doc, _, parsed := b.Doc(d.Path)
		require.True(t, parsed, "doc %q must parse", d.Path)
		require.NotNil(t, doc)
	}
	for path, found := range want {
		require.True(t, found, "expected help doc %q in the play book", path)
	}
}
