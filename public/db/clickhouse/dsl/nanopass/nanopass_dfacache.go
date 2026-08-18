package nanopass

import (
	"sync/atomic"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

// The bounded DFA cache ADR-0084 introduced lived here as a private type with
// one holder per grammar. ADR-0196 §SD3 moved both: the type to antlr4utils
// (it is grammar-agnostic), the holders to the grammar packages (env parses
// grammar1 too and cannot import nanopass, since nanopass imports env). What
// stays here is the tuning surface and the stats, which is what callers knew
// about — the names, positions and semantics below are unchanged.

// MaxDFAStates is the retained DFA-state count above which a grammar's cache is
// rebuilt. The empirical per-state weight is ~5–11 KB, so the default bounds
// the cache at roughly 40–90 MB per grammar; the templated-SQL plateau is a few
// hundred states, so this leaves ~17x headroom before a reset ever triggers.
// Set it before the first parse to tune the memory/re-warm trade-off; lower it
// for a tighter memory bound, raise it to tolerate richer legitimate diversity.
var MaxDFAStates = antlr4utils.DefaultMaxDFAStates

// DFACheckInterval is how many parses occur between cache-size measurements.
// Measuring takes a brief exclusive lock that drains in-flight parses, so it is
// amortised over this many parses; the cost is a worst-case overshoot of about
// DFACheckInterval parses' worth of growth above MaxDFAStates before a reset.
var DFACheckInterval = antlr4utils.DefaultDFACheckInterval

// DFACacheStat reports the state of one grammar's bounded DFA cache.
//
// It mirrors [antlr4utils.DFACacheStat] field for field rather than aliasing
// it: CS008 forbids type aliases, and this has been nanopass's own exported
// shape since ADR-0084 — a caller should not have to learn where the holder
// moved to.
type DFACacheStat struct {
	// States is the retained DFA-state count at the last measurement (taken
	// every DFACheckInterval parses), or 0 if no measurement has run yet.
	States int64
	// Resets is the cumulative number of times the cache was rebuilt because
	// it exceeded MaxDFAStates.
	Resets int64
}

// syncDFALimits pushes the package-level tunables into the shared holders. It
// runs on the parse path rather than in an init so that setting the vars keeps
// working at any time, as it always has; two atomic stores against a path that
// allocates kilobytes is not worth a cleverer arrangement.
func syncDFALimits() {
	grammar1.SharedDFA.SetLimits(MaxDFAStates, DFACheckInterval)
	grammar2.SharedDFA.SetLimits(MaxDFAStates, DFACheckInterval)
}

// DFACacheStats returns the current bounded-DFA-cache state for Grammar1 (used
// by Parse) and Grammar2 (used by ParseCanonical). Intended for monitoring a
// long-running parser: a steadily rising Resets count means the workload's
// structural diversity exceeds MaxDFAStates and the cache is sawtoothing.
//
// The Grammar1 figure now covers every seam that parses grammar1, including
// env.Extract's body scan and play's syntax-error probe, which before
// ADR-0196 §SD3 used the grammar's unbounded package global and were invisible
// here.
func DFACacheStats() (g1, g2 DFACacheStat) {
	return DFACacheStat(grammar1.SharedDFA.Stat()), DFACacheStat(grammar2.SharedDFA.Stat())
}

// Two-stage accounting (ADR-0196 §SD4). A parse that SLL rejects costs SLL
// *and* LL, so a workload where SLL usually fails is slower than a plain LL
// parser would be. These counters are how that shows up instead of being
// guessed at.
//
// They are kept per grammar because the two seams mean different things. A
// grammar1 fallback is a miss — SLL could not handle a statement the pipeline
// considers valid. A grammar2 fallback is usually the system working: rejecting
// non-canonical SQL is what ParseCanonical is for, and a rejection costs both
// stages by construction. Summing them would bury the signal that matters.
var (
	g1Hits      atomic.Int64
	g1Fallbacks atomic.Int64
	g2Hits      atomic.Int64
	g2Fallbacks atomic.Int64
)

// PredictionStat reports how a set of parses split between the SLL fast path
// and the LL fallback.
type PredictionStat struct {
	// Hits is the number of parses SLL resolved on its own.
	Hits int64
	// Fallbacks is the number of parses that were re-run under full-context LL
	// because the SLL attempt reported a diagnostic. Both clean LL parses and
	// genuine syntax errors land here — a syntax error can only be established
	// by LL.
	Fallbacks int64
}

// PredictionStats returns the cumulative two-stage split for Grammar1 (used by
// Parse) and Grammar2 (used by ParseCanonical).
//
// A grammar1 fallback ratio that stays near zero means SLL is carrying the
// workload and ADR-0196 is paying for itself. A ratio that rises and stays high
// means the opposite: those parses pay both stages, and the grammar repair
// ADR-0196 §SD5 defers has become worth its cost. Read the grammar2 ratio
// against how much non-canonical SQL the caller expects to be refused, not
// against zero.
func PredictionStats() (g1, g2 PredictionStat) {
	return PredictionStat{Hits: g1Hits.Load(), Fallbacks: g1Fallbacks.Load()},
		PredictionStat{Hits: g2Hits.Load(), Fallbacks: g2Fallbacks.Load()}
}

// recordTwoStage folds one parse's outcome into the counters.
func recordTwoStage(hits, fallbacks *atomic.Int64, fellBack bool) {
	if fellBack {
		fallbacks.Add(1)
		return
	}
	hits.Add(1)
}
