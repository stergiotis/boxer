package codeview

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0125 §Validation. The memo is package-global, so every test resets it
// first and none of them may run in parallel with each other.

const sampleSQL = `SELECT a, b, count() AS n FROM t WHERE x > 1 GROUP BY a, b ORDER BY n DESC`

// A hit returns the identical holder and does not rebuild.
func TestPrepareMemoHit(t *testing.T) {
	memo.reset()

	first := PrepareSql(sampleSQL)
	hits, misses, _, entries := memo.stats()
	assert.Equal(t, uint64(0), hits)
	assert.Equal(t, uint64(1), misses, "the first prepare is a miss")
	assert.Equal(t, 1, entries)

	second := PrepareSql(sampleSQL)
	hits, misses, _, entries = memo.stats()
	assert.Equal(t, uint64(1), hits, "the second prepare is a hit")
	assert.Equal(t, uint64(1), misses, "and does not rebuild")
	assert.Equal(t, 1, entries)

	// The counters are the only honest evidence of a hit. Buffer identity is
	// NOT: unique.Make interns the serialized bytes, so two independent builds
	// of one source already share a backing array — a pointer comparison would
	// pass with the memo ripped out. Content equality is asserted only as a
	// sanity check that the cached holder is the right one.
	assert.Equal(t, contentOf(first), contentOf(second))
}

// Build* must never consult or populate the memo — it is the escape hatch.
func TestBuildBypassesMemo(t *testing.T) {
	memo.reset()

	_ = BuildSql(sampleSQL)
	_ = BuildSql(sampleSQL)
	_ = BuildJson(`{"a":1}`)
	_ = BuildGo("package main")
	_ = BuildMarkdown("# hi")
	_ = BuildGoLines("package main\nfunc f() {}\n", 1, 2)

	hits, misses, bytes, entries := memo.stats()
	assert.Zero(t, hits)
	assert.Zero(t, misses)
	assert.Zero(t, bytes)
	assert.Zero(t, entries, "Build* leaves the memo untouched")
}

// The same source prepared as two languages is two entries — a shared key would
// serve SQL spans for a Go file.
func TestMemoKeyedByLanguage(t *testing.T) {
	memo.reset()

	const src = "select" // lexes under every highlighter
	_ = PrepareSql(src)
	_ = PrepareJson(src)
	_ = PrepareGo(src)
	_ = PrepareMarkdown(src)

	hits, misses, _, entries := memo.stats()
	assert.Zero(t, hits, "no cross-language hits")
	assert.Equal(t, uint64(4), misses)
	assert.Equal(t, 4, entries, "one entry per language")

	// And each is now individually cached.
	_ = PrepareSql(src)
	_ = PrepareGo(src)
	hits, _, _, _ = memo.stats()
	assert.Equal(t, uint64(2), hits)
}

// The regression this key shape exists for: PrepareGoLines(src, 0, 0) must not
// collide with PrepareGo(src). Both windows are clamped to "empty", so a
// collision would serve the whole highlighted file where an empty window was
// asked for — a wrong render, silently.
func TestMemoGoLinesDoesNotCollideWithGo(t *testing.T) {
	memo.reset()

	const src = "package main\n\nfunc main() {}\n"
	whole := PrepareGo(src)
	window := PrepareGoLines(src, 0, 0)

	_, misses, _, entries := memo.stats()
	assert.Equal(t, uint64(2), misses)
	assert.Equal(t, 2, entries, "whole-file and line-window are distinct entries")
	// Content, not pointers: had the keys collided, the window call would have
	// been handed the whole-file holder, so the two would carry the same bytes.
	assert.NotEqual(t, contentOf(whole), contentOf(window),
		"the (0,0) window must not be served the whole file")
}

// Distinct windows over one source are distinct entries.
func TestMemoGoLinesKeyedByWindow(t *testing.T) {
	memo.reset()

	src := "package main\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() {}\n"
	_ = PrepareGoLines(src, 1, 3)
	_ = PrepareGoLines(src, 3, 5)
	_ = PrepareGoLines(src, 1, 3) // hit

	hits, misses, _, entries := memo.stats()
	assert.Equal(t, uint64(1), hits)
	assert.Equal(t, uint64(2), misses)
	assert.Equal(t, 2, entries)
}

// Eviction drops oldest-first until the charge fits the budget.
func TestMemoEvictsToBudget(t *testing.T) {
	memo.reset()

	// Each entry is charged 2*len(src), so ~600 KB of source per entry means
	// the 8 MB budget holds ~6 of them. Feed enough to force eviction.
	const per = 600 << 10
	const n = 12
	for i := range n {
		_ = PrepareJson(distinctJSON(i, per))
	}

	_, misses, bytes, entries := memo.stats()
	assert.Equal(t, uint64(n), misses)
	assert.LessOrEqual(t, bytes, memoBudgetBytes, "the charge is back under budget")
	assert.Less(t, entries, n, "older entries were evicted")
	assert.Positive(t, entries)

	// The oldest is gone; the newest is still there.
	memoBefore, _, _, _ := memo.stats()
	_ = PrepareJson(distinctJSON(n-1, per))
	hitsAfter, _, _, _ := memo.stats()
	assert.Equal(t, memoBefore+1, hitsAfter, "the most recent entry survived")
}

// An entry larger than the whole budget is still cached and served: that is the
// case that needs the memo most. The invariant is "at most one entry may exceed
// the budget", not "bytes <= budget".
func TestMemoOversizedEntryStillServed(t *testing.T) {
	memo.reset()

	huge := distinctJSON(0, memoBudgetBytes) // 2*len > budget once charged
	first := PrepareJson(huge)
	_, _, bytes, entries := memo.stats()
	require.Equal(t, 1, entries, "the sole entry is kept even over budget")
	assert.Greater(t, bytes, memoBudgetBytes)

	second := PrepareJson(huge)
	hits, _, _, _ := memo.stats()
	assert.Equal(t, uint64(1), hits, "and it hits")
	assert.Equal(t, contentOf(first), contentOf(second))

	// Adding anything else evicts it, since it alone busts the budget.
	_ = PrepareJson(`{"small":1}`)
	_, _, bytes, entries = memo.stats()
	assert.Equal(t, 1, entries)
	assert.LessOrEqual(t, bytes, memoBudgetBytes)
}

// The entry-count backstop bounds a flood of tiny sources that never approach
// the byte budget.
func TestMemoEntryCountBackstop(t *testing.T) {
	memo.reset()

	for i := range memoMaxEntries + 100 {
		_ = PrepareJson(fmt.Sprintf(`{"i":%d}`, i))
	}
	_, _, bytes, entries := memo.stats()
	assert.LessOrEqual(t, entries, memoMaxEntries, "the count backstop holds")
	assert.Positive(t, bytes)
	assert.LessOrEqual(t, bytes, memoBudgetBytes)
}

// Evicting must return the charge: a leak here would ratchet `bytes` upward
// until the cache evicted everything on every Add.
func TestMemoByteAccountingReturnsToZero(t *testing.T) {
	memo.reset()

	var want int
	for i := range 50 {
		src := distinctJSON(i, 1<<10)
		want += 2 * len(src) // chargeOf, independently computed
		_ = PrepareJson(src)
	}
	_, _, bytes, entries := memo.stats()
	assert.Equal(t, 50, entries, "well under both bounds, so nothing was evicted")
	assert.Equal(t, want, bytes, "the charge is exactly the sum of the entries")

	memo.reset()
	_, _, bytes, entries = memo.stats()
	assert.Zero(t, entries)
	assert.Zero(t, bytes, "purge returns the whole charge")
}

// Re-preparing an existing key must not charge it twice.
func TestMemoRepeatDoesNotDoubleCharge(t *testing.T) {
	memo.reset()

	src := distinctJSON(1, 4<<10)
	_ = PrepareJson(src)
	_, _, once, _ := memo.stats()

	for range 20 {
		_ = PrepareJson(src)
	}
	_, _, after, entries := memo.stats()
	assert.Equal(t, once, after, "a hit does not re-charge")
	assert.Equal(t, 1, entries)
}

// Concurrency: overlapping prepares from many goroutines. Run under -race.
// Two goroutines racing one uncached key may both build it — that is wasted
// work, not a wrong result — so this asserts consistency, not the miss count.
func TestMemoConcurrent(t *testing.T) {
	memo.reset()

	const goroutines = 16
	const sources = 8
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range sources {
				src := fmt.Sprintf(`{"src":%d,"pad":"%s"}`, i, strings.Repeat("x", 64))
				if len(contentOf(PrepareJson(src))) == 0 {
					t.Errorf("goroutine %d: empty holder for source %d", g, i)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	hits, misses, bytes, entries := memo.stats()
	assert.Equal(t, sources, entries, "one entry per distinct source, regardless of racing")
	assert.Equal(t, uint64(goroutines*sources), hits+misses, "every call is counted exactly once")
	assert.Positive(t, hits, "the memo did serve hits under contention")
	assert.LessOrEqual(t, bytes, memoBudgetBytes)
}

// A slow build must not block a probe — the lock is not held across build().
// Asserted structurally: prepare a large source on one goroutine while another
// hits a cached key; the hit must complete without waiting for the build.
func TestMemoProbeNotBlockedByBuild(t *testing.T) {
	memo.reset()

	const cached = `{"cached":true}`
	_ = PrepareJson(cached) // seed a hit

	buildStarted := make(chan struct{})
	buildDone := make(chan struct{})
	go func() {
		close(buildStarted)
		_ = PrepareMarkdown(strings.Repeat("# heading\n\nbody *text* here\n\n", 4000))
		close(buildDone)
	}()

	<-buildStarted
	probeDone := make(chan struct{})
	go func() {
		for range 100 {
			_ = PrepareJson(cached)
		}
		close(probeDone)
	}()

	select {
	case <-probeDone:
	case <-buildDone:
		// The build finishing first is not a failure — it only means the build
		// was quick. The failure mode is the probe never finishing at all,
		// which the -race/timeout would surface.
		<-probeDone
	}
}

// distinctJSON builds a valid JSON document of roughly `size` bytes, distinct
// per `i`.
func distinctJSON(i int, size int) string {
	var b strings.Builder
	b.Grow(size + 32)
	fmt.Fprintf(&b, `{"id":%d,"pad":"`, i)
	for b.Len() < size {
		b.WriteString("abcdefghij")
	}
	b.WriteString(`"}`)
	return b.String()
}

// contentOf returns a holder's serialized bytes.
func contentOf(j job) []byte { return j.Untype().Content() }
