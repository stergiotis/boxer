package notebook

import (
	"io/fs"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// TestHelpCorpusIndexes guards the wiring of the inline help (apps/notebook/
// help → Manifest.Help through the embed): a broken directive or a page that
// fails to parse would drop the page from the index with nothing to say so.
func TestHelpCorpusIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok, "the notebook help book must be indexed from Manifest.Help")
	require.Empty(t, b.Validate(), "help front-matter must conform")
	docs := b.Docs()
	require.NotEmpty(t, docs)
	found := false
	for _, info := range docs {
		found = found || info.Path == "overview"
		doc, _, parsed := b.Doc(info.Path)
		require.Truef(t, parsed, "doc %q must parse", info.Path)
		// The zero-drops gate (ADR-0180 M2).
		require.Emptyf(t, doc.Dropped(), "doc %q loses content in the lowering: %+v", info.Path, doc.Dropped())
	}
	require.True(t, found, "the book has an overview")
}

// helpOperationRow matches a table row whose first cell is an operation
// name in backticks.
var helpOperationRow = regexp.MustCompile("(?m)^\\| `([a-z][a-z0-9_]*)` \\|")

// The overview's table and the catalog name the same operations.
func TestHelpNamesEveryOperation(t *testing.T) {
	raw, err := fs.ReadFile(helpFS, "help/overview.md")
	require.NoError(t, err)
	documented := map[string]int{}
	for _, m := range helpOperationRow.FindAllStringSubmatch(string(raw), -1) {
		documented[m[1]]++
	}
	for _, name := range []string{opGetDocument, opCheckNotebook, opSetRecords, opShowFace, opOpenSample, opOpenSnapshot, opCollectNotebook} {
		assert.Equalf(t, 1, documented[name], "the operation %s has one row in the overview", name)
		_, declared := manifest.Operations.Lookup(name)
		assert.Truef(t, declared, "the catalog declares %s", name)
		delete(documented, name)
	}
	assert.Empty(t, documented, "the overview lists an operation the catalog does not have")
}
