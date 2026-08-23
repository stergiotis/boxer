package play

import (
	"maps"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
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

	// The expression declarations, read off the same buffer this parse
	// describes (play_renderer.go parses inst.sql verbatim). Scanned here
	// rather than passed in for the reason renderParamSlots re-scans the enum
	// and ungroup markers per frame: a marker is part of the buffer.
	exprHints := scanExprHints(inst.sql)
	// The error mark the fields draw, derived once per parse rather than per
	// frame: a field edit rewrites its own directive line, so the buffer moves
	// whenever a value does and this parse is where both land (§SD6).
	inst.exprMarks = computeExprMarks(inst.sql, exprHints)

	newDrafts := make(map[string]*string, len(slots))
	newSynced := make(map[string]string, len(slots))
	newSyncedExprs := make(map[string]string, len(slots))
	for _, s := range slots {
		ptr, kept := inst.paramDrafts[s.Name]
		if !kept {
			v := ""
			ptr = &v
		}
		if exprCategoryFor(s.Type).spliced() {
			// A client-side-substituted slot has no prelude tier at all
			// (ADR-0187 §SD2), so it never enters newSynced: its
			// declared value is the `-- play: expr` line, and putting it in the
			// prelude would ship an expression to the server as a string.
			//
			// The declaration wins over the draft, exactly as the prelude does
			// at the pinned tier and for the same reason: drift rewrites the
			// directive first (syncExprDrift), so by the time this parse runs
			// the two already agree and the overwrite is a no-op.
			if v, declared := exprHints[s.Name]; declared {
				*ptr = v
				newSyncedExprs[s.Name] = v
			} else if !kept {
				// No declaration: the slot is at the LIVE tier, and its draft
				// is born from the store exactly as a live value slot's is, so
				// a panel already publishing the name shows its predicate the
				// first frame the field appears.
				if raw, held := inst.signalRawFor(s.Name); held {
					*ptr = raw
					inst.noteLiveSeeded(s.Name, raw)
				}
			}
			newDrafts[s.Name] = ptr
			continue
		}
		if v, hit := preludeValues["param_"+s.Name]; hit {
			// The parser wins — but only at the PINNED tier, which is what a
			// prelude value means (ADR-0124's 2026-07-22 §SD4 amendment). A
			// live name has no prelude entry, so it never reaches here; its
			// draft follows the store instead.
			*ptr = v
			newSynced[s.Name] = v
		} else if !kept {
			// A live draft is born from the store, so a name a panel already
			// publishes (a Timeline extent, a viewport bound) shows its
			// current value the first frame its widget appears rather than an
			// empty field that would then read as drift and clobber it.
			if raw, held := inst.signalRawFor(s.Name); held {
				*ptr = raw
				inst.noteLiveSeeded(s.Name, raw)
			}
		}
		newDrafts[s.Name] = ptr
	}
	inst.paramDrafts = newDrafts
	inst.paramSyncedValues = newSynced
	inst.paramSyncedExprs = newSyncedExprs

	present := make(map[string]struct{}, len(slots))
	for _, s := range slots {
		present[s.Name] = struct{}{}
	}
	for name := range inst.paramLiveSeeded {
		if _, keep := present[name]; !keep {
			delete(inst.paramLiveSeeded, name)
		}
	}
	for _, w := range inst.paramWidgets {
		w.ClearStateForAbsent(present)
	}
}

// paramPinned reports a name's tier (ADR-0124's 2026-07-22 §SD4 amendment):
// a name the buffer SET-binds is PINNED — its drift edits the prelude and the
// buffer stays a self-contained artifact; a name without a SET is LIVE — its
// drift is a provenance'd store write. The bit is derived from the prelude
// mirror the debounced parse maintains, never stored, so deleting a SET line
// by hand and clicking unpin are the same gesture.
// A SQL-valued slot has a second source for the same bit (ADR-0187
// §SD3): it can never be prelude-bound, so what pins it is its own
// `-- play: expr` line. Two mirrors, one predicate — a caller asking "is this
// name pinned" must not have to know which kind of slot it is holding.
func (inst *PlayApp) paramPinned(name string) bool {
	if _, pinned := inst.paramSyncedValues[name]; pinned {
		return true
	}
	_, declared := inst.paramSyncedExprs[name]
	return declared
}

// signalRawFor reads a name's stored raw through this frame's snapshot,
// falling back to the store itself outside a frame (the debounced parse can
// run before Render has taken one).
func (inst *PlayApp) signalRawFor(name string) (raw string, held bool) {
	sig := inst.frameSig
	if sig == nil {
		if inst.graph == nil {
			return
		}
		sig = inst.graph.signals()
	}
	p, ok := sig.Get(name)
	return p.Raw, ok
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

	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("PARAMETERS") {
			rt.Small().Weak()
		}
		inst.renderParamResetControl()
	}

	// Phase 1b: idle live drafts follow the store before any widget reads
	// them, so a panel's publication is what the pane draws this frame.
	inst.syncLiveParamDrafts()

	// The buffer's declared option lists, handed to whichever widgets read them
	// before dispatch — the orchestrator holds what a widget needs from outside
	// its own slots, the way it does the range evaluator (ADR-0124 Update
	// 2026-08-14). Re-scanned per frame for the same reason the ungroup hint is:
	// a marker is part of the buffer, and the buffer is being edited.
	enums := scanEnumHints(inst.sql)
	exprs := scanExprHints(inst.sql)
	for _, w := range inst.paramWidgets {
		if aware, ok := w.(enumHintAwareI); ok {
			aware.SetEnumHints(enums)
		}
		if aware, ok := w.(exprHintAwareI); ok {
			aware.SetExprHints(exprs)
			aware.SetExprMarks(inst.exprMarks)
		}
	}

	consumed := make([]bool, len(slots))
	// grouped tracks the slots a group widget folded, which is what §SD7's
	// near-miss pass reports on. It cannot read `consumed` instead: the tail
	// scalarTextWidget claims every remaining slot, so by the end of dispatch
	// nothing is unconsumed and the interesting set would be empty.
	grouped := make([]bool, len(slots))
	ungroup := scanUngroupHint(inst.sql)
	// A half-pinned pair declines the fold (ADR-0124's 2026-07-22 amendment):
	// its halves are withheld from the group widgets so the tail claims them
	// as two scalars, and the near-miss line says why. Withholding rather
	// than vetoing after the fact keeps paramWidgetI untouched — the matcher
	// never learns what a tier is — and leaves any OTHER pair in the same
	// buffer free to fold.
	halfPinned, mixed := inst.mixedTierRangeHalves(slots)
	unfilled := inst.unfilledSet()
	for _, w := range inst.paramWidgets {
		if ungroup && w.IsGroup() {
			continue
		}
		// Group widgets see the withheld halves as already taken; the tail
		// sees the true consumed set, so nothing is lost.
		withheld := halfPinned
		if !w.IsGroup() {
			withheld = nil
		}
		mask := maskUnion(consumed, withheld)
		remaining := unconsumedSlots(slots, mask)
		if len(remaining) == 0 {
			continue
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
				abs := absoluteIndex(slots, mask, ri)
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
			// The tier control sits beside the claim it migrates, drawn by
			// the orchestrator for the renderFoldLabel reason: a widget
			// deciding its own tier would have to know about the buffer's
			// prelude, which is exactly what §SD4 keeps away from it.
			//
			// The row is always framed, outlined only when the caret is in one
			// of its placeholders (ADR-0130 L3's caret report). Always framed,
			// so the widget tree keeps one shape — a frame that came and went
			// would move every inner widget's id with it, and with it the
			// editor state egui holds per id.
			row := c.Frame(inst.ids.PrepareStr("paramClaim:" + subset[0].Name)).
				Fill(color.Transparent).InnerMargin(styletokens.PaddingHair(styletokens.ActiveDensity()))
			if inst.caretOnClaim(subset) {
				// Hairline: the outline marks the row without competing with the
				// text inside it, which the paragraph on styleCaretRowMark records.
				row = row.Stroke(styletokens.StrokeHair, styleCaretRowMark)
			}
			for range row.KeepIter() {
				for range c.Horizontal().KeepIter() {
					inst.renderClaimTierControl(subset)
					inst.renderClaimUnfilledMark(subset, unfilled)
					w.Render(&paramCtx{
						Ids:    inst.ids,
						Slots:  subset,
						Drafts: inst.paramDrafts,
					})
				}
			}
			mask = maskUnion(consumed, withheld)
			remaining = unconsumedSlots(slots, mask)
			if len(remaining) == 0 {
				break
			}
		}
	}

	inst.renderNearMissNote(slots, grouped, ungroup, mixed,
		orphanEnumHints(enums, slots), orphanExprHints(exprs, slots))

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

// renderClaimTierControl draws the pin/unpin gesture for one claim (ADR-0124's
// 2026-07-22 §SD4 amendment). It operates on the CLAIM, not the slot, so a
// folded pair migrates as a unit and the pane cannot produce a mixed-tier
// range: both halves author their SET in one buffer rewrite, and both live
// values are seeded in one frame, which the frame-snapshot rule (ADR-0097 5a)
// then makes atomic for every consumer.
func (inst *PlayApp) renderClaimTierControl(subset []paramSlot) {
	if len(subset) == 0 {
		return
	}
	pinned := inst.paramPinned(subset[0].Name)
	label := "pin"
	if pinned {
		label = "unpin"
	}
	if c.Button(inst.ids.PrepareStr("paramTier-"+subset[0].Name),
		c.Atoms().Text(label).Keep()).
		Small().Selected(pinned).
		SendResp().HasPrimaryClicked() {
		if pinned {
			inst.unpinParamClaim(subset)
		} else {
			inst.pinParamClaim(subset)
		}
	}
}

// pinParamClaim moves a claim's names to the pinned tier: it authors
// `SET param_<name> = <value>` for each of them through the same prelude path
// a pinned drift takes, so the encoding rules (encodeParamLiteral) are the
// same ones the buffer already lives by. The value is the store's, which is
// what the widget is showing; a name the store never held pins whatever its
// draft holds.
//
// The store keeps its value — a SET shadows it at execution (slice-5 D1)
// rather than replacing it, so pinning `tl_min` does not wipe the extent the
// Timeline publishes, and unpinning finds it still there.
func (inst *PlayApp) pinParamClaim(subset []paramSlot) {
	if exprCategoryFor(subset[0].Type).spliced() {
		inst.pinExprClaim(subset)
		return
	}
	values := make(map[string]string, len(inst.paramSyncedValues)+len(subset))
	maps.Copy(values, inst.paramSyncedValues)
	for _, s := range subset {
		v, held := inst.signalRawFor(s.Name)
		if !held {
			if ptr, has := inst.paramDrafts[s.Name]; has {
				v = *ptr
			}
		}
		values[s.Name] = v
	}
	out, changed := SyncParamPrelude(inst.sql, inst.paramSlots, values)
	if !changed {
		// A transiently unparseable buffer: leave both tiers alone rather
		// than record a migration the buffer does not show.
		return
	}
	inst.sql = out
	for _, s := range subset {
		// The tier bit flips now, not when the debounced parse catches up:
		// the frame in between must not read as live drift and write the
		// value straight back into the store it just left.
		inst.paramSyncedValues[s.Name] = values[s.Name]
		delete(inst.paramLiveSeeded, s.Name)
		if ptr, has := inst.paramDrafts[s.Name]; has {
			*ptr = values[s.Name]
		}
	}
}

// unpinParamClaim moves a claim's names to the live tier: it removes their
// SET lines and seeds the store with the values those lines carried, so the
// value the user was looking at survives the migration and the name is
// immediately live rather than unfilled.
//
// The drift baseline moves with the tier, which is the invariant the frame
// after an unpin depends on: the pane must neither re-author the SET it just
// removed (the name is no longer pinned) nor tear the draft (the store now
// agrees with it).
func (inst *PlayApp) unpinParamClaim(subset []paramSlot) {
	if exprCategoryFor(subset[0].Type).spliced() {
		inst.unpinExprClaim(subset)
		return
	}
	values := make(map[string]string, len(inst.paramSyncedValues))
	maps.Copy(values, inst.paramSyncedValues)
	freed := make(map[string]string, len(subset))
	for _, s := range subset {
		v, bound := inst.paramSyncedValues[s.Name]
		if !bound {
			continue
		}
		freed[s.Name] = v
		delete(values, s.Name)
	}
	if len(freed) == 0 {
		return
	}
	out, changed := SyncParamPrelude(inst.sql, inst.paramSlots, values)
	if !changed {
		return
	}
	inst.sql = out
	for _, s := range subset {
		v, ok := freed[s.Name]
		if !ok {
			continue
		}
		delete(inst.paramSyncedValues, s.Name)
		inst.noteLiveSeeded(s.Name, v)
		if inst.graph != nil {
			inst.graph.setSignalRawFrom(s.Name, v, signalWriterParamWidget)
		}
		if ptr, has := inst.paramDrafts[s.Name]; has {
			*ptr = v
		}
	}
}

// renderClaimUnfilledMark marks a claim whose name the buffer references and
// nothing fills — D3's empty-state, now typed and in the pane. The pane is the
// fill affordance the Run-block hint points at, so the mark and the hint
// retire together: both are derived per frame from unfilledInputs, so typing a
// value clears them with no state to reset.
func (inst *PlayApp) renderClaimUnfilledMark(subset []paramSlot, unfilled map[string]bool) {
	if len(unfilled) == 0 {
		return
	}
	for _, s := range subset {
		if !unfilled[s.Name] {
			continue
		}
		warn := color.Hex(styletokens.WarningDefault.AsHex())
		atoms := c.Atoms().BeginRichTextColored(warn, color.Transparent, "needs a value").Small().End().Keep()
		c.LabelAtoms(atoms).Send()
		return // one mark per claim: a folded pair is one control
	}
}

// caretOnClaim reports whether the editor caret sits in one of this claim's
// placeholders, so the pane can mark the row the user is standing in.
//
// Gated on quiescence: the slots' `Src` describe the buffer the debounced
// parse saw, so mid-edit they are stale and a mark would point at the wrong
// row. The caret value itself is one frame behind here — renderParamSlots
// draws above the editor and so runs before renderSqlEditor resolves it —
// which is invisible for a highlight and keeps the mode-aware resolution
// (plain buffer vs. residual mirror) in the one place that knows the mode.
//
// The end bound is inclusive: a caret resting just past the closing `}` is
// still "on" the placeholder, which is where it lands after typing one.
//
// Known limit: collectParamSlots dedups by name and keeps the FIRST
// occurrence's Src, so a name written twice only marks from its first
// occurrence. It degrades to no mark, never to marking the wrong row.
func (inst *PlayApp) caretOnClaim(subset []paramSlot) bool {
	if inst.sql == "" || inst.sql != inst.formattedFor {
		return false
	}
	for _, s := range subset {
		if s.Src.Empty() || s.Src.End > len(inst.sql) {
			continue
		}
		if inst.caretByte >= s.Src.Start && inst.caretByte <= s.Src.End {
			return true
		}
	}
	return false
}

// unfilledSet is unfilledInputs as a lookup, computed once per frame — the
// per-claim mark would otherwise re-derive it per claim.
func (inst *PlayApp) unfilledSet() (out map[string]bool) {
	names := inst.unfilledInputs()
	if len(names) == 0 {
		return
	}
	out = make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return
}

// mixedTierPair is a range pair §SD5 would fold but whose halves sit in
// different tiers, with the pinned half named first.
type mixedTierPair struct {
	Pinned string
	Live   string
}

// mixedTierRangeHalves finds the range pairs whose halves disagree on tier —
// a hand-authored buffer that SET-binds only one half of a range. Such a pair
// declines the fold: one picker writing two tiers would send half a range to
// the buffer and half to the store, and the control has no way to say so.
//
// Returns a mask of the slots to withhold from the group widgets, and the
// pairs themselves for §SD7's near-miss line. Only pairs that would otherwise
// fold are reported: halves that disagree on TYPE are already the near-miss
// line's own case, and saying both would be two answers to one question.
func (inst *PlayApp) mixedTierRangeHalves(slots []paramSlot) (withheld []bool, pairs []mixedTierPair) {
	for i, lo := range slots {
		stem, suffix, decomposed := splitRangeSuffix(lo.Name)
		if !decomposed {
			continue
		}
		hiSuffix, isLo := rangeHiFor(suffix)
		if !isLo {
			continue
		}
		j := findRangeHalf(slots, stem, hiSuffix)
		if j < 0 {
			continue
		}
		hi := slots[j]
		if !isDateTimeType(lo.Type) || !isDateTimeType(hi.Type) {
			continue // not foldable anyway — the type-mismatch case owns it
		}
		loPinned, hiPinned := inst.paramPinned(lo.Name), inst.paramPinned(hi.Name)
		if loPinned == hiPinned {
			continue
		}
		if withheld == nil {
			withheld = make([]bool, len(slots))
		}
		withheld[i] = true
		withheld[j] = true
		pair := mixedTierPair{Pinned: lo.Name, Live: hi.Name}
		if hiPinned {
			pair = mixedTierPair{Pinned: hi.Name, Live: lo.Name}
		}
		pairs = append(pairs, pair)
	}
	return
}

// mixedTierNote is §SD7's line for a half-pinned pair: it names both halves,
// their tiers, and the one gesture that resolves it. Pure over the pairs, so
// it is testable without a frame.
func mixedTierNote(pairs []mixedTierPair) string {
	if len(pairs) == 0 {
		return ""
	}
	p := pairs[0]
	return "{" + p.Pinned + "} is pinned by a SET and {" + p.Live +
		"} is live — a range picker needs both halves in one tier; pin or unpin the other half"
}

// renderNearMissNote draws §SD7's single advisory line about folds that did not
// happen. Advisory only: it never gates execution, and a query that ignores it
// behaves exactly as it did.
//
// One line, so the cases are ordered by how much they explain: the ungroup
// opt-out accounts for every missing fold at once; a half-pinned pair is next,
// being a specific decline with a one-click fix; then a marker naming a
// placeholder the buffer does not have — enum first, then expression — each a
// typo with a visible symptom and no other explanation; then the type mismatch and the generic vocabulary note, both inside
// nearMissNote.
func (inst *PlayApp) renderNearMissNote(slots []paramSlot, grouped []bool, ungroup bool, mixed []mixedTierPair, orphanEnums []string, orphanExprs []string) {
	unfolded := make([]paramSlot, 0, len(slots))
	for i, s := range slots {
		if !grouped[i] {
			unfolded = append(unfolded, s)
		}
	}
	note := ""
	if !ungroup {
		note = mixedTierNote(mixed)
	}
	if note == "" {
		note = orphanEnumNote(orphanEnums)
	}
	if note == "" {
		note = orphanExprNote(orphanExprs)
	}
	if note == "" {
		note = nearMissNote(unfolded, ungroup)
	}
	if note == "" {
		return
	}
	for rt := range c.RichTextLabel(note) {
		rt.Small().Weak()
	}
}

// maskUnion returns a fresh mask that is true where either input is. A nil
// second mask returns a copy of the first, so the tail widgets pay nothing for
// the group phase's withholding.
func maskUnion(a []bool, b []bool) (out []bool) {
	out = make([]bool, len(a))
	copy(out, a)
	if b == nil {
		return
	}
	for i := range out {
		if i < len(b) && b[i] {
			out[i] = true
		}
	}
	return
}

// syncParamDriftToPrelude compares each draft to its last-synced value and
// routes the drift BY TIER (ADR-0124's 2026-07-22 §SD4 amendment): a pinned
// name's drift rebuilds the editor's leading SET prelude exactly as before, a
// live name's drift is a store write stamped `param-widget`. Idempotent — it
// no-ops when no draft moved.
//
// The prelude rebuild is handed the pinned names only. That is the flipped
// fill default: typing into a widget whose name has no SET no longer authors
// one as a side effect, so filling a picker stops silently disconnecting the
// panel that co-writes the same name. A buffer whose prelude already binds
// every slot has no live names and behaves identically to before.
func (inst *PlayApp) syncParamDriftToPrelude() {
	if len(inst.paramSlots) == 0 {
		return
	}
	pinnedValues := make(map[string]string, len(inst.paramSlots))
	pinnedDrift := false
	exprValues := make(map[string]string, len(inst.paramSlots))
	exprDrift := false
	// The live tier's expressions, handed to the client each frame: they are
	// not in the buffer and they cannot ride the URL (§SD4), so the splice has
	// to be told them. Rebuilt whole rather than merged, so a name that left
	// the live tier stops being substituted.
	liveExprs := make(map[string]string, len(inst.paramSlots))
	for _, s := range inst.paramSlots {
		ptr, ok := inst.paramDrafts[s.Name]
		if !ok {
			continue
		}
		if exprCategoryFor(s.Type).spliced() {
			// The prelude is never this slot's (§SD2) — it would ship an
			// expression to the server as a string. Its two tiers are its own
			// directive and the signal store, forked here on the same bit
			// every other slot uses.
			if !inst.paramPinned(s.Name) {
				inst.syncLiveParamDrift(s.Name, *ptr)
				liveExprs[s.Name] = *ptr
				continue
			}
			// Collected and written once below, so a frame that moved two
			// fields rewrites the buffer once.
			exprValues[s.Name] = *ptr
			if inst.paramSyncedExprs[s.Name] != *ptr {
				exprDrift = true
			}
			continue
		}
		if !inst.paramPinned(s.Name) {
			inst.syncLiveParamDrift(s.Name, *ptr)
			continue
		}
		pinnedValues[s.Name] = *ptr
		if inst.paramSyncedValues[s.Name] != *ptr {
			pinnedDrift = true
		}
	}
	// The directive rewrite runs FIRST and the prelude rewrite second, because
	// SyncParamPrelude rebuilds the leading SET block as prelude + residual and
	// the directives live in that residual: doing it the other way round would
	// have the second writer re-derive a buffer the first had just moved.
	if inst.client != nil {
		inst.client.SetExprValues(liveExprs)
	}
	if exprDrift {
		if out, changed := syncExprDirectives(inst.sql, exprValues); changed {
			inst.sql = out
		}
		maps.Copy(inst.paramSyncedExprs, exprValues)
	}
	if !pinnedDrift {
		return
	}
	out, changed := SyncParamPrelude(inst.sql, inst.paramSlots, pinnedValues)
	if !changed {
		// Parse failure inside SyncParamPrelude — leave inst.sql alone
		// and refresh the cache so we stop re-trying every frame for
		// the same transient broken state.
		maps.Copy(inst.paramSyncedValues, pinnedValues)
		return
	}
	inst.sql = out
	maps.Copy(inst.paramSyncedValues, pinnedValues)
}

// computeExprMarks is §SD6's validation: splice the declared values into the
// buffer, parse the result, and map a syntax error inside a spliced value back
// onto the field it came from.
//
// Splice-then-parse rather than validating a fragment on its own. The buffer
// with its placeholders in place always parses — a placeholder is a grammar
// production — so the only thing worth asking about is the substituted text,
// and asking about it needs no fragment entry rule and no wrapper whose context
// the expression will not actually execute in.
//
// One mark, from the first error. A parse reports the first fault it cannot
// recover from; naming more would be guessing at cascades.
func computeExprMarks(sql string, values map[string]string) (marks map[string]nanopass.SourceRange) {
	if len(values) == 0 {
		return
	}
	spliced, spl, err := spliceExprSlots(sql, values)
	if err != nil || len(spl) == 0 {
		return
	}
	pos := firstSyntaxError(spliced)
	if !pos.Ok {
		return
	}
	name, mark, ok := exprMarkFor(spl, sqleditor.ByteOffsetOfLineCol(spliced, pos.Line, pos.Column))
	if !ok {
		// The fault is outside every spliced value, so it belongs to the query
		// and the editor's own error underline already has it (§SD6).
		return
	}
	return map[string]nanopass.SourceRange{name: mark}
}

// syncLiveParamDrafts makes idle live drafts follow the store, so a value a
// panel publishes — a Timeline extent, a viewport bound — shows up in the
// pane's own typed widget (ADR-0124's 2026-07-22 §SD4 amendment; the idiom is
// slice 5e's Signals editor).
//
// Two guards, both against paramLiveSeeded — the value the pane last agreed
// with the store on:
//
//   - the store must have MOVED since then, or there is nothing to follow and
//     a settled co-writer's identical re-emit costs no draft churn;
//   - the draft must NOT have moved since then, or the user has typed
//     something phase 3 has not committed yet. Typing wins: the pane's write
//     lands this frame and is the later writer, with its provenance on it.
//
// A name the store dropped (the Signals editor's discard) reads as an empty
// value here, so an idle draft empties with it rather than showing a value no
// longer bound to anything.
//
// Runs before dispatch, so the widgets render this frame's value; the
// databinding override tells the frontend to drop its cached buffer for a
// draft written behind an interactive widget's back (the setEndpoint idiom).
func (inst *PlayApp) syncLiveParamDrafts() {
	if inst.frameSig == nil && inst.graph == nil {
		return
	}
	for _, s := range inst.paramSlots {
		if inst.paramPinned(s.Name) {
			continue // the parser owns a pinned draft (phase 1)
		}
		ptr, ok := inst.paramDrafts[s.Name]
		if !ok {
			continue
		}
		raw, _ := inst.signalRawFor(s.Name)
		seeded := inst.paramLiveSeeded[s.Name]
		if raw == seeded || *ptr != seeded {
			continue
		}
		*ptr = raw
		inst.noteLiveSeeded(s.Name, raw)
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(ptr)
	}
}

// syncLiveParamDrift writes a live name's moved draft into the signal store —
// an ordinary provenance'd write, so liveness, the staleness witness, the
// auto-run gate and the history snapshot all compose with no new store
// semantics (ADR-0097's 2026-07-22 Update).
//
// The baseline is paramLiveSeeded — the value the pane last wrote or last
// took from the store — NOT the store's current value. Comparing against the
// store would make an external co-writer's move read as pane drift and get
// written straight back, which is the pane clobbering the Timeline rather
// than following it.
func (inst *PlayApp) syncLiveParamDrift(name string, draft string) {
	if inst.graph == nil {
		return
	}
	if inst.paramLiveSeeded[name] == draft {
		return
	}
	inst.graph.setSignalRawFrom(name, draft, signalWriterParamWidget)
	inst.noteLiveSeeded(name, draft)
}

// noteLiveSeeded records a live name's baseline. It tolerates the
// bare-constructed &PlayApp{} some unit tests use for unrelated work, where
// the constructor's maps are absent.
func (inst *PlayApp) noteLiveSeeded(name string, value string) {
	if inst.paramLiveSeeded == nil {
		inst.paramLiveSeeded = make(map[string]string, 4)
	}
	inst.paramLiveSeeded[name] = value
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
