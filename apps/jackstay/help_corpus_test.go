package jackstay

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/apptest"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelpCorpusIndexes guards the wiring of the inline help
// (apps/jackstay/help → Manifest.Help through the embed): a broken directive
// or a page that fails to parse would drop the page from the index with
// nothing to say so.
func TestHelpCorpusIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(AppId)
	require.True(t, ok, "jackstay help book must be indexed from Manifest.Help")
	require.Empty(t, b.Validate(), "help front-matter must conform")

	want := map[string]bool{"overview": false, "phases": false}
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
	apptest.HelpCorpus(t, AppId)
	apptest.HelpNamesCatalog(t, AppId)
}

// TestHelpNamesEveryOperationAndPhase keeps the book, the catalog and the
// machine together: an operation, a resource or a phase added without a line
// in the help is one a model is told exists and nothing explains. Names are
// matched in backticks, as the book spells them.
func TestHelpNamesEveryOperationAndPhase(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(AppId)
	require.True(t, ok)
	var corpus strings.Builder
	for _, d := range b.Docs() {
		src, found := b.Source(d.Path)
		require.True(t, found, d.Path)
		corpus.Write(src)
		corpus.WriteByte('\n')
	}
	text := corpus.String()
	require.NotNil(t, manifest.Operations)
	for _, op := range manifest.Operations.Operations {
		assert.Containsf(t, text, "`"+op.Name+"`", "operation %q is not named in the help book", op.Name)
	}
	for _, r := range manifest.Operations.Resources {
		assert.Containsf(t, text, "`"+r.Name+"`", "resource %q is not named in the help book", r.Name)
	}
	for _, ph := range allPhases {
		assert.Containsf(t, text, "`"+ph.String()+"`", "phase %q is not named in the help book", ph)
	}
}
