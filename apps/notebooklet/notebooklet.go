// Package notebooklet mints a committed notebook as a launcher entry
// (ADR-0380 §SD6): one notebook, one app of kind notebooklet, whose window
// is the notebook read live and nothing else — no Records face, no
// collection, no store. The records are computed on the server when the
// window opens, read-only, and shown as a bound notebook (package notebook's
// pane); nothing is stored.
//
// Only committed books are read: a reviewed corpus is what stands behind a
// launcher entry, and the corpus test requires that every notebook of a
// registered book classifies read. The window offers the document to the
// notebook window, where it is worked on and collected.
package notebooklet

import (
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

// ErrNotANotebook is a document of a book that holds no record: it is
// passed over, not minted.
var ErrNotANotebook = errors.New("the document holds no record, so it is not a notebook")

// idPrefix makes a slug into an app id; a slug is public once it ships.
const idPrefix = "github.com/stergiotis/boxer/apps/notebooklet/"

// Def is one notebook of a registered book and the manifest it mints.
type Def struct {
	Manifest app.Manifest
	// Book is the book's id, Path the document's path in it and Slug the
	// path's stem: the second half of the app id.
	Book string
	Path string
	Slug string
	// Text is the notebook's markdown; Files is the document's directory in
	// the book, where its stored results, recordings and images are.
	Text  []byte
	Files fs.FS
}

// Slug is the slug a minted app id carries, empty for an id this package
// did not mint.
func Slug(id app.AppIdT) (slug string) {
	slug, found := strings.CutPrefix(string(id), idPrefix)
	if !found {
		return ""
	}
	return
}

// ParseDocSource reads one document of a book into the notebooklet it
// mints. A document that is not a notebook — one that holds no record — is
// refused; so is a path whose stem is not a slug. topics classifies it.
// Whether its records classify read is the pane's to decide when the window
// opens, and the corpus test's to keep off main.
func ParseDocSource(bookID string, p string, src []byte, fsys fs.FS, topics []app.TopicT) (def *Def, err error) {
	slug := strings.TrimSuffix(path.Base(p), path.Ext(p))
	if !validSlug(slug) {
		return nil, eb.Build().Str("bookID", bookID).Str("path", p).Errorf("notebooklet: a document's file name is its slug: letters, digits, '-' and '_'")
	}
	nb, err := mdnotebook.Read(src)
	if err != nil {
		return nil, eb.Build().Str("bookID", bookID).Str("path", p).Errorf("notebooklet: read the notebook: %w", err)
	}
	if !holdsRecord(nb) {
		return nil, eb.Build().Str("bookID", bookID).Str("path", p).Errorf("notebooklet: %w", ErrNotANotebook)
	}
	files := fsys
	if dir := path.Dir(p); dir != "." {
		if files, err = fs.Sub(fsys, dir); err != nil {
			return nil, eb.Build().Str("bookID", bookID).Str("path", p).Errorf("notebooklet: the document's directory: %w", err)
		}
	}
	title := nb.Title
	if title == "" {
		title = slug
	}
	def = &Def{Book: bookID, Path: p, Slug: slug, Text: src, Files: files}
	def.Manifest = app.Manifest{
		Id:      app.AppIdT(idPrefix + slug),
		Version: "0.1.0",
		Display: title,
		Title:   title,
		Summary: "The notebook " + title + ", its records computed on the server when it opens",
		Icon:    icons.PhNotebook,
		Topics:  topics,
		Kind:    app.KindNotebooklet,
		// Its grade is the document's, which nobody has graded
		// (ADR-0375 §SD6).
		Maturity:   app.MaturityUnstated,
		Keywords:   []string{"notebook", "notebooklet", "live", "report", slug},
		Surface:    app.SurfaceWindowed,
		Operations: ops.Catalog(),
		Help:       help.MustSub(helpFS, "help"),
		Caps:       Caps(),
		SurfaceHints: app.SurfaceHints{
			PreferredWidth:  1100,
			PreferredHeight: 860,
		},
	}
	return def, nil
}

// Caps is what a minted notebooklet declares: what its pane's run needs
// (notebook.EmbedCaps), and opening the notebook window on its document.
// A document widens none of them.
func Caps() (caps []app.SubjectFilter) {
	return append(notebook.EmbedCaps(), app.SubjectFilter{Pattern: windowhost.OpenSubject, Direction: app.CapDirectionPub,
		Reason: "notebooklet: Open in notebook — open the notebook window on this document, where it is worked on and collected"})
}

func holdsRecord(nb *mdnotebook.Notebook) bool {
	for i := range nb.Cells {
		if nb.Cells[i].Kind == mdnotebook.CellKindRecord {
			return true
		}
	}
	return false
}

// validSlug is the rule an embedded pane's key follows, since the slug is
// the key.
func validSlug(slug string) bool {
	if slug == "" || len(slug) > 64 {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
