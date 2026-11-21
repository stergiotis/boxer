package mdedit

// The hand-over of a notebook to the window that works on one (ADR-0371
// §SD10). mdedit is where a notebook's prose and presentation cells are
// written; the notebook window is where its records are worked on and where
// it is collected. The button opens that window on the buffer as it stands,
// with the snapshot the document was read from, so the stored results the
// text names are found beside it there. The notebook window hands a text
// here the same way, by mdedit's launch config.

import (
	"context"
	"path"

	nblaunch "github.com/stergiotis/boxer/apps/notebook/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

var atomsOpenNotebook = c.Atoms().Text("Open in notebook").Keep()

const tipOpenNotebook = "Open the notebook window on this text: work on its records in play and collect it. The text is handed over as it stands, with the snapshot it was read from; what was changed here is not stored until the notebook is collected."

// notebookHandover is the hand-over's state: whether the buffer is a
// notebook, decided once per text, and what the last request came to,
// written off the render goroutine under App.mu.
type notebookHandover struct {
	src   string
	srcOk bool
	isNb  bool
	// name is the name the document was handed over under, "" for one that
	// was not: what it is handed on as when no file names it.
	name string
	// note is the outcome of a request, taken into the status line by the
	// next frame; busy says one is in flight.
	note string
	busy bool
}

// bufferIsNotebook says whether the buffer holds a record: the same test
// the reading mode applies before it draws a notebook.
func (inst *App) bufferIsNotebook() bool {
	h := &inst.nbHand
	if !h.srcOk || h.src != inst.src {
		h.src, h.srcOk, h.isNb = inst.src, true, false
		if nb, err := mdnotebook.Read([]byte(inst.src)); err == nil {
			h.isNb = isNotebook(nb)
		}
	}
	return h.isNb
}

// renderOpenNotebook draws the hand-over button for a buffer that is a
// notebook, and lands the outcome of the last request in the status line.
func (inst *App) renderOpenNotebook() {
	inst.mu.Lock()
	note, busy := inst.nbHand.note, inst.nbHand.busy
	inst.nbHand.note = ""
	inst.mu.Unlock()
	if note != "" {
		inst.status = note
	}
	if !inst.bufferIsNotebook() {
		return
	}
	clicked := false
	for range c.HoverText(tipOpenNotebook).KeepIter() {
		clicked = c.Button(inst.ids.PrepareStr("opennotebook"), atomsOpenNotebook).SendResp().HasPrimaryClicked()
	}
	if clicked && !busy {
		inst.openInNotebook()
	}
}

// openInNotebook asks the window host for a notebook window on the buffer.
// The request runs off the render goroutine; the status line says what it
// came to once the host answered.
func (inst *App) openInNotebook() {
	if inst.bus == nil {
		inst.status = "no bus wired — cannot open the notebook window"
		return
	}
	name := inst.boundName
	if name == "" {
		name = inst.readName
	}
	if name == "" {
		name = inst.nbHand.name
	}
	l := nblaunch.NotebookLaunch{Text: inst.src, Name: name}
	if r := inst.snapRef; r != nil && r.path != "" {
		l.Mount, l.SnapNanos, l.Path = r.mount.Value(), r.snap.UnixNano(), r.path
		if l.Name == "" {
			l.Name = path.Base(r.path)
		}
	}
	cfg, err := buscodec.Encode(l)
	if err != nil {
		inst.status = "the notebook hand-over could not be encoded: " + err.Error()
		return
	}
	bus := inst.bus
	inst.mu.Lock()
	inst.nbHand.busy = true
	inst.mu.Unlock()
	inst.status = "opening the notebook window…"
	started := inst.spawn(func(context.Context) {
		_, oErr := windowopen.RequestOpen(bus, nblaunch.AppId, nblaunch.Kind, cfg)
		inst.mu.Lock()
		inst.nbHand.busy = false
		if oErr != nil {
			inst.nbHand.note = "the notebook window was not opened: " + oErr.Error()
		} else {
			inst.nbHand.note = "opened in the notebook window"
		}
		inst.mu.Unlock()
	})
	if !started {
		inst.mu.Lock()
		inst.nbHand.busy = false
		inst.mu.Unlock()
	}
}
