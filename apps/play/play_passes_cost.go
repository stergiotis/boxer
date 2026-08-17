package play

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
)

// play_passes_cost.go is the ADR-0192 consumer side: it turns the durations
// passreg now hangs on every ApplyObservation into the lines the Passes and
// Diagnostics tabs draw.
//
// The reason a rewrite is worth profiling at all is that nanopass.Sequence
// hands each child the previous child's output *string*, so every pass
// re-parses the statement from text and a play Run parses one statement on the
// order of 34 times. The cost is superlinear in expression complexity — 681 B
// of SQL measured 91 ms through this pipeline, 2.8 KB measured 525 ms, 9.4 KB
// measured 3.9 s — so it arrives as a cliff, and an author editing a buffer has
// no other signal that they just crossed it.

const (
	// rewriteCostWarn is the whole-buffer mark above which the rewrite is
	// called out. It sits between the 91 ms a 681 B buffer measures and the
	// 525 ms a 2.8 KB one does, so it separates buffers that are on the
	// superlinear part of the curve from buffers that are not.
	//
	// Declared here rather than taken from the environment on purpose: it is a
	// property of what a person perceives as a stall, not of a deployment, and
	// a knob would make the warning mean something different per session.
	rewriteCostWarn = 250 * time.Millisecond

	// rewriteCostStepWarn is the mark above which one unit or one sub-pass is
	// worth naming in its own right — a step this expensive is the one to look
	// at first, whatever the total.
	rewriteCostStepWarn = 100 * time.Millisecond

	// rewriteCostFloor is the shortest step the breakdown lists individually.
	// Below it a step is noise against the measurement itself, and listing
	// three dozen of them would bury the two that matter.
	rewriteCostFloor = time.Millisecond
)

// rewriteTotalCost sums what the whole client-side rewrite of one buffer cost:
// the registry stage plus play's own steps, which is the wall time between the
// author's buffer settling and the statement being ready to ship.
func rewriteTotalCost(obs []passreg.ApplyObservation) (total time.Duration) {
	for _, o := range obs {
		total += o.Dur
	}
	return
}

// formatCostDur renders one measured duration. Millisecond resolution matches
// how the rest of play prints elapsed times; anything under that reads as
// "<1ms" rather than Go's "0s", which would be indistinguishable from a step
// that did not run.
func formatCostDur(d time.Duration) string {
	if d <= 0 {
		return "0ms"
	}
	if d < time.Millisecond {
		return "<1ms"
	}
	return d.Round(time.Millisecond).String()
}

// isSlowRewriteUnit reports whether one unit is expensive enough to tint on the
// schematic and name in the prose.
func isSlowRewriteUnit(o passreg.ApplyObservation) bool {
	return o.Dur >= rewriteCostStepWarn
}

// costTreeLines flattens a measured pass tree into indented display lines, two
// spaces per level, in the order the passes ran. Each line carries what that
// invocation cost, its fixed-point iteration count where it looped, and — the
// point of the exercise — whether it changed anything for the price. A pass
// that re-parsed the statement and rewrote nothing is the cheapest thing to
// remove from a pipeline, and it is invisible without this.
//
// The root is skipped: it is the unit the caller already labelled, so the lines
// start at its members. A leaf unit therefore produces no lines at all.
func costTreeLines(cost nanopass.StepCost) (lines []string) {
	for _, ch := range cost.Children {
		ch.Walk(func(s nanopass.StepCost, depth int) {
			lines = append(lines, strings.Repeat("  ", depth)+costStepLine(s))
		})
	}
	return
}

// costExpandChildren is how many sub-passes of the costliest unit the
// waterfall draws individually before collapsing the rest into one summary
// row. Three is what fits without the unit rows losing their prominence — the
// chart is meant to be read at a glance, and a fully expanded canonicalizer is
// twelve more rows of mostly-identical bars.
const costExpandChildren = 3

// rewriteWaterfall lays a trace out as a staggered timeline: the units ran in
// sequence, so each bar starts where the previous ended and the reader finds
// the expensive phase by looking rather than by comparing numbers.
//
// The costliest unit is expanded in place when it is worth naming — its top
// sub-passes as indented bars, the remainder as one bar-less summary row,
// which is where "seven of these rewrote nothing" becomes visible.
func rewriteWaterfall(obs []passreg.ApplyObservation) (bars []costBar, span time.Duration) {
	worst := ""
	var worstDur time.Duration
	for _, o := range obs {
		if o.Dur > worstDur {
			worst, worstDur = o.Name, o.Dur
		}
	}
	expand := worstDur >= rewriteCostStepWarn

	var at time.Duration
	for _, o := range obs {
		if o.Dur <= 0 {
			// A declined factory never ran; drawing it at zero width would
			// claim it took part.
			continue
		}
		bars = append(bars, costBar{
			Label: o.Name, Start: at, Dur: o.Dur, Tone: unitTone(o),
			Note: iterNote(o.Cost.Iters),
		})
		if expand && o.Name == worst {
			bars = append(bars, expandedChildren(o, at)...)
		}
		at += o.Dur
	}
	return bars, at
}

// unitTone maps one unit's outcome onto the chart's colour vocabulary.
func unitTone(o passreg.ApplyObservation) costToneE {
	switch {
	case o.Outcome == passreg.ApplyOutcomeSkipped:
		return costToneFailed
	case o.Changed:
		return costToneRewrote
	}
	return costToneUnchanged
}

// expandedChildren is the costliest unit's internals as indented bars, in the
// order they ran, capped at costExpandChildren with the rest folded into a
// bar-less summary row. The remainder gets no bar on purpose: those passes are
// interleaved in time with the ones above, so any single span drawn for them
// would be a fiction.
func expandedChildren(o passreg.ApplyObservation, unitStart time.Duration) (bars []costBar) {
	type child struct {
		step  nanopass.StepCost
		start time.Duration
	}
	var kids []child
	at := unitStart
	for _, ch := range o.Cost.Children {
		kids = append(kids, child{step: ch, start: at})
		at += ch.Dur
	}
	// Rank by cost to pick which to show, but emit in RUN order so the
	// staggering still reads left to right.
	ranked := make([]child, len(kids))
	copy(ranked, kids)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].step.Dur > ranked[j].step.Dur })
	shown := make(map[string]bool, costExpandChildren)
	for i := 0; i < len(ranked) && i < costExpandChildren; i++ {
		shown[ranked[i].step.Name] = true
	}

	var restN int
	var restDur time.Duration
	var restInert int
	for _, k := range kids {
		if shown[k.step.Name] {
			bars = append(bars, costBar{
				Label: k.step.Name, Start: k.start, Dur: k.step.Dur, Depth: 1,
				Tone: stepTone(k.step), Note: iterNote(k.step.Iters),
			})
			continue
		}
		restN++
		restDur += k.step.Dur
		if k.step.Err == nil && !k.step.Changed {
			restInert++
		}
	}
	if restN > 0 {
		label := fmt.Sprintf("… %d more", restN)
		if restInert > 0 {
			label += fmt.Sprintf(", %d rewrote nothing", restInert)
		}
		bars = append(bars, costBar{Label: label, Dur: restDur, Depth: 1})
	}
	return
}

func stepTone(s nanopass.StepCost) costToneE {
	switch {
	case s.Err != nil:
		return costToneFailed
	case s.Changed:
		return costToneRewrote
	}
	return costToneUnchanged
}

// iterNote renders a fixed-point loop's iteration count. Silent for a single
// pass: only a loop is worth naming, and "×1" on a dozen rows is noise.
func iterNote(iters int) string {
	if iters > 1 {
		return "×" + strconv.Itoa(iters)
	}
	return ""
}

// runPhaseBars splits one Run into the three spans a reader is choosing
// between: what this process spent compiling the statement, what the server
// spent answering, and what neither accounts for (the round trip and the Arrow
// decode).
//
// total is the client-observed run duration, measured around the call that
// itself performs the compile — so compile is INSIDE it, and the remainder is
// what is left after both named spans. Clamped at zero: compile comes from the
// trace's own rewrite rather than from the shipped one (ADR-0192 §SD4), so the
// two measurements can disagree by a few milliseconds and must not be allowed
// to draw a negative bar.
func runPhaseBars(compile time.Duration, server time.Duration, total time.Duration) (bars []costBar, span time.Duration) {
	bars = append(bars, costBar{Label: "compile", Dur: compile, Tone: costToneClient})
	at := compile
	if server > 0 {
		bars = append(bars, costBar{Label: "server", Start: at, Dur: server, Tone: costToneServer})
		at += server
	}
	if rest := total - at; rest >= rewriteCostFloor {
		bars = append(bars, costBar{Label: "transfer + decode", Start: at, Dur: rest, Tone: costToneTransfer})
		at += rest
	}
	return bars, max(at, total)
}

// costStepCount is costTreeLines' length without building the strings — the
// panes want the number every frame and the text only for the selection.
func costStepCount(cost nanopass.StepCost) (n int) {
	for _, ch := range cost.Children {
		ch.Walk(func(nanopass.StepCost, int) { n++ })
	}
	return
}

// costStepLine is one measured invocation as text: name, cost, and the verdict
// on what it bought.
func costStepLine(s nanopass.StepCost) string {
	var b strings.Builder
	b.WriteString(s.Name)
	b.WriteString(" · ")
	b.WriteString(formatCostDur(s.Dur))
	if s.Iters > 1 {
		b.WriteString(" ×" + strconv.Itoa(s.Iters) + " iters")
	}
	switch {
	case s.Err != nil:
		b.WriteString(" · failed")
	case s.Changed:
		b.WriteString(" · rewrote")
	default:
		b.WriteString(" · unchanged")
	}
	return b.String()
}

// costUnaccounted names the part of a unit's cost that no listed member
// explains — the unit's Dur less the members' — so the breakdown adds up
// instead of quietly losing a fifth of the time.
//
// Two things live in there and both are real. The env round trip (extracting
// the SET prelude and integrating the rewritten body) sits outside the pass
// tree entirely; on a 9 KB buffer it measured ~344 ms against a 1.99 s unit.
// The rest is the composite's own work between its members. A large figure here
// is a different problem from a slow member, which is why it is stated rather
// than folded into the parent's number.
//
// Empty for a leaf unit, where there are no members to be outside of.
func costUnaccounted(o passreg.ApplyObservation) string {
	if len(o.Cost.Children) == 0 {
		return ""
	}
	rest := o.Dur
	for _, ch := range o.Cost.Children {
		rest -= ch.Dur
	}
	if rest < rewriteCostFloor {
		return ""
	}
	return fmt.Sprintf(
		"%s of that is outside the members listed above — the env round trip plus the composite's own work",
		formatCostDur(rest))
}

// wastedStepCount counts the invocations inside one unit that cost real time
// and rewrote nothing. It is the number behind the standing suspicion about
// this pipeline: most of a canonicalisation pass's members re-parse the
// statement and leave it exactly as they found it.
func wastedStepCount(cost nanopass.StepCost) (n int, wasted time.Duration) {
	for _, ch := range cost.Children {
		ch.Walk(func(s nanopass.StepCost, _ int) {
			if len(s.Children) == 0 && s.Err == nil && !s.Changed && s.Dur >= rewriteCostFloor {
				n++
				wasted += s.Dur
			}
		})
	}
	return
}
