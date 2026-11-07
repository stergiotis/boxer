package splashscreen

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/require"
)

// TestHelpCorpusIndexes guards the wiring of the inline help
// (apps/splashscreen/help → Manifest.Help through the rendering FS): a
// broken directive or a page that fails to parse would drop the page from
// the index with nothing to say so.
func TestHelpCorpusIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok, "splashscreen help book must be indexed from Manifest.Help")
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
// rendered page silently, and this turns that into a failure. The NOTICE
// is in the page, so this also checks that its text lowers verbatim.
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

// TestHelpOverviewCarriesTheAboutContent: the page is the About tab's
// content, read from the process when opened — the identity line the tab
// draws, the Build fields in the tab's order, and the NOTICE.
func TestHelpOverviewCarriesTheAboutContent(t *testing.T) {
	loadAssets()
	page := string(renderAbout([]byte("# About boxer\n")))
	require.Contains(t, page, manifest.Display+" · version "+manifest.Version)
	require.Contains(t, page, "## Build")
	require.Contains(t, page, "**Revision**")
	require.Contains(t, page, "**Module**")
	require.Contains(t, page, "```text\n"+strings.TrimRight(noticeText, "\n")+"\n```")
	require.Len(t, buildInfo()[0], 2)
}
