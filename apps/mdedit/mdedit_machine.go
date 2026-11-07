package mdedit

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// phaseE is where the document stands between the buffer and the places
// it comes from and goes to: the store it is restored from, the file it is
// opened from and saved to, the facts table a send writes. It is a different
// thing from the view mode — the mode says what the window shows, the phase
// says what the document is up to — so a mode can change without moving it.
//
// The window never drives the machine: each frame observes the phase from
// the facts (observeDocument over docFacts) and mirrors it, as jackstay does
// for its plan and play for its query result. The rules draw the graph the
// state chip opens and name the gestures that cause the edges; an edge they
// do not declare is followed, and graded in the log by mirrorPhase.
type phaseE uint8

const (
	phaseRestoring   phaseE = iota // the persisted document is being read, at mount
	phaseClean                     // the buffer is the checkpoint: saved, copied out or restored
	phaseModified                  // the buffer differs from the checkpoint
	phaseFollowing                 // clean, and the opened file is followed on disk
	phaseOpening                   // the read dialog is up, or the file is being read
	phaseSaving                    // the save dialog is up, or the bytes are on their way
	phaseSending                   // the document is being uploaded, with or without a play launch
	phaseDiskChanged               // the followed file changed or went away while the buffer could not take it
)

var allDocPhases = []phaseE{
	phaseRestoring, phaseClean, phaseModified, phaseFollowing, phaseOpening, phaseSaving, phaseSending, phaseDiskChanged,
}

func (inst phaseE) String() (s string) {
	switch inst {
	case phaseRestoring:
		return "restoring"
	case phaseClean:
		return "clean"
	case phaseModified:
		return "modified"
	case phaseFollowing:
		return "following"
	case phaseOpening:
		return "opening"
	case phaseSaving:
		return "saving"
	case phaseSending:
		return "sending"
	case phaseDiskChanged:
		return "changed on disk"
	}
	return "?"
}

// docFacts is what one frame knows about the document's lifecycle, gathered
// by App.documentFacts and judged by observeDocument. A struct rather than
// the App, so the judgement is a pure function a table-driven test can run
// without a window.
type docFacts struct {
	restoring   bool
	fileBusy    bool
	fileOp      fileOpE
	sending     bool
	dirty       bool
	diskChanged bool
	diskGone    bool
	following   bool
}

// observeDocument derives the phase from the facts, with no memory of its
// own. Work in flight outranks the buffer's state: a restore, then a file
// gesture, then a send. A disk that moved under the buffer outranks the
// dirty marker, because it is the thing the person has to decide about.
// Following shows only while the buffer is clean — a dirty buffer under a
// follow is modified, and what the follow then finds is changed on disk.
func observeDocument(f docFacts) (ph phaseE) {
	switch {
	case f.restoring:
		return phaseRestoring
	case f.fileBusy && f.fileOp == fileOpOpen:
		return phaseOpening
	case f.fileBusy && f.fileOp == fileOpSave:
		return phaseSaving
	case f.sending:
		return phaseSending
	case f.diskChanged || f.diskGone:
		return phaseDiskChanged
	case f.dirty:
		return phaseModified
	case f.following:
		return phaseFollowing
	}
	return phaseClean
}

// documentFacts gathers this frame's facts. The in-flight flags live under
// mu because their goroutines clear them; the rest is render-thread state.
func (inst *App) documentFacts() (f docFacts) {
	inst.mu.Lock()
	f.fileBusy, f.fileOp, f.sending = inst.fileBusy, inst.fileOp, inst.sending
	inst.mu.Unlock()
	f.restoring = inst.restoreBusy
	f.dirty = inst.dirty()
	f.diskChanged, f.diskGone, f.following = inst.diskChanged, inst.diskGone, inst.followActive
	return
}

// mirrorPhase moves the machine to the phase this frame observed. A frame
// samples the flags, so a gesture that starts and finishes between two
// frames shows as an edge that skips its running phase; that is the declared
// path taken unwatched and logs at debug. An observation no declared path
// reaches contradicts the model and logs a warning.
func (inst *App) mirrorPhase() {
	m := inst.docMachine
	if m == nil {
		return
	}
	cur, obs := m.Current(), observeDocument(inst.documentFacts())
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := inst.logger.Warn()
	msg := "mdedit: the document phase moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = inst.logger.Debug()
		msg = "mdedit: the document phase skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// newDocMachine declares the document's lifecycle. The restore leads: a
// window opens on the persisted document, or on nothing. Every settled phase
// can open, save or send, and a running gesture returns to the phase it
// interrupted when it is cancelled or fails, so failure needs no state of its
// own; the status line says why. Following is clean under a watched file;
// typing leaves it for modified, and a disk change under a modified buffer
// is the one phase the person has to resolve by hand.
func newDocMachine() (m *fsmview.Machine[phaseE]) {
	m = fsmview.NewMachine(phaseRestoring, 64, fsmview.MachineOptions[phaseE]{
		Label:      func(ph phaseE) string { return ph.String() },
		StateOrder: allDocPhases,
		StateColor: docPhaseColor,
	})
	settled := []phaseE{phaseClean, phaseModified, phaseFollowing, phaseDiskChanged}
	m.AddRule(phaseRestoring, phaseClean, phaseModified).
		AddRule(phaseClean, phaseModified, phaseOpening, phaseSaving, phaseSending).
		AddRule(phaseModified, phaseClean, phaseOpening, phaseSaving, phaseSending, phaseDiskChanged).
		AddRule(phaseFollowing, phaseClean, phaseModified, phaseOpening, phaseSaving, phaseSending, phaseDiskChanged).
		AddRule(phaseDiskChanged, phaseClean, phaseModified, phaseFollowing, phaseOpening, phaseSaving, phaseSending).
		AddRule(phaseOpening, settled...).
		AddRule(phaseSaving, settled...).
		AddRule(phaseSending, settled...).
		EdgeLabel(phaseRestoring, phaseClean, "restored, or nothing to restore").
		EdgeLabel(phaseRestoring, phaseModified, "typed before the restore landed").
		EdgeLabel(phaseClean, phaseModified, "typing").
		EdgeLabel(phaseClean, phaseOpening, "Open").
		EdgeLabel(phaseClean, phaseSaving, "Save / Save as…").
		EdgeLabel(phaseClean, phaseSending, "Send to play / Upload").
		EdgeLabel(phaseModified, phaseClean, "Copy to clipboard").
		EdgeLabel(phaseModified, phaseOpening, "Open, clicked twice").
		EdgeLabel(phaseModified, phaseSaving, "Save").
		EdgeLabel(phaseModified, phaseSending, "Send to play / Upload").
		EdgeLabel(phaseModified, phaseDiskChanged, "the followed file moved").
		EdgeLabel(phaseFollowing, phaseClean, "the follow ended").
		EdgeLabel(phaseFollowing, phaseModified, "typing").
		EdgeLabel(phaseFollowing, phaseOpening, "Open").
		EdgeLabel(phaseFollowing, phaseSaving, "Save / Save as…").
		EdgeLabel(phaseFollowing, phaseSending, "Send to play / Upload").
		EdgeLabel(phaseFollowing, phaseDiskChanged, "the file went away").
		EdgeLabel(phaseDiskChanged, phaseModified, "the disk returned to the checkpoint").
		EdgeLabel(phaseDiskChanged, phaseFollowing, "reloaded from disk").
		EdgeLabel(phaseDiskChanged, phaseClean, "reloaded, follow ended").
		EdgeLabel(phaseDiskChanged, phaseOpening, "Open").
		EdgeLabel(phaseDiskChanged, phaseSaving, "Save / Save as…").
		EdgeLabel(phaseDiskChanged, phaseSending, "Send to play / Upload").
		EdgeLabel(phaseOpening, phaseFollowing, "opened, and followed").
		EdgeLabel(phaseOpening, phaseClean, "opened").
		EdgeLabel(phaseSaving, phaseClean, "saved").
		EdgeLabel(phaseSaving, phaseFollowing, "saved, still followed").
		EdgeLabel(phaseSending, phaseClean, "landed").
		EdgeLabel(phaseSending, phaseFollowing, "landed")
	for _, ph := range []phaseE{phaseModified, phaseDiskChanged} {
		m.EdgeLabel(phaseOpening, ph, "cancelled, or failed").
			EdgeLabel(phaseSaving, ph, "typed meanwhile, cancelled, or failed").
			EdgeLabel(phaseSending, ph, "landed, or failed")
	}
	return
}

// docPhaseColor tints the graph's nodes: the running phases in the accent,
// the one the person must resolve as a warning, the rest muted.
func docPhaseColor(ph phaseE, _ bool) styletokens.RGBA8 {
	switch ph {
	case phaseRestoring, phaseOpening, phaseSaving, phaseSending:
		return styletokens.AccentDefault
	case phaseModified, phaseDiskChanged:
		return styletokens.WarningDefault
	case phaseClean, phaseFollowing:
		return styletokens.NeutralDefault
	}
	return styletokens.NeutralSubtle
}

// docPhaseTone is docPhaseColor for the chip's badge.
func docPhaseTone(ph phaseE) badge.ToneE {
	switch ph {
	case phaseRestoring, phaseOpening, phaseSaving, phaseSending:
		return badge.TonePrimary
	case phaseModified, phaseDiskChanged:
		return badge.ToneWarning
	}
	return badge.ToneNeutral
}

// renderPhase draws the state chip in the status row with its one-line
// summary. The chip opens the inspector: the graph, the history of phases
// and where they come from. Built on first use rather than at Mount, so the
// tour, which mounts nothing, draws the same chip.
func (inst *App) renderPhase() {
	if inst.docMachine == nil {
		return
	}
	if inst.docChip == nil {
		inst.docChip = fsmview.New(inst.ids, "doc-phase", inst.docMachine, fsmview.Options[phaseE]{
			Title:      "Document phase",
			Tethered:   true,
			BadgeTone:  docPhaseTone,
			AutoAnchor: true,
		})
	}
	inst.docChip.Opts.Provenance = inspector.Provenance{
		Subject:   "app.mdedit.document.phase",
		SourceApp: string(manifest.Id),
	}
	inst.docChip.Opts.Summary = func() {
		s := inst.phaseLine()
		if s == "" {
			return
		}
		muted := color.Hex(styletokens.NeutralTextSecondary.AsHex())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(muted, color.Transparent, s).Small().End().Keep()).Send()
	}
	inst.docChip.Render()
}

// phaseLine is the chip's summary: what the phase means for the buffer, in
// the words the badges it replaced used to carry as tooltips.
func (inst *App) phaseLine() (s string) {
	switch inst.docMachine.Current() {
	case phaseRestoring:
		return "reading the persisted document…"
	case phaseClean:
		return tipDirty
	case phaseModified:
		return "the buffer differs from the last checkpoint: a save, a completed copy to the clipboard, or the restore"
	case phaseFollowing:
		return "the opened file is watched; a change on disk is reloaded while the buffer is clean"
	case phaseOpening:
		return "choosing a file, or reading it…"
	case phaseSaving:
		return "choosing where to save, or writing…"
	case phaseSending:
		return "uploading the document to boxer.facts…"
	case phaseDiskChanged:
		if inst.diskGone {
			return tipDiskGone
		}
		return tipDiskChanged
	}
	return ""
}
