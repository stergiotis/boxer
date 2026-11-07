package terrainscope

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// phaseE is where the sweep stands: how much of the selection there is, and
// what the last sweep over it left. It is derived, never driven (keelson-app
// T1): each frame observes the phase from the selection stage and the
// runner (observePhase) and mirrors it, as jackstay's plan does. The rules
// below draw the graph the chip opens and name the gesture behind each edge;
// an edge they do not declare is followed, and graded in the log by
// mirrorPhase.
type phaseE uint8

const (
	phaseIdle      phaseE = iota // no point placed
	phaseOnePoint                // the observer placed, the target not yet
	phaseTwoPoints               // both placed; the sweep has not launched yet
	phaseSweeping                // the ensemble runs
	phaseResult                  // the latest sweep landed
	phaseFailed                  // the latest sweep failed; Retry or a parameter relaunches
	phaseCancelled               // the latest sweep was cancelled; Retry or a parameter relaunches
)

var allPhases = []phaseE{phaseIdle, phaseOnePoint, phaseTwoPoints, phaseSweeping, phaseResult, phaseFailed, phaseCancelled}

func (inst phaseE) String() (s string) {
	switch inst {
	case phaseIdle:
		return "idle"
	case phaseOnePoint:
		return "one point"
	case phaseTwoPoints:
		return "two points"
	case phaseSweeping:
		return "sweeping"
	case phaseResult:
		return "result"
	case phaseFailed:
		return "failed"
	case phaseCancelled:
		return "cancelled"
	}
	return "?"
}

// observePhase derives the phase from this frame's selection and runner. A
// running sweep outranks whatever it will replace; with both points placed
// and nothing running, exactly one of result, error and cancel note is set
// after a launch, and none before the first.
func (inst *App) observePhase() (ph phaseE) {
	switch inst.stage {
	case selectionStageNone:
		return phaseIdle
	case selectionStagePt1:
		return phaseOnePoint
	}
	switch {
	case inst.sweep.Running():
		return phaseSweeping
	case inst.result != nil:
		return phaseResult
	case inst.sweepErr != nil:
		return phaseFailed
	case inst.sweepNote != "":
		return phaseCancelled
	}
	return phaseTwoPoints
}

// mirrorPhase moves the machine to the phase this frame observed. A frame
// samples the runner, so a sweep that launches and lands between two frames
// shows as an edge that skips its running phase; that is the declared path
// taken unwatched and logs at debug. An observation no declared path reaches
// contradicts the model and logs a warning.
func (inst *App) mirrorPhase() {
	m := inst.phaseMachine
	cur, obs := m.Current(), inst.observePhase()
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := inst.logger.Warn()
	msg := "terrainscope: the phase moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = inst.logger.Debug()
		msg = "terrainscope: the phase skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// newPhaseMachine declares the sweep's lifecycle: two clicks, a sweep, and
// what it leaves. Every settled phase relaunches on a parameter change or
// Retry and clears on Reset; the sweep itself ends in a result, a failure or
// a cancel, or is discarded by Reset.
func newPhaseMachine() (m *fsmview.Machine[phaseE]) {
	m = fsmview.NewMachine(phaseIdle, 64, fsmview.MachineOptions[phaseE]{
		Label:      func(ph phaseE) string { return ph.String() },
		StateOrder: allPhases,
		StateColor: phaseColor,
	})
	m.AddRule(phaseIdle, phaseOnePoint, phaseTwoPoints).
		AddRule(phaseOnePoint, phaseTwoPoints, phaseIdle).
		AddRule(phaseTwoPoints, phaseSweeping, phaseIdle).
		AddRule(phaseSweeping, phaseResult, phaseFailed, phaseCancelled, phaseIdle).
		AddRule(phaseResult, phaseSweeping, phaseIdle).
		AddRule(phaseFailed, phaseSweeping, phaseIdle).
		AddRule(phaseCancelled, phaseSweeping, phaseIdle).
		EdgeLabel(phaseIdle, phaseOnePoint, "click the observer").
		EdgeLabel(phaseIdle, phaseTwoPoints, "set_points, or a launch config").
		EdgeLabel(phaseOnePoint, phaseTwoPoints, "click the target").
		EdgeLabel(phaseOnePoint, phaseIdle, "Reset").
		EdgeLabel(phaseTwoPoints, phaseSweeping, "the sweep launches").
		EdgeLabel(phaseTwoPoints, phaseIdle, "Reset").
		EdgeLabel(phaseSweeping, phaseResult, "lands").
		EdgeLabel(phaseSweeping, phaseFailed, "fails").
		EdgeLabel(phaseSweeping, phaseCancelled, "Cancel").
		EdgeLabel(phaseSweeping, phaseIdle, "Reset").
		EdgeLabel(phaseResult, phaseSweeping, "a parameter or the points change").
		EdgeLabel(phaseResult, phaseIdle, "Reset").
		EdgeLabel(phaseFailed, phaseSweeping, "Retry, or a parameter changes").
		EdgeLabel(phaseFailed, phaseIdle, "Reset").
		EdgeLabel(phaseCancelled, phaseSweeping, "Retry, or a parameter changes").
		EdgeLabel(phaseCancelled, phaseIdle, "Reset")
	return
}

// phaseColor tints the graph's nodes: the running phase in the accent, the
// outcomes by severity, the selection phases muted.
func phaseColor(ph phaseE, _ bool) styletokens.RGBA8 {
	switch ph {
	case phaseSweeping:
		return styletokens.AccentDefault
	case phaseResult:
		return styletokens.SuccessDefault
	case phaseFailed:
		return styletokens.ErrorDefault
	case phaseCancelled:
		return styletokens.WarningDefault
	case phaseTwoPoints:
		return styletokens.NeutralDefault
	}
	return styletokens.NeutralSubtle
}

// phaseTone is phaseColor for the chip's badge.
func phaseTone(ph phaseE) badge.ToneE {
	switch ph {
	case phaseSweeping:
		return badge.TonePrimary
	case phaseResult:
		return badge.ToneSuccess
	case phaseFailed:
		return badge.ToneError
	case phaseCancelled:
		return badge.ToneWarning
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
		Subject:   "app.terrainscope.sweep.phase",
		SourceApp: string(manifest.Id),
	}
	inst.phaseChip.Opts.Summary = func() {
		metric(statusLabel(inst.stage, inst.sweep.Running()), tipPhase)
	}
	inst.phaseChip.Render()
}

// tipPhase explains the chip.
const tipPhase = "Where the sweep stands: the points placed so far, a sweep in flight, or what the last one left. Click for the graph and the history."

// phaseLine is the phase with what it holds, for the agent's view.
func (inst *App) phaseLine() (s string) {
	ph := inst.observePhase()
	switch ph {
	case phaseResult:
		if r := inst.result; r != nil {
			return fmt.Sprintf("%s: %d rays, %d samples", ph, len(r.ens.Nominal.Rays), r.ens.Samples)
		}
	case phaseFailed:
		if inst.sweepErr != nil {
			return ph.String() + ": " + inst.sweepErr.Error()
		}
	}
	return ph.String()
}
