package antlr4utils

import (
	"sync"
	"sync/atomic"

	"github.com/antlr4-go/antlr/v4"
)

// The ANTLR runtime memoises adaptive-prediction decisions in a DFA cache that
// each generated grammar holds in a package-level global (decisionToDFA plus a
// PredictionContextCache). The cache keys on token-TYPE sequences, not token
// text, and it never evicts — antlr4-go v4.13.1 exposes no ClearDFA. So varying
// literals and identifiers cost nothing, but structurally novel input (deeply
// or variably nested expressions) accumulates DFA states without bound: a fuzz
// run or an ad-hoc-SQL proxy can drive it into the multi-GB range over a long
// process lifetime. See ADR-0084.
//
// DFACache replaces that shared global with a process-local cache that can be
// rebuilt. It is bounded by its actual retained-state count: every
// CheckInterval parses, one parse measures the cache and, if it exceeds
// MaxStates, rebuilds it from the immutable ATN. A size threshold (rather than
// a parse count) never fires for the templated/parameterised traffic that
// plateaus at a small state count, so the warm cache is preserved for the
// common case and discarded only under genuine structural novelty — where it
// was providing little reuse anyway.
//
// One holder is meant to be shared by every seam that parses a given grammar
// (ADR-0196 §SD3). Private holders per call site would multiply the memory
// bound and split the warmth.

// DefaultMaxDFAStates is the retained DFA-state count above which a cache is
// rebuilt. The empirical per-state weight is ~5–11 KB, so this bounds a cache
// at roughly 40–90 MB; the templated-SQL plateau is a few hundred states, so it
// leaves ~17x headroom before a reset ever triggers.
const DefaultMaxDFAStates int64 = 8192

// DefaultDFACheckInterval is how many parses occur between cache-size
// measurements. Measuring takes a brief exclusive lock that drains in-flight
// parses, so it is amortised over this many parses; the cost is a worst-case
// overshoot of about this many parses' worth of growth above MaxStates before a
// reset.
const DefaultDFACheckInterval int64 = 256

// DFACache is a process-local, size-bounded ANTLR DFA cache for one grammar.
//
// The RWMutex separates the common read path from the rare reset: parses hold
// the read-lock for the duration of the parse (so they run fully concurrently
// and may mutate the shared DFA under the ATN's own internal mutexes, exactly
// as antlr4-go intends), while a reset takes the write-lock, which drains
// in-flight parses so the state count can be summed and the slice swapped with
// no concurrent mutation.
//
// The zero value is ready to use and adopts the package defaults; the ATN is
// captured from the first parser handed to [DFACache.Acquire].
type DFACache struct {
	once  sync.Once
	mu    sync.RWMutex
	atn   *antlr.ATN
	d2dfa []*antlr.DFA
	pcc   *antlr.PredictionContextCache

	maxStates     atomic.Int64 // 0 means DefaultMaxDFAStates
	checkInterval atomic.Int64 // 0 means DefaultDFACheckInterval

	sinceCheck atomic.Int64 // parses since the last measurement
	lastStates atomic.Int64 // last measured retained-state count (for stats)
	resets     atomic.Int64 // cumulative rebuilds (for stats)
}

// SetLimits sets the rebuild threshold and the measurement interval. A
// non-positive value restores the corresponding default. Safe to call at any
// time, including concurrently with parses; the new values take effect at the
// next measurement.
func (inst *DFACache) SetLimits(maxStates, checkInterval int64) {
	if maxStates <= 0 {
		maxStates = 0
	}
	if checkInterval <= 0 {
		checkInterval = 0
	}
	inst.maxStates.Store(maxStates)
	inst.checkInterval.Store(checkInterval)
}

func (inst *DFACache) limits() (maxStates, checkInterval int64) {
	if maxStates = inst.maxStates.Load(); maxStates <= 0 {
		maxStates = DefaultMaxDFAStates
	}
	if checkInterval = inst.checkInterval.Load(); checkInterval <= 0 {
		checkInterval = DefaultDFACheckInterval
	}
	return
}

// Acquire points a freshly constructed parser at this holder's cache and takes
// the read-lock. The caller must assign the returned simulator to
// parser.Interpreter and call the returned release func when parsing is done
// (defer it — release is panic-safe).
func (inst *DFACache) Acquire(p antlr.Parser) (*antlr.ParserATNSimulator, func()) {
	inst.once.Do(func() {
		inst.atn = p.GetATN() // immutable; captured from the generated interpreter
		inst.rebuild()
	})
	inst.mu.RLock()
	sim := antlr.NewParserATNSimulator(p, inst.atn, inst.d2dfa, inst.pcc)
	return sim, inst.release
}

// release ends a parse and, periodically, measures the cache and rebuilds it if
// it has grown past MaxStates.
func (inst *DFACache) release() {
	inst.mu.RUnlock()
	_, checkInterval := inst.limits()
	if inst.sinceCheck.Add(1) >= checkInterval {
		inst.maybeReset()
	}
}

func (inst *DFACache) maybeReset() {
	inst.mu.Lock() // drains in-flight parses: no concurrent DFA mutation under this
	defer inst.mu.Unlock()
	maxStates, checkInterval := inst.limits()
	if inst.sinceCheck.Load() < checkInterval {
		return // another goroutine already ran the check
	}
	inst.sinceCheck.Store(0)

	var n int64
	for _, d := range inst.d2dfa {
		n += int64(d.Len())
	}
	inst.lastStates.Store(n)
	if n > maxStates {
		inst.rebuild()
		inst.resets.Add(1)
	}
}

// rebuild allocates a fresh, empty DFA slice and prediction-context cache from
// the immutable ATN. Must be called with exclusive ownership of inst (inside
// once.Do or while holding inst.mu for writing).
func (inst *DFACache) rebuild() {
	d := make([]*antlr.DFA, len(inst.atn.DecisionToState))
	for i, ds := range inst.atn.DecisionToState {
		d[i] = antlr.NewDFA(ds, i)
	}
	inst.d2dfa = d
	inst.pcc = antlr.NewPredictionContextCache()
}

// DFACacheStat reports the state of one bounded DFA cache.
type DFACacheStat struct {
	// States is the retained DFA-state count at the last measurement (taken
	// every CheckInterval parses), or 0 if no measurement has run yet.
	States int64
	// Resets is the cumulative number of times the cache was rebuilt because
	// it exceeded MaxStates.
	Resets int64
}

// Stat returns the cache's last-measured state count and cumulative reset
// count. Intended for monitoring a long-running parser: a steadily rising
// Resets count means the workload's structural diversity exceeds MaxStates and
// the cache is sawtoothing.
func (inst *DFACache) Stat() DFACacheStat {
	return DFACacheStat{States: inst.lastStates.Load(), Resets: inst.resets.Load()}
}
