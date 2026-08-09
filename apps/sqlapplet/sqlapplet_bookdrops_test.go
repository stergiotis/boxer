package sqlapplet

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// TestBookCorporaDropNothing is the ADR-0180 M2 zero-drops gate over every
// applet book this app ships.
//
// The markdown lowering's failure mode for a construct it has no case for is
// INVISIBLE: the node reaches a default branch and is dropped together with
// the prose it covered, so a page reads short and nothing says why. That is
// how a parser feature once deleted the text it covered.
// [markdown.Doc.Dropped] counts every skip by AST kind, and asserting zero
// over the shipped corpus turns the failure mode into a property a test can
// hold.
//
// It matters more here than in an ordinary help book: these documents are
// authored as data — an applet's prose IS its documentation drawer — and there
// are more of them than anyone re-reads by hand.
//
// The list is enumerated rather than discovered because the embed FSes are
// package-level vars with no registry behind them; a book added to
// sqlapplet_register.go without a line here is a gap, which is what the
// count assertion at the end is for.
func TestBookCorporaDropNothing(t *testing.T) {
	books := []struct {
		id   string
		fsys embed.FS
	}{
		{"book", bookFS},
		{"booktopo", booktopoFS},
		{"bookgodep", bookgodepFS},
		{"bookpprof", bookpprofFS},
		{"bookcapmap", bookcapmapFS},
		{"bookcoverage", bookcoverageFS},
		{"bookjsonbench", bookjsonbenchFS},
		{"bookcodevol", bookcodevolFS},
		{"bookcatalog", bookcatalogFS},
		{"bookadr", bookadrFS},
	}
	seen := 0
	for _, bk := range books {
		t.Run(bk.id, func(t *testing.T) {
			b, err := help.NewBook(app.AppIdT(appletIdPrefix+"book/"+bk.id), help.MustSub(bk.fsys, bk.id))
			require.NoError(t, err)
			docs := b.Docs()
			require.NotEmpty(t, docs, "book %q must not be empty", bk.id)
			for _, info := range docs {
				doc, _, parsed := b.Doc(info.Path)
				require.Truef(t, parsed, "%s/%s must parse", bk.id, info.Path)
				require.Emptyf(t, doc.Dropped(), "%s/%s loses content in the lowering: %+v",
					bk.id, info.Path, doc.Dropped())
			}
		})
		seen += len(mustDocPaths(t, bk.id, bk.fsys))
	}
	// A floor, not an equality: adding an applet must not have to touch this
	// test, but silently losing most of the corpus to a broken embed must.
	require.GreaterOrEqual(t, seen, 30, "the shipped applet corpus shrank unexpectedly")
}

func mustDocPaths(t *testing.T, id string, fsys embed.FS) (paths []string) {
	t.Helper()
	b, err := help.NewBook(app.AppIdT(appletIdPrefix+"book/"+id), help.MustSub(fsys, id))
	require.NoError(t, err)
	for _, info := range b.Docs() {
		paths = append(paths, info.Path)
	}
	return
}
