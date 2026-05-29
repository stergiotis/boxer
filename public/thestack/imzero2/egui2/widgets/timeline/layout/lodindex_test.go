//go:build llm_generated_opus47

package layout

import (
	"testing"
	"time"
)

var defaultScales = []time.Duration{
	1 * time.Millisecond,
	1 * time.Second,
	1 * time.Minute,
	1 * time.Hour,
	24 * time.Hour,
}

func TestBuildLODIndex_EmptyEvents(t *testing.T) {
	idx := BuildLODIndex(nil, defaultScales)
	if idx.Len() != 0 {
		t.Errorf("Len: got %d want 0", idx.Len())
	}
	if idx.ScaleCount() != int32(len(defaultScales)) {
		t.Errorf("ScaleCount: got %d want %d", idx.ScaleCount(), len(defaultScales))
	}
}

func TestBuildLODIndex_EmptyScales(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{{TMS: 0}}, nil)
	if idx.ScaleCount() != 0 {
		t.Errorf("ScaleCount: got %d want 0", idx.ScaleCount())
	}
	if buckets := idx.BucketsForRange(0, 1000, 100); buckets != nil {
		t.Errorf("empty-scales BucketsForRange: got %v want nil", buckets)
	}
}

func TestBuildLODIndex_SingleEvent_OneBucketPerScale(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{{TMS: 5000, Intensity: 0.5}}, defaultScales)
	if idx.Len() != 1 {
		t.Errorf("Len: got %d want 1", idx.Len())
	}
	got := idx.BucketsForRange(0, 60_000, 100)
	if len(got) != 1 {
		t.Fatalf("buckets: got %d want 1", len(got))
	}
	if got[0].Count != 1 {
		t.Errorf("Count: got %d want 1", got[0].Count)
	}
	if got[0].SumIntensity != 0.5 {
		t.Errorf("SumIntensity: got %v want 0.5", got[0].SumIntensity)
	}
}

func TestBuildLODIndex_TwoEventsSameBucket_Aggregated(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{
		{TMS: 1000, Intensity: 0.3},
		{TMS: 1500, Intensity: 0.7},
	}, defaultScales)
	got := idx.BucketsForRange(0, 60_000, 100)
	if len(got) != 1 {
		t.Fatalf("buckets: got %d want 1 (both events fall in same minute)", len(got))
	}
	if got[0].Count != 2 {
		t.Errorf("Count: got %d want 2", got[0].Count)
	}
	if got[0].SumIntensity < 0.999 || got[0].SumIntensity > 1.001 {
		t.Errorf("SumIntensity: got %v want ~1.0", got[0].SumIntensity)
	}
}

func TestBuildLODIndex_NegativeTMS_FloorsToward_NegInf(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{{TMS: -1, Intensity: 1.0}}, []time.Duration{1 * time.Second})
	got := idx.BucketsForRange(-2000, 1000, 100)
	if len(got) != 1 {
		t.Fatalf("buckets: got %d want 1", len(got))
	}
	if got[0].StartMS != -1000 {
		t.Errorf("StartMS: got %d want -1000 (floor toward -Inf)", got[0].StartMS)
	}
}

func TestPickScale_SmallestScaleAtLeastMinPerPx(t *testing.T) {
	scales := []time.Duration{
		1 * time.Millisecond,
		10 * time.Millisecond,
		100 * time.Millisecond,
		1 * time.Second,
	}
	idx := BuildLODIndex(nil, scales)
	cases := []struct {
		name              string
		t0, t1            int64
		pxWidth, wantIdx  int32
	}{
		{"exactly_at_boundary", 0, 100, 10, 1},
		{"just_above_finest", 0, 20, 10, 1},
		{"crosses_to_100ms", 0, 200, 10, 2},
		{"crosses_to_1s", 0, 2000, 10, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := idx.PickScale(tc.t0, tc.t1, tc.pxWidth); got != tc.wantIdx {
				t.Errorf("PickScale(%d,%d,%d): got %d want %d",
					tc.t0, tc.t1, tc.pxWidth, got, tc.wantIdx)
			}
		})
	}
}

func TestPickScale_WideRangeWantsCoarseScale(t *testing.T) {
	idx := BuildLODIndex(nil, defaultScales)
	got := idx.PickScale(0, int64(365*24*time.Hour/time.Millisecond), 1000)
	if got != int32(len(defaultScales)-1) {
		t.Errorf("year-range PickScale: got %d want %d (1d)", got, len(defaultScales)-1)
	}
}

func TestPickScale_DegenerateRange_ReturnsZero(t *testing.T) {
	idx := BuildLODIndex(nil, defaultScales)
	if got := idx.PickScale(0, 0, 100); got != 0 {
		t.Errorf("zero-range PickScale: got %d want 0", got)
	}
	if got := idx.PickScale(0, 100, 0); got != 0 {
		t.Errorf("zero-width PickScale: got %d want 0", got)
	}
}

func TestPickScale_OneMinuteRange_PicksOneSecondScale(t *testing.T) {
	idx := BuildLODIndex(nil, defaultScales)
	t1 := int64(60 * time.Second / time.Millisecond)
	got := idx.PickScale(0, t1, 1000)
	if got != 1 {
		t.Errorf("PickScale: got scale idx %d want 1 (1s); scales=%v", got, defaultScales)
	}
}

func TestBucketsForRange_SparseEvents_NoEmptyBucketsReturned(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{
		{TMS: 0, Intensity: 1},
		{TMS: 60_000, Intensity: 1},
	}, []time.Duration{1 * time.Second})
	got := idx.BucketsForRange(0, 70_000, 70)
	if len(got) != 2 {
		t.Fatalf("buckets: got %d want 2 (sparse — empties not materialized)", len(got))
	}
	if got[0].StartMS != 0 || got[1].StartMS != 60_000 {
		t.Errorf("StartMS order: got [%d,%d] want [0,60000]", got[0].StartMS, got[1].StartMS)
	}
}

func TestBucketsForRange_AscendingOrder(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{
		{TMS: 5_000},
		{TMS: 1_000},
		{TMS: 3_000},
	}, []time.Duration{1 * time.Second})
	got := idx.BucketsForRange(0, 6_000, 60)
	if len(got) != 3 {
		t.Fatalf("buckets: got %d want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].StartMS >= got[i].StartMS {
			t.Errorf("not ascending at i=%d: got [%d,%d]", i, got[i-1].StartMS, got[i].StartMS)
		}
	}
}

func TestBucketsForRange_PartialOverlap_HalfOpenSemantics(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{
		{TMS: 999},
		{TMS: 1000},
		{TMS: 1999},
		{TMS: 2000},
	}, []time.Duration{1 * time.Second})
	got := idx.BucketsForRange(1000, 2000, 10)
	if len(got) != 1 {
		t.Fatalf("buckets: got %d want 1 (half-open: [1000,2000) yields one 1s bucket)", len(got))
	}
	if got[0].Count != 2 {
		t.Errorf("Count in [1000,2000): got %d want 2 (events at 1000 + 1999)", got[0].Count)
	}
	if got[0].StartMS != 1000 {
		t.Errorf("StartMS: got %d want 1000", got[0].StartMS)
	}
}

func TestBuildLODIndex_PanicsOnNonAscendingScales(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on non-ascending scales, got none")
		}
	}()
	BuildLODIndex([]PointEvent{{TMS: 0}},
		[]time.Duration{1 * time.Second, 500 * time.Millisecond})
}

func TestBuildLODIndex_ZeroScale_ClampedToOneMs(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{{TMS: 0}}, []time.Duration{0})
	if idx.ScaleMS(0) != 1 {
		t.Errorf("zero scale should clamp to 1ms, got %d", idx.ScaleMS(0))
	}
}

func TestScaleMS_OutOfRange_ReturnsZero(t *testing.T) {
	idx := BuildLODIndex(nil, defaultScales)
	if got := idx.ScaleMS(-1); got != 0 {
		t.Errorf("ScaleMS(-1): got %d want 0", got)
	}
	if got := idx.ScaleMS(99); got != 0 {
		t.Errorf("ScaleMS(99): got %d want 0", got)
	}
}

func TestBucketAt_HitsTheBucketContainingT(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{
		{TMS: 1500, Intensity: 0.5},
		{TMS: 2500, Intensity: 0.3},
	}, []time.Duration{1 * time.Second})
	// Cursor at 1700ms → 1-second bucket [1000, 2000) → contains 1500.
	b, scaleMS, ok := idx.BucketAt(1700, 0, 5000, 100)
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if scaleMS != 1000 {
		t.Errorf("scaleMS: got %d want 1000", scaleMS)
	}
	if b.StartMS != 1000 {
		t.Errorf("StartMS: got %d want 1000", b.StartMS)
	}
	if b.Count != 1 {
		t.Errorf("Count: got %d want 1", b.Count)
	}
}

func TestBucketAt_MissReturnsOkFalse(t *testing.T) {
	idx := BuildLODIndex([]PointEvent{{TMS: 1500}}, []time.Duration{1 * time.Second})
	// Cursor at 5500ms — no event in [5000, 6000).
	_, _, ok := idx.BucketAt(5500, 0, 10000, 100)
	if ok {
		t.Error("expected miss, got hit")
	}
}

func TestBucketAt_EmptyIndexReturnsOkFalse(t *testing.T) {
	idx := BuildLODIndex(nil, nil)
	_, _, ok := idx.BucketAt(0, 0, 1000, 100)
	if ok {
		t.Error("expected miss on empty index")
	}
}

func TestScaleMSForRange_MatchesPickScale(t *testing.T) {
	scales := []time.Duration{
		1 * time.Millisecond,
		10 * time.Millisecond,
		100 * time.Millisecond,
		1 * time.Second,
	}
	idx := BuildLODIndex(nil, scales)
	cases := []struct {
		t0, t1   int64
		pxWidth  int32
		wantIdx  int32
	}{
		{0, 100, 10, 1},
		{0, 2000, 10, 3},
	}
	for _, tc := range cases {
		gotMS := idx.ScaleMSForRange(tc.t0, tc.t1, tc.pxWidth)
		wantMS := idx.ScaleMS(tc.wantIdx)
		if gotMS != wantMS {
			t.Errorf("ScaleMSForRange(%d,%d,%d): got %d want %d (scale idx %d)",
				tc.t0, tc.t1, tc.pxWidth, gotMS, wantMS, tc.wantIdx)
		}
	}
}

func TestFloorBin_PositiveValues(t *testing.T) {
	cases := []struct {
		num, denom, want int64
	}{
		{0, 10, 0},
		{5, 10, 0},
		{10, 10, 10},
		{15, 10, 10},
		{999, 1000, 0},
		{1000, 1000, 1000},
	}
	for _, tc := range cases {
		if got := floorBin(tc.num, tc.denom); got != tc.want {
			t.Errorf("floorBin(%d,%d): got %d want %d", tc.num, tc.denom, got, tc.want)
		}
	}
}

func TestFloorBin_NegativeValues(t *testing.T) {
	cases := []struct {
		num, denom, want int64
	}{
		{-1, 10, -10},
		{-10, 10, -10},
		{-11, 10, -20},
		{-999, 1000, -1000},
		{-1000, 1000, -1000},
		{-1001, 1000, -2000},
	}
	for _, tc := range cases {
		if got := floorBin(tc.num, tc.denom); got != tc.want {
			t.Errorf("floorBin(%d,%d): got %d want %d (toward -Inf)", tc.num, tc.denom, got, tc.want)
		}
	}
}
