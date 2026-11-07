package writingstylescope

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// phaseE is where the analysis stands: whether there is a result, whether
// it still describes the panes, and whether a sweep is replacing it. It is
// derived, never driven (keelson-app T1): each frame observes the phase
// from the runner, the cached result and the inputs it was computed from
// (observePhase) and mirrors it. The rules below draw the graph the chip
// opens and name the gesture behind each edge; an edge they do not declare
// is followed, and graded in the log by mirrorPhase.
type phaseE uint8

const (
	phaseNoResult phaseE = iota // nothing computed yet, or the last sweep refused and was cleared
	phaseSweeping               // a sweep runs
	phaseResult                 // the result describes the panes as they are
	phaseStale                  // the panes or the floor changed since the result
	phaseFailed                 // the last sweep refused; its reason is shown, no result remains
)

var allPhases = []phaseE{phaseNoResult, phaseSweeping, phaseResult, phaseStale, phaseFailed}

func (inst phaseE) String() (s string) {
	switch inst {
	case phaseNoResult:
		return "no result"
	case phaseSweeping:
		return "sweeping"
	case phaseResult:
		return "result"
	case phaseStale:
		return "stale"
	case phaseFailed:
		return "failed"
	}
	return "?"
}

// observePhase derives the phase from this frame's runner and cache. A
// running sweep outranks what it will replace; a refusal leaves no result,
// so failed and stale never compete.
func (inst *App) observePhase() (ph phaseE) {
	switch {
	case inst.sweep.Running():
		return phaseSweeping
	case inst.err != nil:
		return phaseFailed
	case inst.res == nil:
		return phaseNoResult
	case inst.stale():
		return phaseStale
	}
	return phaseResult
}

// mirrorPhase moves the machine to the phase this frame observed. A frame
// samples the runner, so a sweep that launches and lands between two frames
// (the inline sweep of a host without tasks always does) shows as an edge
// that skips sweeping; that is the declared path taken unwatched and logs at
// debug. An observation no declared path reaches contradicts the model and
// logs a warning.
func (inst *App) mirrorPhase() {
	m := inst.phaseMachine
	cur, obs := m.Current(), inst.observePhase()
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := inst.logger.Warn()
	msg := "writingstylescope: the phase moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = inst.logger.Debug()
		msg = "writingstylescope: the phase skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// newPhaseMachine declares the analysis's lifecycle: Analyze sweeps, a
// sweep lands or refuses, an edit makes a result stale, a cancel keeps the
// result the sweep would have replaced.
func newPhaseMachine() (m *fsmview.Machine[phaseE]) {
	m = fsmview.NewMachine(phaseNoResult, 64, fsmview.MachineOptions[phaseE]{
		Label:      func(ph phaseE) string { return ph.String() },
		StateOrder: allPhases,
		StateColor: phaseColor,
	})
	m.AddRule(phaseNoResult, phaseSweeping).
		AddRule(phaseSweeping, phaseResult, phaseStale, phaseFailed, phaseNoResult).
		AddRule(phaseResult, phaseStale, phaseSweeping).
		AddRule(phaseStale, phaseResult, phaseSweeping).
		AddRule(phaseFailed, phaseSweeping).
		EdgeLabel(phaseNoResult, phaseSweeping, "Analyze").
		EdgeLabel(phaseSweeping, phaseResult, "lands").
		EdgeLabel(phaseSweeping, phaseStale, "lands after an edit, or Cancel keeps a stale result").
		EdgeLabel(phaseSweeping, phaseFailed, "refuses").
		EdgeLabel(phaseSweeping, phaseNoResult, "Cancel with nothing to keep").
		EdgeLabel(phaseResult, phaseStale, "the text or the floor changes").
		EdgeLabel(phaseResult, phaseSweeping, "Analyze").
		EdgeLabel(phaseStale, phaseResult, "the edit is undone").
		EdgeLabel(phaseStale, phaseSweeping, "Analyze").
		EdgeLabel(phaseFailed, phaseSweeping, "Analyze")
	return
}

// phaseColor tints the graph's nodes: the running phase in the accent, the
// outcomes by severity, the empty one muted.
func phaseColor(ph phaseE, _ bool) styletokens.RGBA8 {
	switch ph {
	case phaseSweeping:
		return styletokens.AccentDefault
	case phaseResult:
		return styletokens.SuccessDefault
	case phaseStale:
		return styletokens.WarningDefault
	case phaseFailed:
		return styletokens.ErrorDefault
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
	case phaseStale:
		return badge.ToneWarning
	case phaseFailed:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// tipPhase explains the chip.
const tipPhase = "Where the analysis stands: no result yet, a sweep in flight, a result that describes the panes, one the text has moved on from, or a refusal. Click for the graph and the history."

// renderStatusRow is the window's instrument panel (keelson-app L7): the
// phase chip with its summary, above the tabs so every tab shares it. The
// tour stage never mounts and draws the tab bodies alone, so it has no row.
func (inst *App) renderStatusRow() {
	if inst.phaseChip == nil {
		return
	}
	for range c.PanelTopInside(inst.ids.PrepareStr("status")).Resizable(false).KeepIter() {
		for range c.Horizontal().KeepIter() {
			inst.phaseChip.Opts.Provenance = inspector.Provenance{
				Subject:   "app.writingstylescope.analysis.phase",
				SourceApp: string(manifest.Id),
			}
			inst.phaseChip.Opts.Summary = func() { metric(inst.phaseLine(), tipPhase) }
			inst.phaseChip.Render()
		}
	}
}

// phaseLine is the chip's summary: what the phase holds, in numbers.
func (inst *App) phaseLine() (s string) {
	switch inst.observePhase() {
	case phaseNoResult:
		return "paste two documents and press Analyze"
	case phaseSweeping:
		return "measuring every section pair…"
	case phaseFailed:
		return "cannot sweep: " + inst.err.Error()
	}
	r := inst.res
	s = fmt.Sprintf("%d × %d sections, %d pairs", r.Rows(), r.Cols(), r.Rows()*r.Cols())
	if inst.stale() {
		s += "; the text or the floor changed since"
	}
	return
}
