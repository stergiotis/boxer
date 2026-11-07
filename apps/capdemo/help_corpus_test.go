package capdemo

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/require"
)

// TestHelpCorpusIndexes guards the wiring of the inline help
// (apps/capdemo/help → Manifest.Help through the embed): a broken
// directive or a page that fails to parse would drop the page from the
// index with nothing to say so.
func TestHelpCorpusIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok, "capdemo help book must be indexed from Manifest.Help")
	require.Empty(t, b.Validate(), "help front-matter must conform")

	want := map[string]bool{"overview": false}
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

// TestHelpCorpusDropsNothing is the zero-drops gate (ADR-0180 M2): a
// construct the markdown lowering has no case for vanishes from the
// rendered page silently, and this turns that into a failure.
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

// TestHelpNamesTheCatalog keeps the book and the catalog together (A4):
// every operation and resource the window declares is named, in code
// spans, somewhere in the help text, so a model reading the book can find
// what it may call.
func TestHelpNamesTheCatalog(t *testing.T) {
	text := helpText(t)
	for _, op := range manifest.Operations.Operations {
		require.Containsf(t, text, "`"+op.Name+"`", "the help book names operation %s", op.Name)
	}
	for _, r := range manifest.Operations.Resources {
		require.Containsf(t, text, "`"+r.Name+"`", "the help book names resource %s", r.Name)
	}
}

// helpText is every page of the book, concatenated.
func helpText(t *testing.T) (text string) {
	t.Helper()
	var b strings.Builder
	err := fs.WalkDir(manifest.Help, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, rerr := fs.ReadFile(manifest.Help, p)
		if rerr != nil {
			return rerr
		}
		b.Write(src)
		b.WriteByte('\n')
		return nil
	})
	require.NoError(t, err)
	return b.String()
}
