package notebook

// Hand-overs between this window and mdedit (ADR-0371 §SD10). A notebook's
// prose and presentation cells are written in an editor, and this window
// has none: it hands the text to mdedit by mdedit's launch config, with the
// snapshot the document is of, and takes a text the same way by its own
// (launchcfg). Each hand-over opens a window; neither window follows the
// other afterwards.
//
// Where the host registers an editor app, the toolbar offers that instead
// (ADR-0365), and its saves come back into this window's text.

import (
	"context"
	"io/fs"
	"path"
	"time"

	mdlaunch "github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/apps/notebook/launchcfg"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// emptyFS is the directory of a notebook that has none: every file it
// names is missing, so nothing it stores is found.
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// handOver opens the notebook a launch config carries. A config that does
// not decode fails the mount: the host validated the claimed kind at the
// boundary, so the bytes are a defect, and a plain window in their place
// would hide it (ADR-0135 §SD4).
func (inst *App) handOver(raw []byte) (handed bool, err error) {
	if len(raw) == 0 {
		return false, nil
	}
	l, err := buscodec.Decode[launchcfg.NotebookLaunch](raw)
	if err != nil {
		return false, eh.Errorf("notebook: decode launch config: %w", err)
	}
	name := l.Name
	if name == "" && l.Path != "" {
		name = path.Base(l.Path)
	}
	if name == "" {
		name = "notebook.md"
	}
	switch {
	case l.Mount != 0 && l.Path != "":
		// The snapshot is read first: the stored results are beside it. A
		// text that came along replaces the snapshot's when it lands.
		inst.opening = &snapshotRef{mount: identifier.TaggedId(l.Mount), snap: time.Unix(0, l.SnapNanos), path: path.Clean(l.Path)}
		inst.handed, inst.handedName = l.Text, name
	case l.Text != "":
		inst.setDocument(&document{name: name, origin: "handed over as " + name, text: []byte(l.Text), fsys: emptyFS{}})
	default:
		return false, nil
	}
	return true, nil
}

const (
	labelEditInMdedit = "Edit in mdedit"
	tipEditInMdedit   = "Open mdedit on the notebook's text, with the snapshot it is of. Write prose and presentation cells there; Open in notebook brings the text back to a notebook window."
)

// renderEditInMdedit draws the hand-over to mdedit where the host
// registers no editor app of its own: one way to edit the text at a time.
func (inst *App) renderEditInMdedit() {
	if inst.doc == nil || inst.bus == nil {
		return
	}
	if _, ok := externaledit.Editor(); ok {
		return
	}
	for range c.HoverText(tipEditInMdedit).KeepIter() {
		if c.Button(inst.ids.PrepareStr("editInMdedit"), c.Atoms().Text(labelEditInMdedit).Keep()).SendResp().HasPrimaryClicked() {
			inst.editInMdedit()
		}
	}
}

// editInMdedit asks the window host for an mdedit window on the text. The
// request runs off the render goroutine and the status row says what it
// came to once the host answered.
func (inst *App) editInMdedit() {
	inst.mu.Lock()
	busy := inst.handBusy
	inst.mu.Unlock()
	if busy || inst.doc == nil || inst.bus == nil {
		return
	}
	l := mdlaunch.MdeditLaunch{Text: string(inst.doc.text), Name: inst.doc.name}
	if r := inst.doc.ref; r != nil {
		l.Mount, l.SnapNanos, l.Path = r.mount.Value(), r.snap.UnixNano(), r.path
	}
	cfg, err := buscodec.Encode(l)
	if err != nil {
		inst.note = "the hand-over to mdedit could not be encoded: " + err.Error()
		return
	}
	bus := inst.bus
	inst.mu.Lock()
	inst.handBusy = true
	inst.mu.Unlock()
	inst.note = "opening mdedit…"
	started := inst.scope.Go(func(context.Context) {
		_, oErr := windowopen.RequestOpen(bus, mdlaunch.AppId, mdlaunch.Kind, cfg)
		inst.mu.Lock()
		inst.handBusy = false
		if oErr != nil {
			inst.handNote = "mdedit was not opened: " + oErr.Error()
		} else {
			inst.handNote = "opened in mdedit; Open in notebook there brings the text back"
		}
		inst.mu.Unlock()
	})
	if !started {
		inst.mu.Lock()
		inst.handBusy = false
		inst.mu.Unlock()
	}
}

// ComposeWorkingset is the host's closing-edge hook (ADR-0148 §SD4): the
// launch config that opens this window's notebook again, written only when
// the person did something in the window. A notebook of a snapshot is
// named by its snapshot, with its text when that was edited here; a text
// that is of no snapshot travels whole. A sample that was not edited is not
// kept: an empty window offers it.
func (inst *App) ComposeWorkingset() (cfg []byte, dirty bool, err error) {
	doc := inst.doc
	if !inst.touched || doc == nil || (doc.sample != "" && !doc.edited) {
		return nil, false, nil
	}
	l := launchcfg.NotebookLaunch{Name: doc.name}
	if r := doc.ref; r != nil {
		l.Mount, l.SnapNanos, l.Path = r.mount.Value(), r.snap.UnixNano(), r.path
	}
	if doc.ref == nil || doc.edited {
		l.Text = string(doc.text)
	}
	cfg, err = buscodec.Encode(l)
	if err != nil {
		return nil, false, eh.Errorf("notebook: encode workingset: %w", err)
	}
	return cfg, true, nil
}

// landHandOver takes the outcome of a hand-over into the status row.
func (inst *App) landHandOver() {
	inst.mu.Lock()
	note := inst.handNote
	inst.handNote = ""
	inst.mu.Unlock()
	if note != "" {
		inst.note = note
	}
}
