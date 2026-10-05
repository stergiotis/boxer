package play

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The sampling ladder (ADR-0096 SD10, taken up 2026-10-01): a settled view is
// drawn first from the most-sampled table and then refined, one level at a
// time, towards the full one, on the same lane and under the same vp_*
// params. Each level supersedes the last by the lane's (SQL, params) key and
// the last-good raster stays on screen while the next one loads, so the
// first picture arrives at the coarse level's cost. A pan or a control
// change starts again at the coarsest level, and its run cancels whatever
// level was in flight server-side (SD9).
//
// Levels come from the table name, by the convention the ADS-B schema uses:
// `<base>_sample100` → `<base>_sample10` → `<base>`, starting at the table the
// source control names. Each level's `sampling` is its own factor, which is
// what the brightness normaliser divides by, so the levels agree in
// brightness. A derived level the server does not have (UNKNOWN_TABLE) is
// dropped and remembered; the table the reader typed is never dropped.

// mapLadderBudget is how long one level may take before the ladder stops
// climbing on its own; Refresh climbs regardless.
const mapLadderBudget = 3 * time.Second

// mapLadderFast is how quickly a level must have answered last time for a
// restarted ladder to start there, skipping the coarser levels below it: on
// a source that fast, the coarse levels add their own cost before a picture
// the full level would have drawn almost as soon (the map-tile-addressing
// trial measured about 45 % on the demo slice).
const mapLadderFast = 300 * time.Millisecond

// mapLadderFactors are the sample factors the convention derives, coarsest
// first.
var mapLadderFactors = [...]uint32{100, 10}

var mapSampleSuffixRe = regexp.MustCompile(`^(.+)_sample([0-9]+)$`)

// mapLevel is one rung: the table it reads and that table's sample factor.
// derived marks a level the convention supplied rather than the reader.
type mapLevel struct {
	table    string
	sampling uint32
	derived  bool
}

// label names a level for the status line.
func (inst mapLevel) label() string {
	if inst.sampling <= 1 {
		return "full table"
	}
	return fmt.Sprintf("%s %% sample", strconv.FormatFloat(100/float64(inst.sampling), 'g', 3, 64))
}

// mapLadderLevels derives the ladder for the source table. A source that is
// not a plain (optionally database-qualified) name — a table function, a
// subquery — or refine off gives one level at the manual sampling.
func mapLadderLevels(table string, manual uint32, refine bool, missing map[string]bool) (levels []mapLevel) {
	if !refine || !isPlainTableName(table) {
		return []mapLevel{{table: table, sampling: manual}}
	}
	base, n := table, uint32(0)
	if m := mapSampleSuffixRe.FindStringSubmatch(table); m != nil {
		if u, err := strconv.ParseUint(m[2], 10, 32); err == nil && u > 1 {
			base, n = m[1], uint32(u)
		}
	}
	if n > 0 {
		levels = append(levels, mapLevel{table: table, sampling: n})
	}
	for _, f := range mapLadderFactors {
		if n != 0 && f >= n {
			continue
		}
		t := base + "_sample" + strconv.FormatUint(uint64(f), 10)
		if !missing[t] {
			levels = append(levels, mapLevel{table: t, sampling: f, derived: true})
		}
	}
	if n == 0 || !missing[base] {
		levels = append(levels, mapLevel{table: base, sampling: 1, derived: n > 0})
	}
	return
}

var mapPlainTableRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

func isPlainTableName(s string) bool { return mapPlainTableRe.MatchString(s) }

// isUnknownTable recognises ClickHouse's UNKNOWN_TABLE (Code 60) in a run's
// error text.
func isUnknownTable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Code: 60.") || strings.Contains(msg, "(UNKNOWN_TABLE)")
}

// mapLadder is the panel's ladder state; render-thread only.
type mapLadder struct {
	levels []mapLevel
	level  int
	// inputs identifies what the ladder was built for (viewport, levels,
	// render); a settle with the same inputs leaves the ladder where it is.
	inputs string
	// stopped says a level overran mapLadderBudget; noBudget (set by
	// Refresh) climbs past the budget until the inputs change.
	stopped  bool
	noBudget bool
	// stoppedAfter is how long the level that stopped the climb took.
	stoppedAfter time.Duration
	missing      map[string]bool
	// fresh marks a ladder just restarted, for the raster memo to move it to
	// the finest level it already holds.
	fresh bool
	// lastElapsed is how long each level's table took the last time its
	// result landed, which picks where a restarted ladder starts.
	lastElapsed map[string]time.Duration
}

// reset starts the ladder over when the inputs changed, and reports whether
// they did.
func (inst *mapLadder) reset(inputs string, levels []mapLevel) (changed bool) {
	if inputs == inst.inputs {
		return false
	}
	inst.inputs, inst.levels, inst.level = inputs, levels, inst.startLevel(levels)
	inst.stopped, inst.stoppedAfter = false, 0
	inst.fresh = true
	return true
}

// startLevel is where a restarted ladder begins: the finest level whose
// table last answered within mapLadderFast, or the coarsest when none has —
// a level never run, or one that ran slow, is measured from the bottom
// again.
func (inst *mapLadder) startLevel(levels []mapLevel) int {
	for i := len(levels) - 1; i > 0; i-- {
		if d, ok := inst.lastElapsed[levels[i].table]; ok && d <= mapLadderFast {
			return i
		}
	}
	return 0
}

// current is the level being demanded.
func (inst *mapLadder) current() mapLevel { return inst.levels[inst.level] }

// served decides what to do once the current level's result has landed:
// climb to the next level unless it was the last, or the level overran the
// budget. It reports whether the level moved.
func (inst *mapLadder) served(elapsed time.Duration) (moved bool) {
	if inst.lastElapsed == nil {
		inst.lastElapsed = make(map[string]time.Duration)
	}
	inst.lastElapsed[inst.current().table] = elapsed
	if inst.level+1 >= len(inst.levels) || inst.stopped {
		return false
	}
	if elapsed > mapLadderBudget && !inst.noBudget {
		inst.stopped, inst.stoppedAfter = true, elapsed
		return false
	}
	inst.level++
	return true
}

// dropMissing removes the current level after the server said its table
// does not exist, when the convention supplied it, and reports whether it
// did. The ladder stays at the same index, which now names the next level.
func (inst *mapLadder) dropMissing() (dropped bool) {
	cur := inst.current()
	if !cur.derived || len(inst.levels) < 2 {
		return false
	}
	if inst.missing == nil {
		inst.missing = map[string]bool{}
	}
	inst.missing[cur.table] = true
	inst.levels = append(inst.levels[:inst.level:inst.level], inst.levels[inst.level+1:]...)
	if inst.level >= len(inst.levels) {
		inst.level = len(inst.levels) - 1
	}
	return true
}

// status is the ladder's part of the status line: the level on screen, and
// why it is not climbing when it stopped early. Empty for a one-level ladder.
func (inst *mapLadder) status(onScreen mapLevel, loading bool) string {
	if len(inst.levels) < 2 {
		return ""
	}
	s := onScreen.label()
	switch {
	case inst.stopped:
		s += fmt.Sprintf(" · refinement paused (%s took %.1f s) — Refresh refines", onScreen.label(), inst.stoppedAfter.Seconds())
	case loading && inst.level > 0:
		s += " · refining to " + inst.current().label()
	}
	return s
}
