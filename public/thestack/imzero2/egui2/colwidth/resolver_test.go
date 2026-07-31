package colwidth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The in-memory facts store already implements the three methods StoreI
// needs, so the tests run against the real storage semantics — latest-wins
// per key, tombstones, app scoping — rather than a hand-rolled fake that
// could agree with the resolver and disagree with production.
func newResolver(t *testing.T) (r *Resolver, store *factsstore.InMemoryFactsStore) {
	t.Helper()
	store = factsstore.NewInMemoryFactsStore()
	r, err := New(store, Opts{AppId: "play"})
	require.NoError(t, err)
	return
}

var (
	colA = Column{Name: "name", Type: "String"}
	colB = Column{Name: "count", Type: "UInt64"}
)

// t0 is a fixed base time: the resolver takes `now` as a parameter
// precisely so its debounce can be tested without sleeping.
var t0 = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func TestNew_Validation(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	_, err := New(nil, Opts{AppId: "play"})
	require.Error(t, err)
	_, err = New(store, Opts{})
	require.Error(t, err, "an empty AppId would scope overrides to nothing")
	_, err = New(store, Opts{AppId: "play", Debounce: -time.Second})
	require.Error(t, err)
	_, err = New(store, Opts{AppId: "play", MinPoints: 50, MaxPoints: 10})
	require.Error(t, err, "an inverted clamp range would make every width the max")
}

func TestColumnKey_TypeParticipates(t *testing.T) {
	a := Column{Name: "id", Type: "UInt64"}
	b := Column{Name: "id", Type: "String"}
	assert.NotEqual(t, a.Key(), b.Key(), "a type change must invalidate the override")
	assert.Equal(t, a.Key(), Column{Name: "id", Type: "UInt64"}.Key(), "the key must be stable")
}

// Length-prefixing, not delimiter-joining: these two columns would collide
// under any single-separator scheme.
func TestColumnKey_NoBoundaryCollision(t *testing.T) {
	a := Column{Name: "a", Type: "b|c"}
	b := Column{Name: "a|b", Type: "c"}
	assert.NotEqual(t, a.Key(), b.Key())
}

func TestShapeHash_OrderIndependentAndSetLike(t *testing.T) {
	assert.Equal(t, ShapeHash([]Column{colA, colB}), ShapeHash([]Column{colB, colA}),
		"reordering columns is the same logical table")
	assert.Equal(t, ShapeHash([]Column{colA}), ShapeHash([]Column{colA, colA}),
		"the shape is a set, so a repeated key contributes once")
	assert.NotEqual(t, ShapeHash([]Column{colA}), ShapeHash([]Column{colA, colB}))
}

func TestResolve_FallsBackToDefaults(t *testing.T) {
	r, _ := newResolver(t)
	got := r.Resolve("tbl", []Column{colA, colB}, 12, []float64{80, 40})
	assert.Equal(t, []float64{80, 40}, got)
}

// A short defaults slice is not an error: the missing entries resolve to
// 0, which is the call site's signal to let the crate autofit.
func TestResolve_MissingDefaultsAreZero(t *testing.T) {
	r, _ := newResolver(t)
	got := r.Resolve("tbl", []Column{colA, colB}, 12, []float64{80})
	assert.Equal(t, []float64{80, 0}, got)
}

func TestResolve_TierPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		write []factsstore.ColumnWidthRow
		want  float64
	}{
		{
			name: "column tier only",
			write: []factsstore.ColumnWidthRow{
				{AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: colA.Key(), Points: 30},
			},
			want: 30,
		},
		{
			name: "shape beats column",
			write: []factsstore.ColumnWidthRow{
				{AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: colA.Key(), Points: 30},
				{AppId: "play", Tier: factsstore.ColWidthTierShape, Scope: ShapeHash([]Column{colA}), ColumnKey: colA.Key(), Points: 60},
			},
			want: 60,
		},
		{
			name: "instance beats both",
			write: []factsstore.ColumnWidthRow{
				{AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: colA.Key(), Points: 30},
				{AppId: "play", Tier: factsstore.ColWidthTierShape, Scope: ShapeHash([]Column{colA}), ColumnKey: colA.Key(), Points: 60},
				{AppId: "play", Tier: factsstore.ColWidthTierInstance, Scope: "tbl", ColumnKey: colA.Key(), Points: 90},
			},
			want: 90,
		},
		{
			name: "an instance override under another tag does not apply",
			write: []factsstore.ColumnWidthRow{
				{AppId: "play", Tier: factsstore.ColWidthTierInstance, Scope: "other", ColumnKey: colA.Key(), Points: 90},
			},
			want: 11,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, store := newResolver(t)
			for _, row := range tc.write {
				_, err := store.WriteColumnWidth(row)
				require.NoError(t, err)
			}
			require.NoError(t, r.Load())
			got := r.Resolve("tbl", []Column{colA}, 12, []float64{11})
			assert.Equal(t, tc.want, got[0])
		})
	}
}

// The shape tier is what lets a table inherit widths under a new tag —
// the point of having it at all.
func TestResolve_ShapeTierCrossesTableTags(t *testing.T) {
	r, store := newResolver(t)
	cols := []Column{colA, colB}
	_, err := store.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "play", Tier: factsstore.ColWidthTierShape,
		Scope: ShapeHash(cols), ColumnKey: colA.Key(), Points: 77,
	})
	require.NoError(t, err)
	require.NoError(t, r.Load())

	assert.Equal(t, 77.0, r.Resolve("tag-one", cols, 12, nil)[0])
	assert.Equal(t, 77.0, r.Resolve("tag-two", cols, 12, nil)[0])
}

func TestResolve_RescalesOnFontSizeChange(t *testing.T) {
	tests := []struct {
		name       string
		capturedAt float64
		current    float64
		want       float64
	}{
		{name: "same size is untouched", capturedAt: 12, current: 12, want: 100},
		{name: "larger font widens", capturedAt: 12, current: 18, want: 150},
		{name: "smaller font narrows", capturedAt: 12, current: 6, want: 50},
		{name: "no captured reference disables scaling", capturedAt: 0, current: 18, want: 100},
		{name: "no current reference disables scaling", capturedAt: 12, current: 0, want: 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, store := newResolver(t)
			_, err := store.WriteColumnWidth(factsstore.ColumnWidthRow{
				AppId: "play", Tier: factsstore.ColWidthTierColumn,
				ColumnKey: colA.Key(), Points: 100, FontSize: tc.capturedAt,
			})
			require.NoError(t, err)
			require.NoError(t, r.Load())
			got := r.Resolve("tbl", []Column{colA}, tc.current, nil)
			assert.InDelta(t, tc.want, got[0], 1e-9)
		})
	}
}

func TestResolve_Clamps(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	r, err := New(store, Opts{AppId: "play", MinPoints: 20, MaxPoints: 200})
	require.NoError(t, err)
	_, err = store.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: colA.Key(), Points: 100000,
	})
	require.NoError(t, err)
	require.NoError(t, r.Load())
	assert.Equal(t, 200.0, r.Resolve("tbl", []Column{colA}, 12, nil)[0],
		"an absurd stored width must not render a table undraggable")
	assert.Equal(t, 20.0, r.Resolve("tbl2", []Column{colB}, 12, []float64{1})[0])
}

func TestEpoch_BumpsOnlyWhenResolvedWidthsChange(t *testing.T) {
	r, store := newResolver(t)
	cols := []Column{colA}

	e0 := r.Epoch("tbl")
	r.Resolve("tbl", cols, 12, []float64{50})
	e1 := r.Epoch("tbl")
	assert.Greater(t, e1, e0, "the first resolve is a change from nothing")

	r.Resolve("tbl", cols, 12, []float64{50})
	assert.Equal(t, e1, r.Epoch("tbl"), "an unchanged resolve must not bump")

	_, err := store.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: colA.Key(), Points: 123,
	})
	require.NoError(t, err)
	require.NoError(t, r.Load())
	r.Resolve("tbl", cols, 12, []float64{50})
	assert.Greater(t, r.Epoch("tbl"), e1, "a newly loaded override is a change")
}

// Dropping a column must be seen as a change even when every surviving
// column keeps its width, or a table reusing the tag would compare against
// a stale applied set.
func TestEpoch_BumpsWhenColumnSetShrinks(t *testing.T) {
	r, _ := newResolver(t)
	r.Resolve("tbl", []Column{colA, colB}, 12, []float64{50, 60})
	e := r.Epoch("tbl")
	r.Resolve("tbl", []Column{colA}, 12, []float64{50})
	assert.Greater(t, r.Epoch("tbl"), e)
}

func TestObserve_CapturesDragOnInstanceAndColumnTiers(t *testing.T) {
	r, _ := newResolver(t)
	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, []float64{50})

	r.Observe("tbl", cols, []float64{140}, 12, false, t0)

	assert.Equal(t, 2, r.PendingCount(), "a drag writes the instance and column tiers")
	// The shape tier is read-only in v1.
	assert.Equal(t, 2, r.Len())
}

// Echo suppression: the crate reporting back exactly what we applied is
// not a user adjustment, and a capture must not itself provoke a re-apply.
func TestObserve_EchoIsNotACapture(t *testing.T) {
	r, _ := newResolver(t)
	cols := []Column{colA}
	applied := r.Resolve("tbl", cols, 12, []float64{50})
	e := r.Epoch("tbl")

	r.Observe("tbl", cols, applied, 12, false, t0)
	assert.Zero(t, r.PendingCount())
	assert.Equal(t, e, r.Epoch("tbl"))
}

func TestObserve_CaptureDoesNotBumpEpoch(t *testing.T) {
	r, _ := newResolver(t)
	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, []float64{50})
	e := r.Epoch("tbl")

	r.Observe("tbl", cols, []float64{140}, 12, false, t0)
	assert.Equal(t, e, r.Epoch("tbl"),
		"re-applying the width the user just dragged would fight the drag")

	// And the next resolve agrees with the captured value, so no bump
	// follows on the frame after either.
	got := r.Resolve("tbl", cols, 12, []float64{50})
	assert.Equal(t, 140.0, got[0])
	assert.Equal(t, e, r.Epoch("tbl"))
}

// First show is the crate's force-autofit, not the user's choice.
func TestObserve_FirstShowIsNotACapture(t *testing.T) {
	r, _ := newResolver(t)
	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, []float64{50})
	r.Observe("tbl", cols, []float64{140}, 12, true, t0)
	assert.Zero(t, r.PendingCount())
}

// A column the resolver never applied a width for cannot have been
// dragged away from one.
func TestObserve_UnknownColumnIsIgnored(t *testing.T) {
	r, _ := newResolver(t)
	r.Resolve("tbl", []Column{colA}, 12, []float64{50})
	r.Observe("tbl", []Column{colB}, []float64{140}, 12, false, t0)
	assert.Zero(t, r.PendingCount())
}

func TestFlush_DebouncesUntilMotionStops(t *testing.T) {
	r, store := newResolver(t)
	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, []float64{50})

	r.Observe("tbl", cols, []float64{100}, 12, false, t0)
	n, err := r.Flush(t0.Add(100 * time.Millisecond))
	require.NoError(t, err)
	assert.Zero(t, n, "still moving")

	// The drag continues: the debounce restarts from the newer motion.
	r.Observe("tbl", cols, []float64{140}, 12, false, t0.Add(200*time.Millisecond))
	n, err = r.Flush(t0.Add(500 * time.Millisecond))
	require.NoError(t, err)
	assert.Zero(t, n, "the later motion restarts the debounce")

	n, err = r.Flush(t0.Add(200*time.Millisecond + DefaultDebounce))
	require.NoError(t, err)
	assert.Equal(t, 2, n, "one drag lands as one write per tier, not one per frame")

	rows, err := store.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		assert.Equal(t, 140.0, row.Points, "the final width is stored, not an intermediate one")
		assert.Equal(t, 12.0, row.FontSize)
	}

	n, err = r.Flush(t0.Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n, "a flushed entry is not written again")
}

// A failed write must leave the entry pending: losing a width the user set
// because one insert failed is worse than writing a second row later.
func TestFlush_FailureLeavesEntryPending(t *testing.T) {
	store := &failingStore{InMemoryFactsStore: factsstore.NewInMemoryFactsStore(), fail: true}
	r, err := New(store, Opts{AppId: "play"})
	require.NoError(t, err)
	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, []float64{50})
	r.Observe("tbl", cols, []float64{140}, 12, false, t0)

	_, err = r.Flush(t0.Add(time.Hour))
	require.Error(t, err)
	assert.Equal(t, 2, r.PendingCount(), "the capture must survive to be retried")

	store.fail = false
	n, err := r.Flush(t0.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Zero(t, r.PendingCount())
}

func TestClear_RemovesOverrideAndRestoresDefault(t *testing.T) {
	r, store := newResolver(t)
	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, []float64{50})
	r.Observe("tbl", cols, []float64{140}, 12, false, t0)
	_, err := r.Flush(t0.Add(time.Hour))
	require.NoError(t, err)

	require.NoError(t, r.Clear("tbl", colA))

	rows, err := store.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows, "clearing must tombstone the stored rows, not just forget them")
	assert.Equal(t, 50.0, r.Resolve("tbl", cols, 12, []float64{50})[0])
}

// Clearing while a drag is still pending must not have the debounce write
// the value back a moment later.
func TestClear_DropsPendingCapture(t *testing.T) {
	r, store := newResolver(t)
	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, []float64{50})
	r.Observe("tbl", cols, []float64{140}, 12, false, t0)
	require.Equal(t, 2, r.PendingCount())

	require.NoError(t, r.Clear("tbl", colA))
	assert.Zero(t, r.PendingCount())

	n, err := r.Flush(t0.Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
	rows, err := store.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// A reload racing an unflushed drag must not discard the user's
// adjustment in favour of the older stored value.
func TestLoad_KeepsPendingCaptures(t *testing.T) {
	r, store := newResolver(t)
	_, err := store.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: colA.Key(), Points: 30,
	})
	require.NoError(t, err)
	require.NoError(t, r.Load())

	cols := []Column{colA}
	r.Resolve("tbl", cols, 12, nil)
	r.Observe("tbl", cols, []float64{140}, 12, false, t0)

	require.NoError(t, r.Load())
	assert.Equal(t, 2, r.PendingCount())
	assert.Equal(t, 140.0, r.Resolve("tbl", cols, 12, nil)[0])
}

func TestLoad_IgnoresOtherApps(t *testing.T) {
	r, store := newResolver(t)
	_, err := store.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "imztop", Tier: factsstore.ColWidthTierColumn, ColumnKey: colA.Key(), Points: 999,
	})
	require.NoError(t, err)
	require.NoError(t, r.Load())
	assert.Equal(t, 50.0, r.Resolve("tbl", []Column{colA}, 12, []float64{50})[0])
}

func TestEvict_BoundsMemoryAndSparesPendingCaptures(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	r, err := New(store, Opts{AppId: "play", MaxEntries: 4})
	require.NoError(t, err)

	// Six stored overrides, oldest first.
	for i := range 6 {
		_, werr := store.WriteColumnWidth(factsstore.ColumnWidthRow{
			AppId: "play", Tier: factsstore.ColWidthTierColumn,
			ColumnKey: Column{Name: string(rune('a' + i)), Type: "String"}.Key(),
			Points:    float64(10 + i),
			Ts:        t0.Add(time.Duration(i) * time.Minute),
		})
		require.NoError(t, werr)
	}
	require.NoError(t, r.Load())
	assert.LessOrEqual(t, r.Len(), 4, "the in-memory set is bounded")

	// A pending capture is never evicted, even past the cap.
	cols := []Column{colB}
	r.Resolve("tbl", cols, 12, []float64{50})
	r.Observe("tbl", cols, []float64{140}, 12, false, t0)
	require.NoError(t, r.Load())
	assert.Equal(t, 2, r.PendingCount(), "unsaved user adjustments must survive eviction")
}

// failingStore makes WriteColumnWidth fail on demand.
type failingStore struct {
	*factsstore.InMemoryFactsStore
	fail bool
}

func (inst *failingStore) WriteColumnWidth(row factsstore.ColumnWidthRow) (id uint64, err error) {
	if inst.fail {
		err = eh.Errorf("synthetic write failure")
		return
	}
	id, err = inst.InMemoryFactsStore.WriteColumnWidth(row)
	return
}

var _ StoreI = (*failingStore)(nil)
var _ StoreI = (*factsstore.InMemoryFactsStore)(nil)
var _ StoreI = (factsstore.FactsStoreI)(nil)

func TestClearAll_ResetsEveryColumn(t *testing.T) {
	r, store := newResolver(t)
	cols := []Column{colA, colB}
	r.Resolve("tbl", cols, 12, []float64{50, 60})
	r.Observe("tbl", cols, []float64{140, 160}, 12, false, t0)
	_, err := r.Flush(t0.Add(time.Hour))
	require.NoError(t, err)
	rows, err := store.ListColumnWidths("play")
	require.NoError(t, err)
	require.NotEmpty(t, rows)

	require.NoError(t, r.ClearAll("tbl", cols))

	rows, err = store.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows)
	assert.Equal(t, []float64{50, 60}, r.Resolve("tbl", cols, 12, []float64{50, 60}))
}

// A partial clear is the worst outcome for this gesture, so a failure on
// one column must not abandon the rest.
func TestClearAll_ContinuesPastAFailure(t *testing.T) {
	store := &failingDeleteStore{InMemoryFactsStore: factsstore.NewInMemoryFactsStore(), failFor: colA.Key()}
	r, err := New(store, Opts{AppId: "play"})
	require.NoError(t, err)
	cols := []Column{colA, colB}
	r.Resolve("tbl", cols, 12, []float64{50, 60})
	r.Observe("tbl", cols, []float64{140, 160}, 12, false, t0)
	_, err = r.Flush(t0.Add(time.Hour))
	require.NoError(t, err)

	err = r.ClearAll("tbl", cols)
	require.Error(t, err, "the failure must be reported")

	rows, lerr := store.ListColumnWidths("play")
	require.NoError(t, lerr)
	for _, row := range rows {
		assert.Equal(t, colA.Key(), row.ColumnKey,
			"only the column whose delete failed may survive")
	}
}

func TestClearAll_EmptyColumnSetIsNoop(t *testing.T) {
	r, _ := newResolver(t)
	require.NoError(t, r.ClearAll("tbl", nil))
}

// failingDeleteStore fails DeleteColumnWidth for one column key.
type failingDeleteStore struct {
	*factsstore.InMemoryFactsStore
	failFor string
}

func (inst *failingDeleteStore) DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error) {
	if columnKey == inst.failFor {
		err = eh.Errorf("synthetic delete failure")
		return
	}
	return inst.InMemoryFactsStore.DeleteColumnWidth(appId, tier, scope, columnKey)
}

var _ StoreI = (*failingDeleteStore)(nil)
