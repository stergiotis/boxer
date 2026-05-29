//go:build llm_generated_opus47

package layout

import (
	"fmt"
	"time"
)

// Bucket is one cell of the LOD index: a half-open [StartMS, StartMS+ScaleMS)
// bin containing aggregated counts and intensity sum for any PointEvent
// whose TMS falls inside it.
type Bucket struct {
	StartMS      int64
	Count        int32
	SumIntensity float32
}

// LODIndex holds pre-aggregated point-event bins at multiple time scales.
//
// Built once per data change (BuildLODIndex), queried per frame
// (BucketsForRange). The caller chooses which scales to pre-aggregate;
// the typical ladder for a log-style timeline is roughly
// [1ms, 10ms, 100ms, 1s, 10s, 1m, 10m, 1h, 1d, 1w] but any ascending
// sequence works.
//
// Construction cost is O(N × S) where N is event count and S is scale
// count; query cost is O(visible-buckets-at-chosen-scale). For 100k
// events across ~6 scales the build is ~600k map ops — fine for a
// one-time pass off the render path.
//
// Read-only after construction; concurrent reads are safe. Use the
// constructor BuildLODIndex; the zero value is not useful.
type LODIndex struct {
	scales  []int64
	bins    []map[int64]*Bucket
	nEvents int32
}

// BuildLODIndex aggregates events into bins at each provided scale.
//
// scales is interpreted as time-per-bucket and MUST be strictly ascending
// in millisecond precision. Sub-millisecond scales are clamped to 1 ms
// (the wire precision); a non-ascending pair panics with a clear message
// — silent auto-bump (the previous behaviour) papered over caller bugs
// that produced subtly-wrong LOD choices later.
//
// Negative TMS values (pre-1970 events) bin correctly via floor-division.
//
// An empty scales slice produces an index whose queries always return
// empty bucket lists; use this as a "no LOD" sentinel.
func BuildLODIndex(events []PointEvent, scales []time.Duration) (idx *LODIndex) {
	idx = &LODIndex{}
	if len(scales) == 0 {
		return
	}
	idx.scales = make([]int64, len(scales))
	for i, s := range scales {
		ms := s.Milliseconds()
		if ms <= 0 {
			ms = 1
		}
		if i > 0 && ms <= idx.scales[i-1] {
			panic(fmt.Sprintf("layout: BuildLODIndex requires strictly ascending scales (ms-precision); scales[%d]=%dms <= scales[%d]=%dms",
				i, ms, i-1, idx.scales[i-1]))
		}
		idx.scales[i] = ms
	}
	idx.bins = make([]map[int64]*Bucket, len(idx.scales))
	for i := range idx.bins {
		idx.bins[i] = make(map[int64]*Bucket)
	}
	idx.nEvents = int32(len(events))
	for _, ev := range events {
		for i, scaleMS := range idx.scales {
			start := floorBin(ev.TMS, scaleMS)
			b, ok := idx.bins[i][start]
			if !ok {
				b = &Bucket{StartMS: start}
				idx.bins[i][start] = b
			}
			b.Count++
			b.SumIntensity += ev.Intensity
		}
	}
	return
}

// PickScale returns the index of the smallest scale whose bin width in ms
// is >= (t1-t0) / pxWidth — i.e. the finest LOD that still guarantees at
// most one bucket per pixel column.
//
// Returns the coarsest scale when no finer one fits (very wide view), and
// scale index 0 for degenerate input — keeps the widget useful instead of
// returning empty.
func (inst *LODIndex) PickScale(t0, t1 int64, pxWidth int32) (scaleIdx int32) {
	if len(inst.scales) == 0 || pxWidth <= 0 || t1 <= t0 {
		return
	}
	minPerPx := max((t1-t0)/int64(pxWidth), 1)
	for i, s := range inst.scales {
		if s >= minPerPx {
			scaleIdx = int32(i)
			return
		}
	}
	scaleIdx = int32(len(inst.scales) - 1)
	return
}

// BucketsForRange returns buckets whose StartMS intersects [t0, t1) at the
// scale chosen by PickScale. Output is sorted ascending by StartMS and is
// safe to mutate by the caller (each Bucket is a copy).
//
// Empty range or empty index yields a nil slice.
func (inst *LODIndex) BucketsForRange(t0, t1 int64, pxWidth int32) (buckets []Bucket) {
	if len(inst.scales) == 0 || t1 <= t0 {
		return
	}
	scaleIdx := inst.PickScale(t0, t1, pxWidth)
	scaleMS := inst.scales[scaleIdx]
	bins := inst.bins[scaleIdx]
	startBin := floorBin(t0, scaleMS)
	endBin := floorBin(t1-1, scaleMS) + scaleMS
	approxN := min(int((endBin-startBin)/scaleMS)+1, len(bins))
	buckets = make([]Bucket, 0, approxN)
	for k := startBin; k < endBin; k += scaleMS {
		if b, ok := bins[k]; ok {
			buckets = append(buckets, *b)
		}
	}
	return
}

// ScaleMS returns the bin width in milliseconds at the given scale index,
// or 0 for an out-of-range index.
func (inst *LODIndex) ScaleMS(scaleIdx int32) (ms int64) {
	if scaleIdx < 0 || int(scaleIdx) >= len(inst.scales) {
		return
	}
	ms = inst.scales[scaleIdx]
	return
}

// ScaleMSForRange returns the bin width in milliseconds at the scale that
// PickScale would choose for the same inputs. Convenience for renderers
// that need both the buckets and their width without restating the picker
// inputs.
func (inst *LODIndex) ScaleMSForRange(t0, t1 int64, pxWidth int32) (ms int64) {
	ms = inst.ScaleMS(inst.PickScale(t0, t1, pxWidth))
	return
}

// BucketAt looks up the single bucket containing tMS at the scale that
// PickScale would choose for the view [t0, t1] at pxWidth. Returns the
// bucket, the picked scale width, and ok=false on miss (empty index,
// degenerate inputs, no event in that bucket). Use for per-frame cursor
// hit-tests — much cheaper than BucketsForRange when the caller wants
// exactly one bucket and not the whole visible slice.
func (inst *LODIndex) BucketAt(tMS int64, t0, t1 int64, pxWidth int32) (bucket Bucket, scaleMS int64, ok bool) {
	if len(inst.scales) == 0 {
		return
	}
	scaleIdx := inst.PickScale(t0, t1, pxWidth)
	scaleMS = inst.scales[scaleIdx]
	if scaleMS <= 0 {
		return
	}
	start := floorBin(tMS, scaleMS)
	b, found := inst.bins[scaleIdx][start]
	if !found {
		return
	}
	bucket = *b
	ok = true
	return
}

// ScaleCount returns the number of pre-aggregated scales.
func (inst *LODIndex) ScaleCount() (n int32) {
	n = int32(len(inst.scales))
	return
}

// Len reports the number of events indexed (sum of all events fed to Build).
func (inst *LODIndex) Len() (n int32) {
	n = inst.nEvents
	return
}

// floorBin rounds num down to a multiple of denom, toward -Inf.
// Avoids Go's truncated-division surprise for negative num.
func floorBin(num, denom int64) (start int64) {
	q := num / denom
	r := num % denom
	if r != 0 && (r < 0) != (denom < 0) {
		q--
	}
	start = q * denom
	return
}
