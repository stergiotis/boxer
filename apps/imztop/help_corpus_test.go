package imztop

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/apptest"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/require"
)

// TestHelpCorpusIndexes guards the wiring of the inline help (apps/imztop/help
// → Manifest.Help through the embed): a broken directive or a page that fails
// to parse would drop the page from the index with nothing to say so.
func TestHelpCorpusIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok, "imztop help book must be indexed from Manifest.Help")
	require.Empty(t, b.Validate(), "help front-matter must conform")

	want := map[string]bool{"overview": false, "operations": false, "replay": false, "panels": false}
	for _, d := range b.Docs() {
		if _, expected := want[d.Path]; !expected {
			continue
		}
		want[d.Path] = true
		require.NotEmpty(t, d.Title, "doc %q: title", d.Path)
		require.NotEmpty(t, d.Type, "doc %q: type", d.Path)
		doc, _, parsed := b.Doc(d.Path)
		require.True(t, parsed, "doc %q must parse", d.Path)
		require.NotNil(t, doc)
	}
	for path, found := range want {
		require.True(t, found, "expected help doc %q", path)
	}
}

// TestHelpCorpus is the conformance kit's check of the help book
// (ADR-0396 §8): it indexes, every page parses and loses nothing in the
// lowering, and every operation and resource is named.
func TestHelpCorpus(t *testing.T) {
	apptest.HelpCorpus(t, manifest.Id)
	apptest.HelpNamesCatalog(t, manifest.Id)
}
