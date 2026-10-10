package antlr4utils

import (
	"sync"
	"sync/atomic"

	"github.com/antlr4-go/antlr/v4"
)

// A Sync that does not take the ATN's mutex.
//
// The generated parser calls ErrorStrategy.Sync before every loop iteration
// and optional block — about three times per token. antlr4-go v4.13.1's
// DefaultErrorStrategy.Sync asks the ATN for the current state's within-rule
// follow set, and ATN.NextTokensNoContext takes an exclusive ATN-wide mutex on
// every call, even when the set is already cached on the state. Every parse of
// a grammar, on every goroutine, serialises on that one mutex.
//
// syncSets memoises those sets per ATN state as bitsets that are read without
// a lock; the first lookup of a state goes through the runtime once. When the
// lookahead is in the set — every Sync of a clean parse — FastSyncStrategy
// returns, exactly as the default does. Otherwise it defers to the default
// strategy, which recomputes the same set and recovers as it always has, so
// error recovery and diagnostics are unchanged.

// tokenSet is an ATN state's within-rule follow set, as membership bits.
type tokenSet struct {
	words   [4]uint64 // token types 0..255
	eof     bool
	epsilon bool // the state can reach the end of its rule: Sync never fails
	// exotic marks a set holding a token type the bitset cannot represent;
	// such a state always takes the default path.
	exotic bool
}

func newTokenSet(s *antlr.IntervalSet) (ts *tokenSet) {
	ts = &tokenSet{}
	if s == nil {
		ts.exotic = true
		return
	}
	for _, iv := range s.GetIntervals() {
		for v := iv.Start; v < iv.Stop; v++ {
			switch {
			case v == antlr.TokenEpsilon:
				ts.epsilon = true
			case v == antlr.TokenEOF:
				ts.eof = true
			case v >= 0 && v < 256:
				ts.words[v>>6] |= 1 << (uint(v) & 63)
			default:
				ts.exotic = true
			}
		}
	}
	return
}

// covers reports whether Sync may return without consulting the runtime.
func (inst *tokenSet) covers(la int) bool {
	switch {
	case inst.exotic:
		return false
	case inst.epsilon:
		return true
	case la == antlr.TokenEOF:
		return inst.eof
	case la >= 0 && la < 256:
		return inst.words[la>>6]&(1<<(uint(la)&63)) != 0
	}
	return false
}

// syncSets is a copy-on-write table of tokenSets indexed by ATN state number.
// Reads are one atomic load; writes happen once per state per process.
type syncSets struct {
	mu    sync.Mutex
	table atomic.Pointer[[]*tokenSet]
}

func (inst *syncSets) get(state int) *tokenSet {
	if t := inst.table.Load(); t != nil && state < len(*t) {
		return (*t)[state]
	}
	return nil
}

func (inst *syncSets) put(state int, ts *tokenSet) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var old []*tokenSet
	if t := inst.table.Load(); t != nil {
		old = *t
	}
	n := max(len(old), state+1)
	if state >= len(old) {
		n = max(n, 2*len(old))
	}
	t := make([]*tokenSet, n)
	copy(t, old)
	t[state] = ts
	inst.table.Store(&t)
}

// withinRuleExpecter is implemented by every generated parser (through the
// embedded BaseParser) but is not part of antlr.Parser.
type withinRuleExpecter interface {
	GetExpectedTokensWithinCurrentRule() *antlr.IntervalSet
}

// FastSyncStrategy is antlr.DefaultErrorStrategy with a lock-free Sync for
// the common case. One instance per parser: the embedded default strategy
// carries per-parse recovery state.
type FastSyncStrategy struct {
	*antlr.DefaultErrorStrategy
	sets *syncSets
}

var _ antlr.ErrorStrategy = (*FastSyncStrategy)(nil)

// Sync returns when the lookahead can continue the current rule, and otherwise
// does what antlr.DefaultErrorStrategy.Sync does.
func (inst *FastSyncStrategy) Sync(p antlr.Parser) {
	if inst.InErrorRecoveryMode(p) {
		return
	}
	if state := p.GetState(); state >= 0 {
		ts := inst.sets.get(state)
		if ts == nil {
			if e, ok := p.(withinRuleExpecter); ok {
				ts = newTokenSet(e.GetExpectedTokensWithinCurrentRule())
				inst.sets.put(state, ts)
			}
		}
		if ts != nil && ts.covers(p.GetTokenStream().LA(1)) {
			return
		}
	}
	inst.DefaultErrorStrategy.Sync(p)
}
