//go:build integration

package colwidth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// The resolver's own tests run against InMemoryFactsStore, which shares the
// interface but not the storage. These run the same state machine over live
// ClickHouse, because the two backends differ in ways that only show up
// there: the collapse to latest-per-key happens in SQL rather than a
// reverse scan, widths and font sizes round-trip through the f64 section,
// and "latest" is (ts, id) instead of insertion order.
//
// Scratch database, not boxer.facts — a developer running this is likely to
// have a desktop pointed at the real table.
const durabilityDb = "colwidth_durability_test"

func newLiveStore(t *testing.T) (store colwidth.StoreI, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", chclient.Defaults().URL, err)
	}
	cfg := chstore.Defaults()
	cfg.Database = durabilityDb
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	require.NoError(t, s.DropTable(ctx))
	require.NoError(t, s.SetupTable(ctx, "MergeTree() ORDER BY tuple()"))
	cleanup = func() { _ = s.DropTable(context.Background()) }
	return s, cleanup
}

// reopen builds a store client that shares nothing with the caller's but
// the database — the closest a single process gets to "a later run".
func reopen(t *testing.T) colwidth.StoreI {
	t.Helper()
	cfg := chstore.Defaults()
	cfg.Database = durabilityDb
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	return s
}

var (
	colA = colwidth.Column{Name: "name", Type: "String"}
	colB = colwidth.Column{Name: "count", Type: "UInt64"}
)

// capturedAt / flushedAt are both in the past, and flushedAt is what lands
// on the row: Flush stamps rows with the time it is handed. Dating them
// forward to clear the debounce — the obvious way to write this — puts the
// width row ahead of a tombstone written at real now, and Clear then loses
// on the (ts, id) sort key. The rows must sit where a real session would
// have left them.
func testClock() (capturedAt, flushedAt time.Time) {
	capturedAt = time.Now().Add(-time.Hour)
	flushedAt = capturedAt.Add(colwidth.DefaultDebounce + time.Second)
	return
}

func newResolver(t *testing.T, store colwidth.StoreI) *colwidth.Resolver {
	t.Helper()
	r, err := colwidth.New(store, colwidth.Opts{AppId: "play"})
	require.NoError(t, err)
	require.NoError(t, r.Load())
	return r
}

// settleReports is the resolver's reseedSettleFrames, which is unexported.
// Spelled as a literal with the reason attached rather than plumbed out of the
// package: a test is entitled to know the number, and if the resolver ever
// changes it these tests should fail loudly rather than track it silently.
const settleReports = 2

// seed resolves a table's widths and then spends the settle window, which is
// what a render loop does across frames and what these tests used to skip.
//
// Resolve opens that window whenever it bumps a table's epoch — so always on
// the first Resolve for a table. The reports arriving inside it describe the
// crate's own doing rather than a person's: the first was produced before the
// seed landed, the second is the seeded frame's own result. Only from the
// third does a difference mean someone dragged.
//
// Skipping it does not fail loudly, which is why these tests went stale
// unnoticed for two weeks: the very first Observe is *adopted* as the baseline
// instead of captured, so the "drag" lands as the crate's own settled width,
// Flush writes nothing, and every assertion downstream is made against a
// capture that never happened.
func seed(t *testing.T, r *colwidth.Resolver, tag string, cols []colwidth.Column, fontSize float64, defaults []float64, now time.Time) (applied []float64) {
	t.Helper()
	applied = r.Resolve(tag, cols, fontSize, defaults)
	for range settleReports {
		r.Observe(tag, cols, applied, fontSize, false, now)
	}
	return
}

// The whole point of ADR-0151: a width the user drags is still there next
// time. One resolver captures it; a fresh resolver over a fresh client
// resolves to it rather than to the caller's default.
func TestWidthsSurviveAcrossResolvers_LiveCH(t *testing.T) {
	store, cleanup := newLiveStore(t)
	defer cleanup()
	cols := []colwidth.Column{colA, colB}
	now, flushAt := testClock()

	first := newResolver(t, store)
	applied := seed(t, first, "attr-results", cols, 12, []float64{80, 40}, now)
	require.Equal(t, []float64{80, 40}, applied)
	// The user drags column A wider.
	first.Observe("attr-results", cols, []float64{175, 40}, 12, false, now)
	written, err := first.Flush(flushAt)
	require.NoError(t, err)
	require.Equal(t, 2, written, "a drag writes the instance and column tiers")

	second := newResolver(t, reopen(t))
	got := second.Resolve("attr-results", cols, 12, []float64{80, 40})
	assert.InDelta(t, 175.0, got[0], 1e-6, "the dragged width must survive")
	assert.InDelta(t, 40.0, got[1], 1e-6, "an untouched column keeps its default")
}

// The column tier is what lets a recurring column keep its width in a
// differently-shaped result — the tier a drag writes alongside instance.
func TestColumnTierAppliesUnderAnotherTable_LiveCH(t *testing.T) {
	store, cleanup := newLiveStore(t)
	defer cleanup()
	now, flushAt := testClock()

	one := []colwidth.Column{colA}
	first := newResolver(t, store)
	seed(t, first, "attr-results", one, 12, []float64{80}, now)
	first.Observe("attr-results", one, []float64{175}, 12, false, now)
	written, err := first.Flush(flushAt)
	require.NoError(t, err)
	require.Equal(t, 2, written, "the drag must reach the store for the read-back to mean anything")

	// A different table, containing the same column.
	second := newResolver(t, reopen(t))
	got := second.Resolve("some-other-table", []colwidth.Column{colB, colA}, 12, []float64{40, 80})
	assert.InDelta(t, 175.0, got[1], 1e-6)
}

// Font size travels with the width through the f64 section and drives the
// rescale on the far side. Worth a live check specifically: the f64 value
// column's physical name was the one thing in this schema that could not
// be extrapolated.
func TestFontRescaleSurvivesTheStore_LiveCH(t *testing.T) {
	store, cleanup := newLiveStore(t)
	defer cleanup()
	cols := []colwidth.Column{colA}
	now, flushAt := testClock()

	first := newResolver(t, store)
	seed(t, first, "attr-results", cols, 12, []float64{80}, now)
	first.Observe("attr-results", cols, []float64{100}, 12, false, now)
	written, err := first.Flush(flushAt)
	require.NoError(t, err)
	require.Equal(t, 2, written, "the drag must reach the store for the rescale to be exercised")

	second := newResolver(t, reopen(t))
	got := second.Resolve("attr-results", cols, 24, []float64{80})
	assert.InDelta(t, 200.0, got[0], 1e-6,
		"a width captured at font 12 must read back doubled at font 24")
}

// Clearing has to reach the store, not just the in-memory set: the next
// run must see defaults. This is the tombstone path end to end.
func TestClearSurvivesAcrossResolvers_LiveCH(t *testing.T) {
	store, cleanup := newLiveStore(t)
	defer cleanup()
	cols := []colwidth.Column{colA}
	now, flushAt := testClock()

	first := newResolver(t, store)
	seed(t, first, "attr-results", cols, 12, []float64{80}, now)
	first.Observe("attr-results", cols, []float64{175}, 12, false, now)
	written, err := first.Flush(flushAt)
	require.NoError(t, err)
	require.Equal(t, 2, written, "there must be a stored override for the clear to tombstone")

	second := newResolver(t, reopen(t))
	require.InDelta(t, 175.0, second.Resolve("attr-results", cols, 12, []float64{80})[0], 1e-6)
	require.NoError(t, second.Clear("attr-results", colA))

	third := newResolver(t, reopen(t))
	assert.InDelta(t, 80.0, third.Resolve("attr-results", cols, 12, []float64{80})[0], 1e-6,
		"a cleared override must not come back from the store")
}

// Overrides are scoped by app id, and the store is shared by every app in
// the process. A second app must not inherit the first's widths.
//
// The written-count assertion is load-bearing here in a way it is not in its
// siblings. This test asserts an ABSENCE — that the other app sees its own
// default — which an empty store satisfies just as well as a correctly scoped
// one. While the settle window went unspent it was the one test of the five
// that still passed, and it passed for exactly the wrong reason: nothing had
// been written for it to fail to cross. Pinning the write first is what makes
// the absence downstream mean something.
func TestOverridesDoNotCrossApps_LiveCH(t *testing.T) {
	store, cleanup := newLiveStore(t)
	defer cleanup()
	cols := []colwidth.Column{colA}
	now, flushAt := testClock()

	play := newResolver(t, store)
	seed(t, play, "attr-results", cols, 12, []float64{80}, now)
	play.Observe("attr-results", cols, []float64{175}, 12, false, now)
	written, err := play.Flush(flushAt)
	require.NoError(t, err)
	require.Equal(t, 2, written, "play's override must exist before another app can fail to see it")

	other, err := colwidth.New(reopen(t), colwidth.Opts{AppId: "imztop"})
	require.NoError(t, err)
	require.NoError(t, other.Load())
	assert.InDelta(t, 80.0, other.Resolve("attr-results", cols, 12, []float64{80})[0], 1e-6,
		"another app must see its own defaults, not play's overrides")
}
