package imztop

import "sort"

// NamedSeries is one labelled history series in a published snapshot
// (e.g. "sda" → read rates over the last 10 minutes). The slice is a
// fresh copy of the sampler's ring backing memory; mutating it does
// not affect future ticks.
type NamedSeries struct {
	Name string
	Y    []float64
}

// indexedWindowSet manages history rings keyed by integer index — used
// for per-CPU-core and per-GPU-device series where the index is
// stable. Rings are lazily allocated when a new index appears; on
// disappearance the ring is kept and receives 0 pushes so all rings
// stay length-aligned with the time ring.
type indexedWindowSet struct {
	histN int32
	rings []*SlidingWindow[float64]
}

func newIndexedWindowSet(histN int32) (inst *indexedWindowSet) {
	inst = &indexedWindowSet{histN: histN}
	return
}

// targetLen is the length all existing rings already have BEFORE this
// tick's push — derived from any one ring (they must all be equal).
func (inst *indexedWindowSet) targetLen() (n int32) {
	if len(inst.rings) == 0 {
		return
	}
	n = inst.rings[0].Len()
	return
}

// push records `values` for this tick. New indices grow the slice
// (padded with zeros to the existing length); indices that disappeared
// receive a 0 push to keep alignment with the rest of the rings.
func (inst *indexedWindowSet) push(values []float64) {
	target := inst.targetLen()
	for len(inst.rings) < len(values) {
		r := NewSlidingWindow[float64](inst.histN)
		for r.Len() < target {
			r.Push(0)
		}
		inst.rings = append(inst.rings, r)
	}
	for i, v := range values {
		inst.rings[i].Push(v)
	}
	for i := len(values); i < len(inst.rings); i++ {
		inst.rings[i].Push(0)
	}
}

func (inst *indexedWindowSet) snapshot() (out [][]float64) {
	out = make([][]float64, len(inst.rings))
	for i, r := range inst.rings {
		out[i] = copyFloats(r.Values())
	}
	return
}

// NamedValue is a (key, value) pair for namedWindowSet.push.
type NamedValue struct {
	Name  string
	Value float64
}

// namedWindowSet manages history rings keyed by a stable string name —
// used for per-block-device and per-network-interface series. Iteration
// order is deterministic (sorted ASCII) so the renderer assigns
// consistent colours across frames. Names that disappear from the
// current tick still receive a 0 push to maintain length alignment.
type namedWindowSet struct {
	histN  int32
	byName map[string]*SlidingWindow[float64]
	order  []string
}

func newNamedWindowSet(histN int32) (inst *namedWindowSet) {
	inst = &namedWindowSet{
		histN:  histN,
		byName: map[string]*SlidingWindow[float64]{},
	}
	return
}

func (inst *namedWindowSet) targetLen() (n int32) {
	for _, r := range inst.byName {
		n = r.Len()
		return
	}
	return
}

func (inst *namedWindowSet) push(pairs []NamedValue) {
	target := inst.targetLen()
	seen := make(map[string]struct{}, len(pairs))
	for _, p := range pairs {
		seen[p.Name] = struct{}{}
		r, ok := inst.byName[p.Name]
		if !ok {
			r = NewSlidingWindow[float64](inst.histN)
			for r.Len() < target {
				r.Push(0)
			}
			inst.byName[p.Name] = r
			inst.order = append(inst.order, p.Name)
			sort.Strings(inst.order)
		}
		r.Push(p.Value)
	}
	for name, r := range inst.byName {
		if _, ok := seen[name]; ok {
			continue
		}
		r.Push(0)
	}
}

func (inst *namedWindowSet) snapshot() (out []NamedSeries) {
	out = make([]NamedSeries, 0, len(inst.order))
	for _, name := range inst.order {
		out = append(out, NamedSeries{
			Name: name,
			Y:    copyFloats(inst.byName[name].Values()),
		})
	}
	return
}
