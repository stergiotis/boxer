// Package help is the runtime's inline-help index. Apps that ship
// Markdown documentation populate [app.Manifest.Help] with an fs.FS
// (typically an embed.FS rooted at the app's `help/` directory); the
// [DefaultLibrary] lazily builds a [BookI] per app on first access.
//
// The package is intentionally render-agnostic: it owns parsing and
// indexing (frontmatter, headings, doc paths) and hands the parsed
// [markdown.Doc] back to the consumer. A help-reader app, a tooltip
// popup, or a CLI exporter all consume the same BookI surface.
//
// The library auto-syncs against [app.DefaultRegistry] on first use,
// so apps that already register against the runtime get help indexing
// for free as soon as Manifest.Help is populated. Explicit
// [Register] / [SyncFromRegistry] hooks exist for tests and special
// wiring.
//
// What this package does NOT do (deferred to follow-up rounds):
//
//   - Open help over the bus. [RefT] is the typed payload a future
//     `runtime.help.open` cap subject will carry; the bus subscription
//     and a HelpHost app land separately.
//   - Resolve cross-app wikilinks. The markdown widget's
//     [resolver.ResolverI] hook is where `[[appid/doc#section]]` will
//     be lowered to RefT, also in a follow-up round.
//   - Full-text search. The current index is title + heading + path;
//     a body-text index is out of scope for M0.
package help

import (
	"io/fs"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// RefT references one help location: a book (selected by [app.AppIdT]),
// a document inside that book (FS-relative path minus the .md suffix,
// e.g. `"overview"` or `"howto/replay"`), and an optional heading slug
// inside that document (matches [markdown.SlugHeading]).
//
// RefT is a plain value: it will round-trip through CBOR on the bus
// (once the bus subject lands) and across Go function boundaries today.
// The String form is for debug and log output, not a wire format.
type RefT struct {
	AppId   app.AppIdT
	Doc     string
	Section string
}

// String returns a human-readable rendering of the ref for logs and
// error messages: `"<AppId>/<Doc>[#<Section>]"`. Not a parseable
// canonical form — RefT travels as a struct, not a URL, until the bus
// subject lands.
func (inst RefT) String() (s string) {
	s = string(inst.AppId) + "/" + inst.Doc
	if inst.Section != "" {
		s += "#" + inst.Section
	}
	return
}

// IsZero reports whether the ref is the zero value. Useful for "no
// target" guards in tooltip-style consumers.
func (inst RefT) IsZero() (zero bool) {
	zero = inst.AppId == "" && inst.Doc == "" && inst.Section == ""
	return
}

// Book is a package-level shortcut for [DefaultLibrary.Book].
func Book(id app.AppIdT) (b BookI, ok bool) {
	b, ok = DefaultLibrary.Book(id)
	return
}

// Books is a package-level shortcut for [DefaultLibrary.Books].
func Books() (books []BookI) {
	books = DefaultLibrary.Books()
	return
}

// Register is a package-level shortcut for [DefaultLibrary.Register].
func Register(b BookI) (err error) {
	err = DefaultLibrary.Register(b)
	return
}

// SyncFromRegistry is a package-level shortcut for
// [DefaultLibrary.SyncFromRegistry].
func SyncFromRegistry() (added int) {
	added = DefaultLibrary.SyncFromRegistry()
	return
}

// MustSub returns the [fs.Sub] view of fsys rooted at dir. Panics
// when the lookup fails, which surfaces a misaligned `//go:embed`
// directive at init() time instead of letting [BookI] silently index
// an empty corpus. The canonical use site is one line in the app's
// manifest declaration:
//
//	//go:embed help
//	var helpFS embed.FS
//
//	var manifest = app.Manifest{
//	    ...
//	    Help: help.MustSub(helpFS, "help"),
//	}
//
// The named subdirectory is what the embed directive listed; with
// the `//go:embed help` form above, that's `"help"`. Apps that ship
// docs from a differently-named directory (e.g., `//go:embed manual`)
// pass that name instead.
func MustSub(fsys fs.FS, dir string) (sub fs.FS) {
	var err error
	sub, err = fs.Sub(fsys, dir)
	if err != nil {
		panic("help.MustSub(" + dir + "): " + err.Error())
	}
	return
}
