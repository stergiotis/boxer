package nanopass

import (
	"sync"
	"sync/atomic"

	"github.com/antlr4-go/antlr/v4"
)

// The ANTLR runtime memoises adaptive-prediction decisions in a DFA cache that
// each generated grammar holds in a package-level global (decisionToDFA plus a
// PredictionContextCache). The cache keys on token-TYPE sequences, not token
// text, and it never evicts — antlr4-go v4.13.1 exposes no ClearDFA. So varying
// literals and identifiers cost nothing, but structurally novel SQL (deeply or
// variably nested expressions) accumulates DFA states without bound: a fuzz run
// or an ad-hoc-SQL proxy can drive it into the multi-GB range over a long
// process lifetime. See ADR-0084.
//
// dfaCache replaces that shared global with a process-local cache that we can
// rebuild. It is bounded by its actual retained-state count: every
// DFACheckInterval parses, one parse measures the cache and, if it exceeds
// MaxDFAStates, rebuilds it from the immutable ATN. A size threshold (rather
// than a parse count) never fires for the templated/parameterised traffic that
// plateaus at a small state count, so the warm cache is preserved for the
// common case and discarded only under genuine structural novelty — where it
// was providing little reuse anyway.

// MaxDFAStates is the retained DFA-state count above which a grammar's cache is
// rebuilt. The empirical per-state weight is ~5–11 KB, so the default bounds
// the cache at roughly 40–90 MB per grammar; the templated-SQL plateau is a few
// hundred states, so this leaves ~17x headroom before a reset ever triggers.
// Set it before the first parse to tune the memory/re-warm trade-off; lower it
// for a tighter memory bound, raise it to tolerate richer legitimate diversity.
var MaxDFAStates int64 = 8192

// DFACheckInterval is how many parses occur between cache-size measurements.
// Measuring takes a brief exclusive lock that drains in-flight parses, so it is
// amortised over this many parses; the cost is a worst-case overshoot of about
// DFACheckInterval parses' worth of growth above MaxDFAStates before a reset.
var DFACheckInterval int64 = 256

// dfaCache is a process-local, size-bounded ANTLR DFA cache for one grammar.
//
// The RWMutex separates the common read path from the rare reset: parses hold
// the read-lock for the duration of QueryStmt (so they run fully concurrently
// and may mutate the shared DFA under the ATN's own internal mutexes, exactly
// as antlr4-go intends), while a reset takes the write-lock, which drains
// in-flight parses so the state count can be summed and the slice swapped with
// no concurrent mutation.
type dfaCache struct {
	once  sync.Once
	mu    sync.RWMutex
	atn   *antlr.ATN
	d2dfa []*antlr.DFA
	pcc   *antlr.PredictionContextCache

	sinceCheck atomic.Int64 // parses since the last measurement
	lastStates atomic.Int64 // last measured retained-state count (for stats)
	resets     atomic.Int64 // cumulative rebuilds (for stats)
}

// One holder per grammar. The ATN is immutable and shared; only the DFA slice
// and prediction-context cache are per-holder and resettable.
var (
	grammar1DFA dfaCache
	grammar2DFA dfaCache
)

// acquire points a freshly constructed parser at this holder's cache and takes
// the read-lock. The caller must assign the returned simulator to
// parser.Interpreter and call the returned release func when parsing is done
// (defer it — release is panic-safe).
func (c *dfaCache) acquire(p antlr.Parser) (*antlr.ParserATNSimulator, func()) {
	c.once.Do(func() {
		c.atn = p.GetATN() // immutable; captured from the generated interpreter
		c.rebuild()
	})
	c.mu.RLock()
	sim := antlr.NewParserATNSimulator(p, c.atn, c.d2dfa, c.pcc)
	return sim, c.release
}

// release ends a parse and, periodically, measures the cache and rebuilds it if
// it has grown past MaxDFAStates.
func (c *dfaCache) release() {
	c.mu.RUnlock()
	if c.sinceCheck.Add(1) >= DFACheckInterval {
		c.maybeReset()
	}
}

func (c *dfaCache) maybeReset() {
	c.mu.Lock() // drains in-flight parses: no concurrent DFA mutation under this
	defer c.mu.Unlock()
	if c.sinceCheck.Load() < DFACheckInterval {
		return // another goroutine already ran the check
	}
	c.sinceCheck.Store(0)

	var n int64
	for _, d := range c.d2dfa {
		n += int64(d.Len())
	}
	c.lastStates.Store(n)
	if n > MaxDFAStates {
		c.rebuild()
		c.resets.Add(1)
	}
}

// rebuild allocates a fresh, empty DFA slice and prediction-context cache from
// the immutable ATN. Must be called with exclusive ownership of c (inside
// once.Do or while holding c.mu for writing).
func (c *dfaCache) rebuild() {
	d := make([]*antlr.DFA, len(c.atn.DecisionToState))
	for i, ds := range c.atn.DecisionToState {
		d[i] = antlr.NewDFA(ds, i)
	}
	c.d2dfa = d
	c.pcc = antlr.NewPredictionContextCache()
}

// DFACacheStat reports the state of one grammar's bounded DFA cache.
type DFACacheStat struct {
	// States is the retained DFA-state count at the last measurement (taken
	// every DFACheckInterval parses), or 0 if no measurement has run yet.
	States int64
	// Resets is the cumulative number of times the cache was rebuilt because
	// it exceeded MaxDFAStates.
	Resets int64
}

// DFACacheStats returns the current bounded-DFA-cache state for Grammar1 (used
// by Parse) and Grammar2 (used by ParseCanonical). Intended for monitoring a
// long-running parser: a steadily rising Resets count means the workload's
// structural diversity exceeds MaxDFAStates and the cache is sawtoothing.
func DFACacheStats() (grammar1, grammar2 DFACacheStat) {
	return DFACacheStat{States: grammar1DFA.lastStates.Load(), Resets: grammar1DFA.resets.Load()},
		DFACacheStat{States: grammar2DFA.lastStates.Load(), Resets: grammar2DFA.resets.Load()}
}
