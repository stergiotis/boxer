package mdedit

// A snapshot document that is a sealed notebook (ADR-0371) is drawn as one
// in the reading mode: its stored results as tables and panels, its figures
// checked against what is stored. The other modes show its text, which is
// where it is edited.
//
// The blobs a notebook names lie beside it in the snapshot, the place a
// snapshot document's images are already read from. A document from the
// persist store, the clipboard or a single file has no such place, and is
// drawn as markdown: its `sql` fences as code.
//
// Reading the blobs runs on a lane, as the images do (mdedit_images.go);
// the view is handed the result on the render goroutine.

import (
	"context"
	"io/fs"

	"github.com/stergiotis/boxer/apps/mdedit/mdeditlink"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// notebookState is the reading mode's notebook, if the document is one.
type notebookState struct {
	view *notebookview.View
	// job prepares the notebook for a key: the snapshot document and its
	// text. It sets no disposer — a prepared notebook holds memory, nothing
	// to close — so the view owns the one it is handed.
	job bgjob.Keyed[*notebookview.Prepared]

	// src is the buffer the fields below were decided for.
	src   string
	srcOk bool
	// nb is the buffer read as a notebook, nil when it is not one.
	nb  *mdnotebook.Notebook
	key string
	// given is what the view was last handed.
	given *notebookview.Prepared
}

// isNotebook reports a document that holds a record: one its frontmatter
// lists, or one written as a cell and waiting to be collected. A record is
// what a notebook is of. A `sql` fence with a second word is a notebook's
// presentation cell and an applet document's panel alike (apps/sqlapplet),
// and a recording alone on its line is an embed in any markdown, so neither
// says which of the two a document is; without a record a presentation cell
// has nothing to read, and the document is drawn as the markdown it also is.
func isNotebook(nb *mdnotebook.Notebook) bool {
	if len(nb.Records) > 0 {
		return true
	}
	for i := range nb.Cells {
		if nb.Cells[i].Kind == mdnotebook.CellKindRecord {
			return true
		}
	}
	return false
}

// resetNotebook drops the notebook with the document it belonged to.
func (inst *App) resetNotebook() {
	n := &inst.notebook
	n.job.Close()
	if n.view != nil {
		n.view.Set(nil)
	}
	n.src, n.srcOk, n.nb, n.key, n.given = "", false, nil, "", nil
}

// renderNotebook draws the document as a notebook and reports that it did.
// It declines — and the caller draws markdown — outside the reading mode,
// for a document that is not from a snapshot, and for one that is not a
// notebook.
func (inst *App) renderNotebook() (drawn bool) {
	s := inst.snapImages
	if inst.viewMode != viewRead || s == nil {
		return false
	}
	n := &inst.notebook
	if !n.srcOk || n.src != inst.src {
		n.src, n.srcOk, n.nb, n.key = inst.src, true, nil, ""
		if nb, err := mdnotebook.Read([]byte(inst.src)); err == nil && isNotebook(nb) {
			n.nb = nb
			n.key = s.key + "\x00" + mdnotebook.BlobHash([]byte(inst.src))
		}
	}
	if n.nb == nil {
		return false
	}
	nb := n.nb
	prepared, done, err, busy := n.job.Demand(n.key, func(context.Context) (*notebookview.Prepared, error) {
		dir, sErr := fs.Sub(s.fsys, s.dir)
		if sErr != nil {
			return nil, sErr
		}
		return notebookview.Prepare(nb, dir), nil
	})
	reading := busy || !done
	switch {
	case reading:
		c.RequestRepaint()
		if n.given == nil {
			c.Label("Reading the notebook's stored results…").Send()
			return true
		}
		// The buffer changed under a notebook that is drawn — a task box
		// ticked. What was read last stays on screen until the lane has
		// read the new text: a frame of one line in its place would cost
		// the reader their place in the scroll area.
	case err != nil || prepared == nil:
		// The snapshot could not be opened at the document's directory:
		// the text is still the document.
		return false
	default:
		if n.view == nil {
			n.view = notebookview.New(inst.ids, "notebook", notebookview.Options{
				Statements: notebookview.StatementsHidden,
				LinkClaims: mdeditlink.Claimed,
			})
		}
		if n.given != prepared {
			n.view.Set(prepared)
			n.given = prepared
		}
	}
	if slug, changed := inst.takeScrollTarget(); changed {
		n.view.ScrollToSection(slug)
	}
	if off, ok := inst.takeScrollOffset(); ok {
		n.view.ScrollToOffset(off)
	}
	ev := n.view.Render()
	for _, l := range ev.Links {
		inst.followLink(l.URL)
	}
	for _, t := range ev.Tasks {
		if reading {
			// The offsets are of the text the view was read from, not of
			// the buffer.
			break
		}
		if next, ok := toggleTaskAt(inst.src, t.Offset); ok {
			inst.rebindBuffer(next)
		}
	}
	for _, v := range ev.Vals {
		inst.status = "the figure " + v.Label + " is " + v.Column + " of the record " + v.Record
	}
	return true
}
