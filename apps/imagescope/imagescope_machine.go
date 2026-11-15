package imagescope

import (
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
	phaseEmpty     phaseE = iota // no image
	phaseOpening                 // a file is being chosen, read or decoded, or a sample made
	phaseViewing                 // an image is shown; no region is measured
	phaseMeasuring               // a region is marked and its statistics are being computed
	phaseMeasured                // a region is marked and its statistics are shown
)

var allPhases = []phaseE{phaseEmpty, phaseOpening, phaseViewing, phaseMeasuring, phaseMeasured}

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
	}
	return "?"
}

// observePhase derives the phase from this frame's facts. An open in flight
// outranks what it will replace; a marked region whose measurement failed
// is viewing, with the reason in the statistics pane.
func (inst *App) observePhase() (ph phaseE) {
	switch {
	case inst.open.Running():
		return phaseOpening
	case inst.img.IsZero():
		return phaseEmpty
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
// region of it is marked and measured, and either can be replaced.
func newPhaseMachine() (m *fsmview.Machine[phaseE]) {
	m = fsmview.NewMachine(phaseEmpty, 64, fsmview.MachineOptions[phaseE]{
		Label:      func(ph phaseE) string { return ph.String() },
		StateOrder: allPhases,
		StateColor: phaseColor,
	})
	m.AddRule(phaseEmpty, phaseOpening).
		AddRule(phaseOpening, phaseViewing, phaseMeasuring, phaseEmpty).
		AddRule(phaseViewing, phaseOpening, phaseMeasuring).
		AddRule(phaseMeasuring, phaseMeasured, phaseViewing, phaseOpening).
		AddRule(phaseMeasured, phaseMeasuring, phaseViewing, phaseOpening).
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
		EdgeLabel(phaseMeasured, phaseOpening, "Open, or a sample")
	return
}

// phaseColor tints the graph's nodes: the running phases in the accent, a
// measurement in the success role, the rest muted.
func phaseColor(ph phaseE, _ bool) styletokens.RGBA8 {
	switch ph {
	case phaseOpening, phaseMeasuring:
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
	case phaseOpening, phaseMeasuring:
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
const tipPhase = "Where the window stands: no image, one being opened, one shown, or a region of it being measured or measured. Click for the graph and the history."

// phaseLine is the phase with what it holds.
func (inst *App) phaseLine() (s string) {
	ph := inst.observePhase()
	if ph == phaseEmpty || ph == phaseOpening {
		return ph.String()
	}
	return ph.String() + ": " + describeImage(inst.img)
}
