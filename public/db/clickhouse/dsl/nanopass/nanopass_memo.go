package nanopass

import (
	"slices"
	"sync"

	"github.com/antlr4-go/antlr/v4"
	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
)

// Parsing and environment extraction memoised by text (ADR-0306).
//
// A pipeline hands each pass the previous pass's output string, and every pass
// parses what it is handed. Most passes rewrite nothing on a given statement,
// so most of those parses are of text the process parsed a moment ago: a play
// pre-execute stage parsed its statement 26 to 28 times, over only 4 to 8
// distinct texts. Both functions are deterministic in their input, so a repeat can be
// answered from the earlier result.
//
// The memo is keyed by the text rather than scoped to one run because passes
// call [Parse] as a free function with no handle on the run they belong to;
// a key that is the text needs none, and also answers a run of a statement
// that has not changed since the last one.
//
// Only successful results are kept. A cached [ParseResult] is handed to every
// caller that asks for the same text, so it is shared: callers must treat it
// as read-only, which every caller in the repository already does — nothing
// writes to a tree, a token or the stream after a parse.

// DefaultMemoBudget is the default byte budget of each memo, charged as an
// estimate of what an entry retains (see parseCharge, extractCharge).
const DefaultMemoBudget int64 = 8 << 20

// memoMaxEntries backstops the byte budget against a flood of tiny inputs.
const memoMaxEntries = 256

// parseCharge estimates the heap a cached ParseResult retains. Measured on
// 2026-10-10 over the pipeline benchmark's fixtures: about 7.5 KB for a
// 27-byte statement, 80 KB for 572 bytes and 570 KB for 10 KB — a fixed part
// plus roughly 57 bytes per source byte. The charge rounds both up.
func parseCharge(sql string) int64 { return 8<<10 + 64*int64(len(sql)) }

// extractCharge estimates an extracted environment: the key, which the
// environment's raw values slice into, plus the scanned slot and setting text.
func extractCharge(sql string) int64 { return 1<<10 + 2*int64(len(sql)) }

// textMemo is a byte-budgeted LRU from input text to a result. The lock is
// never held while a result is computed, so two goroutines racing the same
// uncached text both compute it and the second put replaces the first: wasted
// work, not a wrong answer, since the computation is deterministic.
type textMemo[V any] struct {
	mu     sync.Mutex
	lru    *simplelru.LRU[string, V]
	charge func(string) int64
	bytes  int64
	budget int64
	hits   int64
	misses int64
}

func newTextMemo[V any](charge func(string) int64) *textMemo[V] {
	inst := &textMemo[V]{charge: charge, budget: DefaultMemoBudget}
	lru, err := simplelru.NewLRU(memoMaxEntries, func(k string, _ V) {
		// Called by simplelru with inst.mu already held — never lock here.
		inst.bytes -= inst.charge(k)
	})
	if err != nil {
		// memoMaxEntries is a positive constant; failing here is a programming
		// error, not a runtime condition.
		log.Panic().Err(err).Msg("unable to construct a nanopass memo")
	}
	inst.lru = lru
	return inst
}

func (inst *textMemo[V]) get(k string) (v V, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.budget <= 0 {
		return
	}
	v, ok = inst.lru.Get(k)
	if ok {
		inst.hits++
	} else {
		inst.misses++
	}
	return
}

// put caches v under k. An entry whose charge alone exceeds the budget is not
// cached: holding it would evict everything else, and the large statements
// that would qualify are the ones whose retained trees cost the most to keep.
func (inst *textMemo[V]) put(k string, v V) {
	c := inst.charge(k)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if c > inst.budget {
		return
	}
	// Peek, not Contains-then-Add: a racing goroutine may have added k
	// already, and charging it twice would leak the budget.
	if _, existed := inst.lru.Peek(k); !existed {
		inst.bytes += c
	}
	inst.lru.Add(k, v)
	inst.evictToBudgetLocked()
}

func (inst *textMemo[V]) evictToBudgetLocked() {
	for inst.bytes > inst.budget && inst.lru.Len() > 0 {
		inst.lru.RemoveOldest() // fires the evict callback, which decrements bytes
	}
}

func (inst *textMemo[V]) setBudget(budget int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.budget = budget
	inst.evictToBudgetLocked()
}

func (inst *textMemo[V]) stat() MemoStat {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return MemoStat{Hits: inst.hits, Misses: inst.misses, Entries: inst.lru.Len(), Bytes: inst.bytes}
}

// extracted is a memoised [env.Extract]: the environment is a template that
// is never handed out, only cloned, because passes write to the one they get.
type extracted struct {
	e    *env.Environment
	body string
}

var (
	parseMemo1  = newTextMemo[*ParseResult](parseCharge)
	parseMemo2  = newTextMemo[*ParseResult](parseCharge)
	extractMemo = newTextMemo[extracted](extractCharge)
)

// extractEnv is [env.Extract] answered from the memo when sql was extracted
// before. The environment returned is the caller's own.
func extractEnv(sql string) (e *env.Environment, body string, err error) {
	if v, ok := extractMemo.get(sql); ok {
		return v.e.Clone(), v.body, nil
	}
	e, body, err = env.Extract(sql)
	if err != nil {
		return
	}
	extractMemo.put(sql, extracted{e: e.Clone(), body: body})
	return
}

// SetMemoBudget sets the byte budget of each of the three memos — grammar1
// parses, grammar2 parses and environment extraction — evicting down to it. A
// budget of zero or less disables memoisation and empties the memos; it is
// what a benchmark of the parse itself, or a test of what one parse does,
// sets. Safe to call at any time, including concurrently with parses — but
// the setting is process-wide, so tests that change it must not run in
// parallel with tests that depend on it.
func SetMemoBudget(budget int64) {
	parseMemo1.setBudget(budget)
	parseMemo2.setBudget(budget)
	extractMemo.setBudget(budget)
}

// MemoStat reports one memo's counters and occupancy.
type MemoStat struct {
	Hits    int64
	Misses  int64
	Entries int
	// Bytes is the charged estimate, not a measured heap size.
	Bytes int64
}

// MemoStats returns the counters of the grammar1 parse memo (used by [Parse]),
// the grammar2 parse memo (used by [ParseCanonical]) and the extraction memo
// (used by [Pass.Run]).
func MemoStats() (parse1, parse2, extract MemoStat) {
	return parseMemo1.stat(), parseMemo2.stat(), extractMemo.stat()
}

// detachPrediction leaves a finished parser referring to the immutable ATN
// only. The generated tree nodes keep their parser, the parser its
// simulator, and the simulator — together with the LL-island listener — the
// shared DFA it predicted from. A cached ParseResult would otherwise hold the
// DFA that was live when it was parsed, and keep it alive after the bounded
// cache (ADR-0084) has rebuilt and dropped it.
func detachPrediction(p *antlr.BaseParser) {
	for _, l := range slices.Clone(p.GetParseListeners()) {
		p.RemoveParseListener(l)
	}
	// The simulator is never asked to predict again, so it gets no DFA at all.
	// Not the generated parser's own static DFA either: that one is unbounded.
	p.Interpreter = antlr.NewParserATNSimulator(p, p.GetATN(), nil, nil)
}
