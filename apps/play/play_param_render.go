package play

import (
	"maps"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// refreshParamSlotsFromParse is called from updatePreview after a
// successful parse. It refreshes inst.paramSlots, ensures every slot
// has a stable-pointer draft entry, and overwrites drafts whose
// prelude value differs (parser wins on text edits). Slots that
// disappeared from the buffer have their drafts evicted; the
// paramSyncedValues cache mirrors the new prelude exactly so the
// post-render drift check stays a no-op until a widget actually
// mutates.
//
// Called only when ExtractParamSlots and ExtractParams have already
// succeeded for inst.sql.
func (inst *PlayApp) refreshParamSlotsFromParse(slots []paramSlot, preludeValues map[string]string) {
	inst.paramSlots = slots

	newDrafts := make(map[string]*string, len(slots))
	newSynced := make(map[string]string, len(slots))
	for _, s := range slots {
		ptr, kept := inst.paramDrafts[s.Name]
		if !kept {
			v := ""
			ptr = &v
		}
		if v, hit := preludeValues["param_"+s.Name]; hit {
			*ptr = v
			newSynced[s.Name] = v
		}
		newDrafts[s.Name] = ptr
	}
	inst.paramDrafts = newDrafts
	inst.paramSyncedValues = newSynced

	present := make(map[string]struct{}, len(slots))
	for _, s := range slots {
		present[s.Name] = struct{}{}
	}
	for _, w := range inst.paramWidgets {
		w.ClearStateForAbsent(present)
	}
}

// renderParamSlots draws the per-slot widgets above the SQL editor,
// closed by a horizontal rule that divides them from the editor below.
// Each registered widget is offered the remaining (unconsumed) slots in
// editor order; the scalarTextWidget at the tail is the catch-all so
// every slot renders something.
//
// After dispatch, the function compares each draft to its last
// prelude-synced value; on drift it calls SyncParamPrelude and
// commits the new sql + updated cache. Widget-driven mutations
// surface one frame after the click (the FFFI2 SendRespVal apply
// runs at end-of-frame), which is acceptable for picker UX.
func (inst *PlayApp) renderParamSlots() {
	slots := inst.paramSlots
	if len(slots) == 0 {
		return
	}

	for rt := range c.RichTextLabel("PARAMETERS") {
		rt.Small().Weak()
	}

	consumed := make([]bool, len(slots))
	// grouped tracks the slots a group widget folded, which is what §SD7's
	// near-miss pass reports on. It cannot read `consumed` instead: the tail
	// scalarTextWidget claims every remaining slot, so by the end of dispatch
	// nothing is unconsumed and the interesting set would be empty.
	grouped := make([]bool, len(slots))
	ungroup := scanUngroupHint(inst.sql)
	for _, w := range inst.paramWidgets {
		if ungroup && w.IsGroup() {
			continue
		}
		remaining := unconsumedSlots(slots, consumed)
		if len(remaining) == 0 {
			break
		}
		// Repeated dispatch lets one widget claim multiple disjoint
		// matches in a single frame (e.g. two from/to pairs in one
		// query). The empty-idx guard is defensive: a misbehaving
		// widget that returns ok=true with nil indices would consume
		// nothing yet re-match identically next iteration.
		for {
			idxInRemaining, ok := w.Matches(remaining)
			if !ok || len(idxInRemaining) == 0 {
				break
			}
			subset := make([]paramSlot, 0, len(idxInRemaining))
			absoluteIdx := make([]int, 0, len(idxInRemaining))
			for _, ri := range idxInRemaining {
				abs := absoluteIndex(slots, consumed, ri)
				if abs < 0 || consumed[abs] {
					break
				}
				subset = append(subset, slots[abs])
				absoluteIdx = append(absoluteIdx, abs)
			}
			if len(subset) != len(idxInRemaining) {
				break
			}
			for _, a := range absoluteIdx {
				consumed[a] = true
				grouped[a] = w.IsGroup()
			}
			if w.IsGroup() {
				inst.renderFoldLabel(subset)
			}
			w.Render(&paramCtx{
				Ids:    inst.ids,
				Slots:  subset,
				Drafts: inst.paramDrafts,
			})
			remaining = unconsumedSlots(slots, consumed)
			if len(remaining) == 0 {
				break
			}
		}
	}

	inst.renderNearMissNote(slots, grouped, ungroup)

	// Divider between the parameter block and the SQL editor below it.
	c.Separator().Horizontal().Send()

	inst.syncParamDriftToPrelude()
}

// renderFoldLabel names a fold the registry inferred and its opt-out, so the
// inference is legible and reversible rather than magic (ADR-0124 §SD7).
//
// The evaluator note closes the one gap §SD3 leaves open: a picker that
// degraded to two calendar buttons because no evaluator was wired is otherwise
// two different UIs for one query shape with nothing saying why. It is decided
// here rather than in the widget because a widget that had to explain why it
// was chosen would need to know about the alternatives it was chosen over —
// dateTimePairWidget does not know an evaluator exists, and coupling it to
// §SD3 for a label would be the wrong trade.
func (inst *PlayApp) renderFoldLabel(subset []paramSlot) {
	if len(subset) != 2 {
		return
	}
	// En dash, not U+2192: the host's main font (NotoSans) has no arrow glyph,
	// so one would render only via the CJK mono fallback — a wrong-metric glyph
	// in a proportional label, and tofu if that fallback ever goes away.
	note := "range · " + subset[0].Name + " – " + subset[1].Name
	if inst.paramEvaluator == nil {
		note += " · no evaluator: expressions unavailable"
	}
	note += ` · "-- play: ungroup" splits it`
	for rt := range c.RichTextLabel(note) {
		rt.Small().Weak()
	}
}

// renderNearMissNote draws §SD7's single advisory line about folds that did not
// happen. Advisory only: it never gates execution, and a query that ignores it
// behaves exactly as it did.
func (inst *PlayApp) renderNearMissNote(slots []paramSlot, grouped []bool, ungroup bool) {
	unfolded := make([]paramSlot, 0, len(slots))
	for i, s := range slots {
		if !grouped[i] {
			unfolded = append(unfolded, s)
		}
	}
	note := nearMissNote(unfolded, ungroup)
	if note == "" {
		return
	}
	for rt := range c.RichTextLabel(note) {
		rt.Small().Weak()
	}
}

// syncParamDriftToPrelude compares each draft to its last-synced
// value and, on any drift, rebuilds the editor's leading SET prelude.
// Idempotent — no-ops when drafts match the cache.
func (inst *PlayApp) syncParamDriftToPrelude() {
	if len(inst.paramSlots) == 0 {
		return
	}
	values := make(map[string]string, len(inst.paramSlots))
	drift := false
	for _, s := range inst.paramSlots {
		ptr, ok := inst.paramDrafts[s.Name]
		if !ok {
			continue
		}
		values[s.Name] = *ptr
		if inst.paramSyncedValues[s.Name] != *ptr {
			drift = true
		}
	}
	if !drift {
		return
	}
	out, changed := SyncParamPrelude(inst.sql, inst.paramSlots, values)
	if !changed {
		// Parse failure inside SyncParamPrelude — leave inst.sql alone
		// and refresh the cache so we stop re-trying every frame for
		// the same transient broken state.
		maps.Copy(inst.paramSyncedValues, values)
		return
	}
	inst.sql = out
	maps.Copy(inst.paramSyncedValues, values)
}

func unconsumedSlots(slots []paramSlot, consumed []bool) (out []paramSlot) {
	out = make([]paramSlot, 0, len(slots))
	for i, s := range slots {
		if !consumed[i] {
			out = append(out, s)
		}
	}
	return
}

// absoluteIndex maps a "ri-th unconsumed slot" back to the index in
// the original slots slice, skipping anything already consumed.
// Returns -1 when ri is out of range.
func absoluteIndex(slots []paramSlot, consumed []bool, ri int) int {
	seen := 0
	for i := range slots {
		if consumed[i] {
			continue
		}
		if seen == ri {
			return i
		}
		seen++
	}
	return -1
}
