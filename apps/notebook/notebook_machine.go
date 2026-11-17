package notebook

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// phaseE is where the window's notebook stands. The window never drives
// the machine: each frame observes the phase from what it holds
// (observePhase) and mirrors it, as play does for its query result. The
// rules draw the graph the state chip opens and name the gesture behind
// each edge; an edge they do not declare is followed, and graded in the log
// by mirrorPhase.
type phaseE uint8

const (
	phaseEmpty      phaseE = iota // no notebook is open
	phaseOpening                  // a snapshot is being read from the store
	phaseUnreadable               // the text is not a notebook
	phaseReading                  // the stored results are being read and the cells answered
	phaseSealed                   // the notebook shows what it stores
	phaseStale                    // a record was edited since collection; what is stored is shown
	phaseUnsealed                 // a problem, or a cell with no answer
	phaseCollecting               // the records run and the snapshot is published
	phaseRunning                  // the records are computed for a live reading (ADR-0380)
	phaseLive                     // the notebook is shown over records computed when it was run
)

var allPhases = []phaseE{phaseEmpty, phaseOpening, phaseUnreadable, phaseReading, phaseSealed, phaseStale, phaseUnsealed, phaseCollecting, phaseRunning, phaseLive}

func (inst phaseE) String() (s string) {
	switch inst {
	case phaseEmpty:
		return "nothing open"
	case phaseOpening:
		return "opening"
	case phaseUnreadable:
		return "not a notebook"
	case phaseReading:
		return "reading"
	case phaseSealed:
		return "sealed"
	case phaseStale:
		return "stale"
	case phaseUnsealed:
		return "not sealed"
	case phaseCollecting:
		return "collecting"
	case phaseRunning:
		return "running live"
	case phaseLive:
		return "live"
	}
	return "?"
}

// observePhase derives the phase from what the window holds this frame,
// with no memory of its own. A running collection outranks an open, and
// both outrank what the text on screen came to: a failure returns the
// window to the phase it interrupted.
func (inst *App) observePhase() (ph phaseE) {
	switch {
	case inst.collect.job.Running():
		return phaseCollecting
	case inst.pane.job.Running():
		return phaseRunning
	case inst.opening != nil:
		return phaseOpening
	case inst.doc == nil:
		return phaseEmpty
	case inst.nb == nil:
		return phaseUnreadable
	case inst.pane.shown == readingLive:
		return phaseLive
	case !inst.checked:
		return phaseReading
	case inst.check.Sealed():
		return phaseSealed
	case len(inst.check.Problems) == 0 && len(inst.check.Refused) == 0:
		// Nothing is wrong but the age of what is stored: collection
		// cures it.
		return phaseStale
	}
	return phaseUnsealed
}

// mirrorPhase moves the machine to the observed phase.
func (inst *App) mirrorPhase() {
	m := inst.phaseMachine
	cur, obs := m.Current(), inst.observePhase()
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := inst.log.Warn()
	msg := "notebook: the phase moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = inst.log.Debug()
		msg = "notebook: the phase skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

func newPhaseMachine() (m *fsmview.Machine[phaseE]) {
	m = fsmview.NewMachine(phaseEmpty, 64, fsmview.MachineOptions[phaseE]{
		Label:      func(ph phaseE) string { return ph.String() },
		StateOrder: allPhases,
		StateColor: phaseColor,
	})
	settled := []phaseE{phaseUnreadable, phaseSealed, phaseStale, phaseUnsealed}
	m.AddRule(phaseEmpty, phaseOpening, phaseReading, phaseUnreadable).
		AddRule(phaseOpening, phaseEmpty, phaseReading, phaseUnreadable, phaseSealed, phaseStale, phaseUnsealed).
		AddRule(phaseReading, phaseSealed, phaseStale, phaseUnsealed, phaseOpening).
		AddRule(phaseUnreadable, phaseReading, phaseOpening).
		AddRule(phaseSealed, phaseReading, phaseUnreadable, phaseOpening, phaseCollecting, phaseRunning).
		AddRule(phaseStale, phaseReading, phaseUnreadable, phaseOpening, phaseCollecting, phaseRunning).
		AddRule(phaseUnsealed, phaseReading, phaseUnreadable, phaseOpening, phaseCollecting, phaseRunning).
		AddRule(phaseCollecting, phaseOpening, phaseSealed, phaseStale, phaseUnsealed, phaseReading, phaseLive).
		AddRule(phaseRunning, phaseLive, phaseSealed, phaseStale, phaseUnsealed, phaseReading, phaseOpening).
		AddRule(phaseLive, phaseRunning, phaseReading, phaseSealed, phaseStale, phaseUnsealed, phaseOpening, phaseCollecting, phaseUnreadable).
		EdgeLabel(phaseEmpty, phaseOpening, "Open…, or a hand-over with a snapshot").
		EdgeLabel(phaseEmpty, phaseReading, "Open sample, or a hand-over of a text").
		EdgeLabel(phaseOpening, phaseEmpty, "could not be opened").
		EdgeLabel(phaseOpening, phaseReading, "the snapshot's text").
		EdgeLabel(phaseReading, phaseSealed, "nothing owed").
		EdgeLabel(phaseReading, phaseStale, "a record edited since collection").
		EdgeLabel(phaseReading, phaseUnsealed, "a problem, or a cell refused").
		EdgeLabel(phaseCollecting, phaseOpening, "published").
		EdgeLabel(phaseCollecting, phaseSealed, "not collected").
		EdgeLabel(phaseCollecting, phaseStale, "not collected").
		EdgeLabel(phaseCollecting, phaseUnsealed, "not collected")
	for _, ph := range settled {
		m.EdgeLabel(ph, phaseOpening, "Open…")
		m.EdgeLabel(phaseOpening, ph, "could not be opened")
	}
	for _, ph := range []phaseE{phaseSealed, phaseStale, phaseUnsealed} {
		m.EdgeLabel(ph, phaseReading, "Take the buffer back, or an editor's save").
			EdgeLabel(ph, phaseCollecting, "Collect and publish").
			EdgeLabel(ph, phaseRunning, "Run live").
			EdgeLabel(phaseRunning, ph, "the run did not answer, or was cancelled")
	}
	m.EdgeLabel(phaseRunning, phaseLive, "every record answered").
		EdgeLabel(phaseLive, phaseRunning, "Run again").
		EdgeLabel(phaseLive, phaseReading, "Show stored, or the text changed").
		EdgeLabel(phaseLive, phaseCollecting, "Collect and publish").
		EdgeLabel(phaseLive, phaseOpening, "Open…")
	m.EdgeLabel(phaseUnreadable, phaseReading, "an editor's save")
	return
}

func phaseColor(ph phaseE, _ bool) styletokens.RGBA8 {
	switch ph {
	case phaseOpening, phaseReading, phaseCollecting, phaseRunning, phaseLive:
		return styletokens.AccentDefault
	case phaseSealed:
		return styletokens.SuccessDefault
	case phaseStale:
		return styletokens.WarningDefault
	case phaseUnsealed, phaseUnreadable:
		return styletokens.ErrorDefault
	}
	return styletokens.NeutralSubtle
}

func phaseTone(ph phaseE) badge.ToneE {
	switch ph {
	case phaseOpening, phaseReading, phaseCollecting, phaseRunning, phaseLive:
		return badge.TonePrimary
	case phaseSealed:
		return badge.ToneSuccess
	case phaseStale:
		return badge.ToneWarning
	case phaseUnsealed, phaseUnreadable:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// renderPhase draws the phase as the chip of the status row; a click opens
// the machine behind it.
func (inst *App) renderPhase() {
	if inst.phaseChip == nil {
		inst.phaseChip = fsmview.New(inst.ids, "notebook-phase", inst.phaseMachine, fsmview.Options[phaseE]{
			Title:      "Notebook phase",
			Tethered:   true,
			BadgeTone:  phaseTone,
			AutoAnchor: true,
		})
	}
	inst.phaseChip.Opts.Provenance = inspector.Provenance{
		Subject:   "app.notebook.document.phase",
		SourceApp: string(AppId),
	}
	inst.phaseChip.Render()
}
