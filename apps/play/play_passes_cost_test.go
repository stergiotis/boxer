package play

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stretchr/testify/assert"
)

// play_passes_cost_test.go pins ADR-0192's consumer side. The durations are
// synthetic throughout — the panes are fed observations rather than a real
// rewrite — so nothing here depends on machine speed.

// barLabels renders a waterfall as "<indent><label>@<start>+<dur>" strings, so
// a whole chart's shape — order, nesting, and the staggering that is the point
// of the form — is one assertion.
func barLabels(bars []costBar) (out []string) {
	for _, b := range bars {
		out = append(out, strings.Repeat("  ", b.Depth)+b.Label+
			"@"+formatCostDur(b.Start)+"+"+formatCostDur(b.Dur))
	}
	return
}

// TestRewriteWaterfallStaggersInRunOrder is the chart's core claim: the passes
// ran in sequence, so each bar starts where the previous one ended. A bar drawn
// at the wrong offset would put the blame on the wrong pass.
func TestRewriteWaterfallStaggersInRunOrder(t *testing.T) {
	obs := []passreg.ApplyObservation{
		{Name: "extract-params", Dur: 20 * time.Millisecond, Outcome: passreg.ApplyOutcomeApplied},
		{Name: "canonicalize", Dur: 300 * time.Millisecond, Outcome: passreg.ApplyOutcomeApplied, Changed: true},
		{Name: "set-format", Dur: 50 * time.Millisecond, Outcome: passreg.ApplyOutcomeApplied, Changed: true},
	}
	bars, span := rewriteWaterfall(obs)
	assert.Equal(t, []string{
		"extract-params@0ms+20ms",
		"canonicalize@20ms+300ms",
		"set-format@320ms+50ms",
	}, barLabels(bars))
	assert.Equal(t, 370*time.Millisecond, span, "the span is the whole rewrite")
}

// TestRewriteWaterfallDropsUnitsThatNeverRan — a declined factory has no span,
// and a zero-width bar on the timeline would claim it took part.
func TestRewriteWaterfallDropsUnitsThatNeverRan(t *testing.T) {
	bars, span := rewriteWaterfall([]passreg.ApplyObservation{
		{Name: "ran", Dur: 5 * time.Millisecond, Outcome: passreg.ApplyOutcomeApplied},
		{Name: "declined", Outcome: passreg.ApplyOutcomeDeclined},
	})
	assert.Equal(t, []string{"ran@0ms+5ms"}, barLabels(bars))
	assert.Equal(t, 5*time.Millisecond, span)
}

// TestRewriteWaterfallExpandsTheCostliestUnit covers the second tier: the
// dominant unit opens in place, its top sub-passes keep their real offsets, and
// the remainder collapses into one row that carries the finding.
func TestRewriteWaterfallExpandsTheCostliestUnit(t *testing.T) {
	heavy := passreg.ApplyObservation{
		Name: "CanonicalizeFull", Dur: 300 * time.Millisecond,
		Outcome: passreg.ApplyOutcomeApplied, Changed: true,
		Cost: nanopass.StepCost{Name: "CanonicalizeFull", Dur: 290 * time.Millisecond, Children: []nanopass.StepCost{
			{Name: "big1", Dur: 90 * time.Millisecond, Changed: true},
			{Name: "small1", Dur: 10 * time.Millisecond},
			{Name: "big2", Dur: 80 * time.Millisecond, Iters: 2},
			{Name: "small2", Dur: 12 * time.Millisecond},
			{Name: "big3", Dur: 70 * time.Millisecond},
		}},
	}
	bars, _ := rewriteWaterfall([]passreg.ApplyObservation{
		{Name: "extract-params", Dur: 10 * time.Millisecond, Outcome: passreg.ApplyOutcomeApplied},
		heavy,
	})
	assert.Equal(t, []string{
		"extract-params@0ms+10ms",
		"CanonicalizeFull@10ms+300ms",
		"  big1@10ms+90ms",
		"  big2@110ms+80ms",
		"  big3@202ms+70ms",
		"  … 2 more, 2 rewrote nothing@0ms+22ms",
	}, barLabels(bars), "top three by cost, emitted in run order, remainder folded")

	// The children keep the tones that make the chart readable at a glance.
	assert.Equal(t, costToneRewrote, bars[2].Tone)
	assert.Equal(t, costToneUnchanged, bars[4].Tone)
	assert.Equal(t, "×2", bars[3].Note, "a fixed-point loop names its iteration count")
	assert.Equal(t, costToneInvalid, bars[5].Tone,
		"the collapsed remainder is not a contiguous span, so it gets no bar")
}

// TestRewriteWaterfallLeavesACheapUnitClosed — expanding a unit that is not the
// problem would bury the units that are.
func TestRewriteWaterfallLeavesACheapUnitClosed(t *testing.T) {
	bars, _ := rewriteWaterfall([]passreg.ApplyObservation{{
		Name: "cheap", Dur: 20 * time.Millisecond, Outcome: passreg.ApplyOutcomeApplied,
		Cost: nanopass.StepCost{Name: "cheap", Children: []nanopass.StepCost{{Name: "kid", Dur: 5 * time.Millisecond}}},
	}})
	assert.Equal(t, []string{"cheap@0ms+20ms"}, barLabels(bars))
}

// TestRunPhaseBarsSplitOneRun is the tier that answers "is this my fault or the
// server's". Compile is measured INSIDE the run's elapsed, so the remainder is
// what neither named span accounts for.
func TestRunPhaseBarsSplitOneRun(t *testing.T) {
	bars, span := runPhaseBars(500*time.Millisecond, 90*time.Millisecond, 620*time.Millisecond)
	assert.Equal(t, []string{
		"compile@0ms+500ms",
		"server@500ms+90ms",
		"transfer + decode@590ms+30ms",
	}, barLabels(bars))
	assert.Equal(t, 620*time.Millisecond, span)
}

// TestRunPhaseBarsNeverDrawNegative guards the seam in ADR-0192 §SD4: compile
// comes from the trace's own rewrite rather than the shipped one, so the two
// measurements can disagree and must not produce a backwards bar.
func TestRunPhaseBarsNeverDrawNegative(t *testing.T) {
	bars, span := runPhaseBars(500*time.Millisecond, 90*time.Millisecond, 400*time.Millisecond)
	assert.Equal(t, []string{"compile@0ms+500ms", "server@500ms+90ms"}, barLabels(bars),
		"the remainder is negative, so it is omitted rather than drawn")
	assert.Equal(t, 590*time.Millisecond, span, "the span still covers every bar drawn")
}

// TestRunPhaseBarsOmitAnUnreportedServer — chlocal and the mocks report no
// summary, and a zero-length server bar would read as an instant server.
func TestRunPhaseBarsOmitAnUnreportedServer(t *testing.T) {
	bars, _ := runPhaseBars(50*time.Millisecond, 0, 80*time.Millisecond)
	assert.Equal(t, []string{"compile@0ms+50ms", "transfer + decode@50ms+30ms"}, barLabels(bars))
}

// TestRewriteCostThresholdsArePinned guards the two numbers the warning means.
// They are a claim about the measured shape of this pipeline (91 ms at 681 B,
// 525 ms at 2.8 KB), so moving one silently would change what the pane asserts
// about a buffer without anyone deciding to.
func TestRewriteCostThresholdsArePinned(t *testing.T) {
	assert.Equal(t, 250*time.Millisecond, rewriteCostWarn,
		"the whole-buffer mark sits between the 681 B and 2.8 KB corpus measurements")
	assert.Equal(t, 100*time.Millisecond, rewriteCostStepWarn)
	assert.Equal(t, time.Millisecond, rewriteCostFloor)
	assert.Less(t, rewriteCostFloor, rewriteCostStepWarn)
	assert.Less(t, rewriteCostStepWarn, rewriteCostWarn)
}

func TestRewriteTotalCostSumsEveryStep(t *testing.T) {
	obs := []passreg.ApplyObservation{
		{Name: "extract-params", Dur: 2 * time.Millisecond},
		{Name: "canonicalize", Dur: 300 * time.Millisecond},
		{Name: "declined-factory", Outcome: passreg.ApplyOutcomeDeclined},
	}
	assert.Equal(t, 302*time.Millisecond, rewriteTotalCost(obs))
	assert.GreaterOrEqual(t, rewriteTotalCost(obs), rewriteCostWarn, "this buffer warns")
}

func TestFormatCostDur(t *testing.T) {
	// Sub-millisecond reads as "<1ms" rather than Go's "0s", which would be
	// indistinguishable from a step that never ran.
	assert.Equal(t, "0ms", formatCostDur(0))
	assert.Equal(t, "<1ms", formatCostDur(400*time.Microsecond))
	assert.Equal(t, "349ms", formatCostDur(349*time.Millisecond))
	assert.Equal(t, "3.412s", formatCostDur(3412*time.Millisecond))
}

func TestIsSlowRewriteUnit(t *testing.T) {
	assert.True(t, isSlowRewriteUnit(passreg.ApplyObservation{Dur: rewriteCostStepWarn}))
	assert.False(t, isSlowRewriteUnit(passreg.ApplyObservation{Dur: rewriteCostStepWarn - time.Millisecond}))
	assert.False(t, isSlowRewriteUnit(passreg.ApplyObservation{Outcome: passreg.ApplyOutcomeDeclined}))
}

// costTestTree is a composite shaped like the real canonicalizer: several
// members, most of them re-parsing the statement and rewriting nothing.
func costTestTree() nanopass.StepCost {
	return nanopass.StepCost{
		Name: "canonicalize-full",
		Dur:  700 * time.Millisecond,
		Children: []nanopass.StepCost{
			{Name: "Keywords", Dur: 100 * time.Millisecond},
			{Name: "Constructors", Dur: 400 * time.Millisecond, Iters: 3, Changed: true,
				Children: []nanopass.StepCost{{Name: "Ctor", Dur: 380 * time.Millisecond, Changed: true}}},
			{Name: "Quoting", Dur: 150 * time.Millisecond},
		},
	}
}

func TestCostTreeLinesAreIndentedInRunOrder(t *testing.T) {
	assert.Equal(t, []string{
		"Keywords · 100ms · unchanged",
		"Constructors · 400ms ×3 iters · rewrote",
		"  Ctor · 380ms · rewrote",
		"Quoting · 150ms · unchanged",
	}, costTreeLines(costTestTree()))
	assert.Empty(t, costTreeLines(nanopass.StepCost{Name: "leaf", Dur: time.Second}),
		"a leaf unit has no internals to list")
}

func TestCostStepLineNamesTheVerdict(t *testing.T) {
	assert.Equal(t, "p · 5ms · failed",
		costStepLine(nanopass.StepCost{Name: "p", Dur: 5 * time.Millisecond, Err: errors.New("boom")}))
	assert.Equal(t, "p · 5ms · unchanged",
		costStepLine(nanopass.StepCost{Name: "p", Dur: 5 * time.Millisecond}))
	assert.Equal(t, "p · 5ms · rewrote",
		costStepLine(nanopass.StepCost{Name: "p", Dur: 5 * time.Millisecond, Changed: true}))
	assert.Equal(t, "p · 5ms · unchanged",
		costStepLine(nanopass.StepCost{Name: "p", Dur: 5 * time.Millisecond, Iters: 1}),
		"a single iteration is just a call; only a loop is worth naming")
}

// TestCostUnaccounted pins that the breakdown adds up. The members are measured
// inside the pass tree while the unit's Dur is the whole Run, so the env round
// trip sits between them — on the real 9 KB fixture that gap was ~344 ms
// against a 1.99 s unit, which is far too much to leave unnamed.
func TestCostUnaccounted(t *testing.T) {
	// Members sum to 650ms; the unit's whole Run measured 800ms.
	o := passreg.ApplyObservation{Dur: 800 * time.Millisecond, Cost: costTestTree()}
	assert.Contains(t, costUnaccounted(o), "150ms")

	assert.Empty(t, costUnaccounted(passreg.ApplyObservation{
		Dur: time.Second, Cost: nanopass.StepCost{Name: "leaf", Dur: time.Second}}),
		"a leaf has no members to be outside of")
	assert.Empty(t, costUnaccounted(passreg.ApplyObservation{
		Dur: 650*time.Millisecond + 500*time.Microsecond, Cost: costTestTree()}),
		"a sub-millisecond remainder is not worth a line")
}

// TestWastedStepCount is the number behind the standing suspicion about this
// pipeline: passes hand each other text, so one that rewrites nothing still
// pays a full re-parse to find that out.
func TestWastedStepCount(t *testing.T) {
	n, wasted := wastedStepCount(costTestTree())
	assert.Equal(t, 2, n, "Keywords and Quoting cost time and changed nothing")
	assert.Equal(t, 250*time.Millisecond, wasted)

	// A failed step is not "wasted" — it is a different problem, already
	// reported as a skipped rewrite.
	n, _ = wastedStepCount(nanopass.StepCost{
		Name:     "u",
		Children: []nanopass.StepCost{{Name: "broke", Dur: 50 * time.Millisecond, Err: errors.New("boom")}},
	})
	assert.Zero(t, n)
}
