package writingstylescope

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/apptest"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/require"
)

// TestHelpCorpusIndexes guards the wiring of the inline help (apps/writingstylescope/help
// → Manifest.Help through the embed): a broken directive or a page that fails
// to parse would drop the page from the index with nothing to say so.
func TestHelpCorpusIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok, "writingstylescope help book must be indexed from Manifest.Help")
	require.Empty(t, b.Validate(), "help front-matter must conform")

	want := map[string]bool{"overview": false, "method": false, "operations": false}
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
// (ADR-0396 §8): it indexes, and every page parses and loses nothing in the
// lowering.
func TestHelpCorpus(t *testing.T) {
	apptest.HelpCorpus(t, manifest.Id)
}

// TestHelpBookNamesEveryOperation keeps the operations topic in step with
// the catalog (keelson-app A4): an operation added to writingstylescope_ops.go
// without a line in the book is one a model has to guess at.
func TestHelpBookNamesEveryOperation(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok)
	var corpus strings.Builder
	for _, d := range b.Docs() {
		src, found := b.Source(d.Path)
		require.True(t, found, d.Path)
		corpus.Write(src)
	}
	catalog := ops.Catalog()
	require.NotEmpty(t, catalog.Operations)
	for _, spec := range catalog.Operations {
		require.Containsf(t, corpus.String(), spec.Name, "operation %q is not named in the help book", spec.Name)
	}
	for _, r := range catalog.Resources {
		require.Containsf(t, corpus.String(), "`"+r.Name+"`", "resource %q is not named in the help book", r.Name)
	}
}
