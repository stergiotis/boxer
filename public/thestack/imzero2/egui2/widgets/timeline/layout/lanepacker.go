//go:build llm_generated_opus47

package layout

import (
	"container/heap"
	"sort"
)

// Lane is an ordered run of IntervalEvents drawn on one timeline row.
//
// For auto-packed lanes (empty Hint), items are guaranteed non-overlapping
// by the greedy algorithm. For hint-pinned lanes (non-empty Hint), items
// share the row by caller intent and MAY overlap visually — the caller
// took responsibility for that lane's contents when they set LaneHint.
type Lane struct {
	Hint  string
	Items []*IntervalEvent
}

// LaneAssignment is the output of PackLanes: lane list + a reverse lookup
// from each input event pointer to its assigned lane index.
//
// Direct EventLane access is a footgun — a missing key reads as 0, which
// is indistinguishable from "event is in lane 0". Use LaneOf for safe
// lookups. nil event pointers are filtered during packing and have no entry.
type LaneAssignment struct {
	Lanes     []Lane
	EventLane map[*IntervalEvent]int32
}

// LaneOf returns the lane index assigned to ev, with ok=false when ev was
// not part of the input (or was filtered as nil during packing). Prefer
// this over `assn.EventLane[ev]` to avoid the missing-key-reads-as-0
// ambiguity.
func (inst *LaneAssignment) LaneOf(ev *IntervalEvent) (idx int32, ok bool) {
	idx, ok = inst.EventLane[ev]
	return
}

// PackLanes assigns IntervalEvents to non-overlapping lanes via the standard
// greedy left-to-right strategy used by d3-layout-timeline, perfetto, and
// most general-purpose Gantt renderers:
//
//  1. Events with non-empty LaneHint are pinned to one lane per distinct
//     hint. Hint lanes appear first in Lanes, in first-seen order. Items
//     inside a hint lane are sorted by FromMS but may overlap — pinning is
//     a caller-asserted invariant, not enforced by the packer.
//
//  2. Unhinted events are sorted by FromMS (stable, so ties preserve input
//     order) then placed in the auto-lane that became free *earliest* (i.e.
//     the lane whose last item has the smallest ToMS) — equivalent to the
//     classic earliest-finish-first interval-scheduling greedy. Ties on
//     lastTo break toward the smaller laneIdx for deterministic output. A
//     new auto-lane is appended when no existing lane has freed up in time.
//
// Complexity: O(n log n) — sort + heap operations. The earlier
// linear-scan-per-event implementation was O(n × L_auto) which degraded to
// O(n²) for fully-overlapping input (each event creates a new lane); the
// heap-based placement makes that worst case O(n log n) too. Note the
// behavioural shift: previously the lowest-indexed-lane-that-fits won;
// now the earliest-freed-lane wins (same lane count, possibly different
// per-event assignment for mixed-overlap inputs). Both are valid greedy
// schedules.
//
// Nil event pointers in the input are filtered silently and have no
// EventLane entry. An entirely-empty (or all-nil) input returns an
// assignment with empty Lanes and empty EventLane.
func PackLanes(events []*IntervalEvent) (assn LaneAssignment) {
	assn.EventLane = make(map[*IntervalEvent]int32, len(events))
	if len(events) == 0 {
		return
	}

	var (
		hintOrder      []string
		hintLaneByName = make(map[string]int32)
		autoCandidates []*IntervalEvent
	)
	for _, ev := range events {
		if ev == nil || ev.Validate() != nil {
			continue
		}
		if ev.LaneHint != "" {
			if _, seen := hintLaneByName[ev.LaneHint]; !seen {
				hintLaneByName[ev.LaneHint] = int32(len(hintOrder))
				hintOrder = append(hintOrder, ev.LaneHint)
			}
			continue
		}
		autoCandidates = append(autoCandidates, ev)
	}

	assn.Lanes = make([]Lane, 0, len(hintOrder)+8)
	for _, h := range hintOrder {
		assn.Lanes = append(assn.Lanes, Lane{Hint: h})
	}

	for _, ev := range events {
		if ev == nil || ev.LaneHint == "" || ev.Validate() != nil {
			continue
		}
		idx := hintLaneByName[ev.LaneHint]
		assn.Lanes[idx].Items = append(assn.Lanes[idx].Items, ev)
		assn.EventLane[ev] = idx
	}
	for i := range hintOrder {
		sortByFromMS(assn.Lanes[i].Items)
	}

	sortByFromMS(autoCandidates)

	h := &laneHeap{}
	for _, ev := range autoCandidates {
		if h.Len() > 0 && (*h)[0].lastTo <= ev.FromMS {
			slot := heap.Pop(h).(laneSlot)
			slot.lastTo = ev.ToMS
			heap.Push(h, slot)
			assn.Lanes[slot.laneIdx].Items = append(assn.Lanes[slot.laneIdx].Items, ev)
			assn.EventLane[ev] = slot.laneIdx
			continue
		}
		newIdx := int32(len(assn.Lanes))
		assn.Lanes = append(assn.Lanes, Lane{Items: []*IntervalEvent{ev}})
		heap.Push(h, laneSlot{lastTo: ev.ToMS, laneIdx: newIdx})
		assn.EventLane[ev] = newIdx
	}
	return
}

// LaneCount returns the total lane count (hint + auto).
func (inst *LaneAssignment) LaneCount() (n int32) {
	n = int32(len(inst.Lanes))
	return
}

func sortByFromMS(items []*IntervalEvent) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].FromMS < items[j].FromMS
	})
}

// laneSlot is a heap entry: one auto-lane's most-recent ToMS + its index
// into LaneAssignment.Lanes. Min-heap ordering on lastTo (earliest-freed
// lane wins) with laneIdx as a deterministic tiebreaker.
type laneSlot struct {
	lastTo  int64
	laneIdx int32
}

type laneHeap []laneSlot

func (h laneHeap) Len() int { return len(h) }
func (h laneHeap) Less(i, j int) (less bool) {
	if h[i].lastTo != h[j].lastTo {
		less = h[i].lastTo < h[j].lastTo
		return
	}
	less = h[i].laneIdx < h[j].laneIdx
	return
}
func (h laneHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *laneHeap) Push(x any)        { *h = append(*h, x.(laneSlot)) }
func (h *laneHeap) Pop() (popped any) {
	old := *h
	n := len(old)
	popped = old[n-1]
	*h = old[:n-1]
	return
}
