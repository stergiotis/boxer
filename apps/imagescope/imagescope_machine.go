package imagescope

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// phaseE is where the window stands: whether it has an image, and whether a
// region of it is marked and measured. It is derived, never driven
// (keelson-app T1): each frame observes it from the open job, the image and
// the measurement (observePhase) and mirrors it. An open that fails or is
// cancelled owns no phase; the window returns to the one the open
// interrupted, and the status row says why.
type phaseE uint8

const (
	phaseEmpty           phaseE = iota // no image
	phaseOpening                       // a file is being chosen, read or decoded, or a sample made
	phaseViewing                       // an image is shown; no region is measured
	phaseMeasuring                     // a region is marked and its statistics are being computed
	phaseMeasured                      // a region is marked and its statistics are shown
	phaseSelecting                     // another selection of the stack open is being taken (ADR-0398 §M6)
	phaseRegionMeasuring               // regions are being measured (ADR-0399 §M2)
	phaseProfiling                     // a line's profile is being taken (ADR-0399 §M2)
	phaseRunning                       // an operation is being run (ADR-0400 §M2)
)

var allPhases = []phaseE{phaseEmpty, phaseOpening, phaseViewing, phaseSelecting, phaseMeasuring, phaseMeasured,
	phaseRegionMeasuring, phaseProfiling, phaseRunning}

// workPhases are the phases of work on the image shown that the window
// runs as a task: each is entered from any phase with an image and left
// for any of them, as the work starts and lands between frames.
var workPhases = []phaseE{phaseRegionMeasuring, phaseProfiling, phaseRunning}

// imagePhases are the phases with an image shown that no task of the
// window's work holds.
var imagePhases = []phaseE{phaseViewing, phaseSelecting, phaseMeasuring, phaseMeasured}

func (inst phaseE) String() (s string) {
	switch inst {
	case phaseEmpty:
		return "empty"
	case phaseOpening:
		return "opening"
	case phaseViewing:
		return "viewing"
	case phaseMeasuring:
		return "measuring"
	case phaseMeasured:
		return "measured"
	case phaseSelecting:
		return "selecting"
	case phaseRegionMeasuring:
		return "measuring-regions"
	case phaseProfiling:
		return "profiling"
	case phaseRunning:
		return "running"
	}
	return "?"
}

// observePhase derives the phase from this frame's facts. An open in flight
// outranks what it will replace, and a selection being taken outranks the
// measurement of the one it replaces; then a run, a profile and a
// measurement of regions, in that order, outrank the marked rectangle's
// measurement, which goes on beside them; a marked region whose
// measurement failed is viewing, with the reason in the statistics pane.
func (inst *App) observePhase() (ph phaseE) {
	switch {
	case inst.open.Running():
		return phaseOpening
	case inst.pic == nil:
		return phaseEmpty
	case inst.selectionPending():
		return phaseSelecting
	case inst.runJob.Running():
		return phaseRunning
	case inst.profileJob.Running():
		return phaseProfiling
	case inst.regionJob.Running():
		return phaseRegionMeasuring
	}
	if _, marked := inst.viewer.Region(); marked {
		switch {
		case inst.measuring:
			return phaseMeasuring
		case inst.measured != nil:
			return phaseMeasured
		}
	}
	return phaseViewing
}

// mirrorPhase moves the machine to the phase this frame observed. A frame
// samples the jobs, so one that starts and lands between two frames shows
// as an edge that skips its running phase; that is the declared path taken
// unwatched and logs at debug. An observation no declared path reaches
// contradicts the model and logs a warning.
func (inst *App) mirrorPhase() {
	m := inst.phaseMachine
	cur, obs := m.Current(), inst.observePhase()
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := inst.logger.Warn()
	msg := "imagescope: the phase moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = inst.logger.Debug()
		msg = "imagescope: the phase skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// newPhaseMachine declares the window's lifecycle: an image is opened, a
// region of it is marked and measured, either can be replaced, and the image
// of a stack can be replaced by another selection of it, which the marked
// region's measurement then follows.
func newPhaseMachine() (m *fsmview.Machine[phaseE]) {
	m = fsmview.NewMachine(phaseEmpty, 64, fsmview.MachineOptions[phaseE]{
		Label:      func(ph phaseE) string { return ph.String() },
		StateOrder: allPhases,
		StateColor: phaseColor,
	})
	m.AddRule(phaseEmpty, phaseOpening).
		AddRule(phaseOpening, phaseViewing, phaseMeasuring, phaseEmpty).
		AddRule(phaseViewing, phaseOpening, phaseMeasuring, phaseSelecting).
		AddRule(phaseMeasuring, phaseMeasured, phaseViewing, phaseOpening, phaseSelecting).
		AddRule(phaseMeasured, phaseMeasuring, phaseViewing, phaseOpening, phaseSelecting).
		AddRule(phaseSelecting, phaseViewing, phaseMeasuring, phaseOpening).
		EdgeLabel(phaseEmpty, phaseOpening, "Open, or a sample").
		EdgeLabel(phaseOpening, phaseViewing, "the image lands, or the open fails over an image").
		EdgeLabel(phaseOpening, phaseMeasuring, "the image lands with a seeded region").
		EdgeLabel(phaseOpening, phaseEmpty, "the open fails or is cancelled").
		EdgeLabel(phaseViewing, phaseOpening, "Open, or a sample").
		EdgeLabel(phaseViewing, phaseMeasuring, "a region is marked").
		EdgeLabel(phaseMeasuring, phaseMeasured, "the statistics land").
		EdgeLabel(phaseMeasuring, phaseViewing, "Clear region, or the measurement fails").
		EdgeLabel(phaseMeasuring, phaseOpening, "Open, or a sample").
		EdgeLabel(phaseMeasured, phaseMeasuring, "another region is marked").
		EdgeLabel(phaseMeasured, phaseViewing, "Clear region").
		EdgeLabel(phaseMeasured, phaseOpening, "Open, or a sample").
		EdgeLabel(phaseViewing, phaseSelecting, "a position or the bands are set").
		EdgeLabel(phaseMeasuring, phaseSelecting, "a position or the bands are set").
		EdgeLabel(phaseMeasured, phaseSelecting, "a position or the bands are set").
		EdgeLabel(phaseSelecting, phaseViewing, "the selection is shown, or taking it failed").
		EdgeLabel(phaseSelecting, phaseMeasuring, "the selection is shown with a region marked, which is measured again").
		EdgeLabel(phaseSelecting, phaseOpening, "Open, or a sample")
	// The window's work on the image shown: a run, a profile, a measurement
	// of regions. Each starts from any phase with an image and lands in any
	// of them, or in another of the three when one starts as another ends;
	// an open or a selection outranks them.
	for _, w := range workPhases {
		m.AddRule(w, append(append([]phaseE{phaseOpening}, imagePhases...), otherWork(w)...)...)
		m.EdgeLabel(w, phaseOpening, "Open, or a sample")
		m.AddRule(phaseOpening, w)
		m.EdgeLabel(phaseOpening, w, "the image lands while the work goes on")
		for _, ph := range imagePhases {
			m.AddRule(ph, w)
			m.EdgeLabel(ph, w, workStarts(w))
			m.EdgeLabel(w, ph, workEnds(w))
		}
		for _, o := range otherWork(w) {
			m.EdgeLabel(w, o, workEnds(w)+", while "+strings.TrimSuffix(workStarts(o), " starts")+" starts")
		}
	}
	return
}

// otherWork are the work phases other than w.
func otherWork(w phaseE) (out []phaseE) {
	for _, o := range workPhases {
		if o != w {
			out = append(out, o)
		}
	}
	return
}

// workStarts labels the edge into a work phase: the gesture that starts it.
func workStarts(w phaseE) string {
	switch w {
	case phaseRunning:
		return "Run, or run_operation"
	case phaseProfiling:
		return "Profile of a line"
	}
	return "Measure, Measure all, or measure_region"
}

// workEnds labels the edge out of a work phase.
func workEnds(w phaseE) string {
	switch w {
	case phaseRunning:
		return "the result lands, or the run fails or is cancelled"
	case phaseProfiling:
		return "the profile lands, or fails"
	}
	return "the figures land, or the measurement fails"
}

// phaseColor tints the graph's nodes: the running phases in the accent, a
// measurement in the success role, the rest muted.
func phaseColor(ph phaseE, _ bool) styletokens.RGBA8 {
	switch ph {
	case phaseOpening, phaseMeasuring, phaseSelecting, phaseRegionMeasuring, phaseProfiling, phaseRunning:
		return styletokens.AccentDefault
	case phaseMeasured:
		return styletokens.SuccessDefault
	case phaseViewing:
		return styletokens.NeutralDefault
	}
	return styletokens.NeutralSubtle
}

// phaseTone is phaseColor for the chip's badge.
func phaseTone(ph phaseE) badge.ToneE {
	switch ph {
	case phaseOpening, phaseMeasuring, phaseSelecting, phaseRegionMeasuring, phaseProfiling, phaseRunning:
		return badge.TonePrimary
	case phaseMeasured:
		return badge.ToneSuccess
	}
	return badge.ToneNeutral
}

// renderPhase draws the state chip (keelson-app T2) with its one-line
// summary; the chip opens the inspector with the graph, the table and the
// history. The tour stage never mounts, so it has no chip to draw.
func (inst *App) renderPhase() {
	if inst.phaseChip == nil {
		return
	}
	inst.phaseChip.Opts.Provenance = inspector.Provenance{
		Subject:   "app.imagescope.image.phase",
		SourceApp: string(manifest.Id),
	}
	inst.phaseChip.Opts.Summary = func() {
		metric(inst.phaseLine(), tipPhase)
	}
	inst.phaseChip.Render()
}

// tipPhase explains the chip.
const tipPhase = "Where the window stands: no image, one being opened, one shown, another selection of a stack being taken, the marked region being measured or measured, regions being measured, a line being profiled, or an operation being run. Click for the graph and the history."

// phaseLine is the phase with what it holds.
func (inst *App) phaseLine() (s string) {
	ph := inst.observePhase()
	if ph == phaseEmpty || ph == phaseOpening {
		return ph.String()
	}
	return ph.String() + ": " + inst.describeShown()
}

// describeShown is the image shown in one line and, for a stack's, the
// selection it is.
func (inst *App) describeShown() string {
	s := describeImage(inst.pic)
	if stk := inst.stackShown(); stk != nil && inst.pic != nil {
		s += " · " + stk.describe(stk.shown)
	}
	if d := inst.describeComposite(); d != "" {
		s += " · " + d
	}
	if inst.shownId != "" && inst.shownId != openedId {
		s = inst.shownId + " · " + s
	}
	return s
}
