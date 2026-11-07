package sqlapplet

// A minted applet's help book (keelson-app A4) is its contributing book's
// documents, which are the applets' own pages, plus one shared topic the
// minter injects: what the window's catalog serves (sqlapplet_app_ops.go),
// what it leaves out and why, and how a bundle binds datasets. The topic
// lives outside every book directory because a book's files are parsed as
// applet documents (ParseBook), and a page without a sql fence would fail
// that parse; it is joined to the book at Help only, so the applet parser
// never sees it.

import (
	"embed"
	"errors"
	"io/fs"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

//go:embed help
var appletHelpFS embed.FS

// appletHelpDoc is the shared topic's document path, as the help library
// indexes it.
const appletHelpDoc = "agent-operations"

// appletHelp is the Help FS of a minted manifest: the book's documents
// beside the shared topic. A stored applet has no book and gets the topic
// alone.
func appletHelp(bookFsys fs.FS) (fsys fs.FS) {
	shared := help.MustSub(appletHelpFS, "help")
	if bookFsys == nil {
		return shared
	}
	return unionFS{primary: bookFsys, secondary: shared}
}

// unionFS reads primary first and secondary for what primary lacks; a
// directory listing is both, by name, with primary's entry winning.
type unionFS struct {
	primary   fs.FS
	secondary fs.FS
}

var (
	_ fs.FS        = unionFS{}
	_ fs.ReadDirFS = unionFS{}
)

func (u unionFS) Open(name string) (f fs.File, err error) {
	f, err = u.primary.Open(name)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return
	}
	return u.secondary.Open(name)
}

func (u unionFS) ReadDir(name string) (entries []fs.DirEntry, err error) {
	first, perr := fs.ReadDir(u.primary, name)
	second, serr := fs.ReadDir(u.secondary, name)
	switch {
	case perr != nil && !errors.Is(perr, fs.ErrNotExist):
		return nil, perr
	case serr != nil && !errors.Is(serr, fs.ErrNotExist):
		return nil, serr
	case perr != nil && serr != nil:
		return nil, perr
	}
	entries = slices.Clone(first)
	for _, e := range second {
		if !slices.ContainsFunc(entries, func(have fs.DirEntry) bool { return have.Name() == e.Name() }) {
			entries = append(entries, e)
		}
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return
}
