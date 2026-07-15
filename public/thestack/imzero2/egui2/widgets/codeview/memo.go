package codeview

import (
	"sync"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// memo.go implements ADR-0125: the Prepare* family memoises, Build* does not.
//
// Before it, the two families were the same function and the split lived in a
// doc comment. That mattered because highlighting is not cheap — SQL runs a full
// nanopass.Parse (lex, parse, CST walk), which is ~129 µs for a one-line query
// and ~3.5 ms for a three-line CTE — and four call sites invoked it every frame.
//
// A probe costs 30 ns for a query and ~7 µs for a 312 KB document: Go hashes the
// whole string key at ~45 GB/s, three to four orders below the build it skips.

// job is the retained CodeViewJob every builder returns.
type job = typed.RetainedFffiHolderTyped[c.CodeViewJobS]

// memoBudgetBytes is the memo's byte budget, charged against source length
// (see chargeOf). Bytes rather than entries because entries span a 31-byte
// query to a 300 KB document — four orders of magnitude — so a count says
// nothing about memory (ADR-0125 §SD2).
//
// 8 MB is comfortably larger than any single realistic entry, so nothing is
// uncacheable and no one entry can evict the whole cache.
const memoBudgetBytes = 8 << 20

// memoMaxEntries is a secondary backstop so a flood of tiny sources cannot grow
// the map without bound while staying far under the byte budget.
const memoMaxEntries = 4096

// langE identifies which builder produced an entry, so the same source prepared
// as two languages does not collide.
type langE uint8

const (
	langSQL langE = iota + 1
	langJSON
	langGo
	// langGoGoLines is distinct from langGo even though both run the Go
	// highlighter: PrepareGoLines(src, 0, 0) would otherwise key identically to
	// PrepareGo(src) and serve the whole file where a (clamped, empty) window
	// was asked for.
	langGoLines
	langMarkdown
)

// memoKey identifies one prepared result. src is part of the key rather than a
// digest of it: a 64-bit collision is unlikely but its consequence is a document
// rendered with another's spans — a silently wrong, unreproducible render — and
// the probe is already free relative to the build (ADR-0125 §SD1).
type memoKey struct {
	lang      langE
	firstLine int32
	lastLine  int32
	src       string
}

// chargeOf estimates an entry's memory: the key retains a copy of the source,
// and the value's interned buffer embeds the source plus its sections. An
// estimate rather than the holder's true size, which would couple this cache to
// the wire layout.
func chargeOf(k memoKey) int { return 2 * len(k.src) }

// memoT is the package-level prepared-job cache.
//
// The lock is never held across a build (§SD3): a 26 ms markdown build under a
// package-global mutex would stall every other caller. It must be locked at all,
// despite the frame path being single-threaded, because markdown.Parse calls
// Prepare* at parse time and the documented retain-once idiom is a package-level
// `var doc = markdown.Parse(...)` — which runs at init, on whatever goroutine
// gets there.
type memoT struct {
	mu    sync.Mutex
	lru   *simplelru.LRU[memoKey, job]
	bytes int

	// Observability for the tests; ADR-0125 defers real telemetry.
	hits   uint64
	misses uint64
}

var memo = newMemo()

func newMemo() *memoT {
	inst := &memoT{}
	lru, err := simplelru.NewLRU(memoMaxEntries, func(k memoKey, _ job) {
		// Called by simplelru with inst.mu already held by this goroutine —
		// never take the lock here.
		inst.bytes -= chargeOf(k)
	})
	if err != nil {
		// memoMaxEntries is a positive constant; a failure here is a
		// programming error, not a runtime condition.
		log.Panic().Err(err).Msg("unable to construct the codeview memo")
	}
	inst.lru = lru
	return inst
}

// prepare returns the memoised job for k, calling build on a miss.
//
// build runs outside the lock, so two goroutines racing the same uncached key
// both build it and the second Add replaces the first. That is wasted work, not
// a wrong result: the two holders serialize to identical bytes and intern to the
// same buffer.
func (inst *memoT) prepare(k memoKey, build func() job) job {
	inst.mu.Lock()
	if v, ok := inst.lru.Get(k); ok {
		inst.hits++
		inst.mu.Unlock()
		return v
	}
	inst.misses++
	inst.mu.Unlock()

	v := build()

	inst.mu.Lock()
	// Peek, not Contains-then-Add: a racing goroutine may have added this key
	// already, and charging it twice would leak the budget downward until the
	// cache held nothing.
	if _, existed := inst.lru.Peek(k); !existed {
		inst.bytes += chargeOf(k)
	}
	inst.lru.Add(k, v)
	inst.evictToBudgetLocked()
	inst.mu.Unlock()
	return v
}

// evictToBudgetLocked drops oldest-first until the charge fits.
//
// It stops at one entry, so a source larger than the whole budget is still
// cached and still served — that is the case that needs the memo most (a large
// help document re-parsed every frame). The invariant is therefore "at most one
// entry may exceed the budget", not "bytes <= budget".
func (inst *memoT) evictToBudgetLocked() {
	for inst.bytes > memoBudgetBytes && inst.lru.Len() > 1 {
		inst.lru.RemoveOldest() // fires the evict callback, which decrements bytes
	}
}

// memoStats reports the cache's counters. Unexported: ADR-0125 defers telemetry,
// and this exists so the tests can assert a hit is a hit.
func (inst *memoT) stats() (hits uint64, misses uint64, bytes int, entries int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.hits, inst.misses, inst.bytes, inst.lru.Len()
}

// reset empties the cache and its counters, for tests.
func (inst *memoT) reset() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.lru.Purge()
	inst.bytes = 0
	inst.hits = 0
	inst.misses = 0
}
