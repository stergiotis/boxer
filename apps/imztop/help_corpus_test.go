package imztop

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/assert"
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

// TestHelpCorpusDropsNothing is the zero-drops gate (ADR-0180 M2): a construct
// the markdown lowering has no case for vanishes from the rendered page
// silently, and this turns that into a failure.
func TestHelpCorpusDropsNothing(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok)
	docs := b.Docs()
	require.NotEmpty(t, docs)
	for _, info := range docs {
		doc, _, parsed := b.Doc(info.Path)
		require.Truef(t, parsed, "doc %q must parse", info.Path)
		require.Emptyf(t, doc.Dropped(), "doc %q loses content in the lowering: %+v", info.Path, doc.Dropped())
	}
}

// TestHelpNamesEveryOperation keeps the book and the catalog together: an
// operation or resource added to imztop_ops.go without a line in the help is
// one a model is told exists (keelson('helpsections')) and nothing explains.
// Names are matched in backticks, as the book spells them.
func TestHelpNamesEveryOperation(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
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
}
