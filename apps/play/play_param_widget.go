package play

import (
	"context"
	"math"
	"strings"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// paramWidgetI authors widget UI for one or more unbound `{name:Type}`
// placeholders that the debounced parse extracted from the editor
// buffer. Widgets read and write the *draft* string for each slot they
// own; the orchestrator (renderParamSlots) mirrors drafts back into
// the editor's leading `SET param_*` prelude via SyncParamPrelude.
//
// Widgets are stateful — DateTime widgets cache the packed uint64
// across frames so the in-flight one-frame-lag SendRespVal binding
// survives debounce-driven draft refreshes. The cache lives on the
// widget struct, keyed by slot name; clearStateForAbsent prunes
// entries whose slot has disappeared.
type paramWidgetI interface {
	// Matches scans slots in editor order and returns the indices it
	// consumes when it can handle them. Returns nil, false to pass.
	Matches(slots []paramSlot) (consumedIdx []int, ok bool)

	// Render draws the widget UI for ctx.Slots inside the current ui
	// scope; reads and writes the per-slot draft string via the
	// pointers in ctx.Drafts.
	Render(ctx *paramCtx)

	// ClearStateForAbsent drops any cached per-slot state whose name
	// is not in the supplied set. Invoked once per frame after dispatch
	// so widgets don't accumulate entries for deleted placeholders.
	ClearStateForAbsent(present map[string]struct{})

	// IsGroup reports whether this widget consumes multiple slots as
	// a logical bundle (e.g. from/to → range picker). The `-- play:
	// ungroup` comment-line opt-out skips group widgets so users who
	// want one independent control per slot can force the fallback.
	IsGroup() bool
}

// paramCtx is what each Render receives.
type paramCtx struct {
	Ids    *c.WidgetIdStack
	Slots  []paramSlot
	Drafts map[string]*string
}

// timeRangeEvaluatorI is the minimal shape the range widget needs
// from a Phase-4 evaluator; matches evaluator.Evaluator.Eval. Carved
// as an interface so the widget can be unit-tested with a stub and
// so play_param_widget.go does not have to import the evaluator
// package directly.
type timeRangeEvaluatorI interface {
	Eval(ctx context.Context, anchor time.Time, tzID uint16, fromExpr, toExpr string) (fromMs, toMs int64, err error)
}

// evaluatorAwareI is an opt-in sub-interface for widgets that need
// a timerange evaluator (or any future bus-backed resolver).
// PlayApp.SetCapabilities fans the constructed evaluator out to
// every registered paramWidgetI that implements this — widgets that
// don't (scalarTextWidget, dateTimePairWidget) ignore the wiring.
// Late-binding via setter rather than constructor argument so
// NewPlayApp can register the widget set before the host hands over
// its bus.
type evaluatorAwareI interface {
	SetTimeRangeEvaluator(timeRangeEvaluatorI)
}

// chDateTimeLayout is the ClickHouse canonical DateTime literal form
// without timezone (UTC implied). Matches what `toDateTime('YYYY-MM-DD
// HH:MM:SS')` and the HTTP param channel accept verbatim.
const chDateTimeLayout = "2006-01-02 15:04:05"

// parseChDateTime decodes a draft string into a UTC instant.
// Accepts the canonical CH forms (`YYYY-MM-DD HH:MM:SS` and the
// millis-bearing `YYYY-MM-DD HH:MM:SS.mmm` that the range widget
// emits), plus RFC3339 — covers both pickers' round-trip output.
// Returns ok=false on any unrecognised form so the caller falls
// back to a default seed.
func parseChDateTime(s string) (t time.Time, ok bool) {
	s = strings.Trim(strings.TrimSpace(s), "'")
	if s == "" {
		return
	}
	layouts := []string{
		chDateTimeLayout,
		"2006-01-02 15:04:05.999", // millis-bearing CH literal (range widget output)
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			t = parsed.UTC()
			ok = true
			return
		}
	}
	return
}

// formatChDateTime returns the canonical CH literal text for t (UTC).
func formatChDateTime(t time.Time) string {
	return t.UTC().Format(chDateTimeLayout)
}

// dateTimePairWidget renders two DateTimePickerButtons side by side
// when the parser found an adjacent (from, to) pair of DateTime-typed
// placeholders. The `-- play: ungroup` comment-line opt-out is
// honoured at the dispatcher level (see scanUngroupHint).
//
// State per slot lives on a dateTimePairSlotState — packed uint64
// (stable pointer for SendRespVal), the last draft value we saw
// (for change detection), the last packed value we rendered with
// (for click detection), parseOK (whether the current draft was
// last seen as parseable; mirror-writeback is gated on it so an
// unparseable user typo survives the round-trip), and seeded
// (whether updatePackedFromDraft has run at least once).
type dateTimePairWidget struct {
	state map[string]*dateTimePairSlotState
}

type dateTimePairSlotState struct {
	packed           uint64
	lastDraft        string
	lastRenderPacked uint64
	parseOK          bool
	seeded           bool
}

func newDateTimePairWidget() *dateTimePairWidget {
	return &dateTimePairWidget{state: map[string]*dateTimePairSlotState{}}
}

// rangeSuffixes is the closed vocabulary of range-bound names (ADR-0124 §SD5),
// each entry the (lo, hi) halves of one range idiom. `first`/`last` is excluded:
// it names an ordering, not a bound. `t0`/`t1` is excluded: a digit suffix needs
// a different decomposition and earns little.
var rangeSuffixes = [...]struct{ lo, hi string }{
	{"from", "to"},
	{"min", "max"},
	{"start", "end"},
	{"lo", "hi"},
	{"since", "until"},
}

// splitRangeSuffix decomposes a slot name into a stem and one of the
// rangeSuffixes halves: the name either equals a half (empty stem) or ends with
// `_` followed by one. Both returns are lower-cased so callers can compare
// stems directly. ok=false means the name carries no range suffix.
//
// A name that ends with two halves resolves against the first table entry that
// matches (`a_min_max` is stem `a_min`, suffix `max`) — deterministic, and the
// alternative readings are not ones a caller could act on.
func splitRangeSuffix(name string) (stem, suffix string, ok bool) {
	lower := strings.ToLower(name)
	for _, e := range rangeSuffixes {
		for _, half := range [2]string{e.lo, e.hi} {
			if lower == half {
				return "", half, true
			}
			if strings.HasSuffix(lower, "_"+half) {
				return lower[:len(lower)-len(half)-1], half, true
			}
		}
	}
	return
}

// rangeHiFor returns the hi half of the rangeSuffixes entry whose lo half is
// suffix. ok=false means suffix is not a lo half — it is a hi half, or not a
// range suffix at all.
func rangeHiFor(suffix string) (hi string, ok bool) {
	for _, e := range rangeSuffixes {
		if e.lo == suffix {
			return e.hi, true
		}
	}
	return
}

// findRangeHalf returns the index of the slot whose name decomposes to
// (stem, suffix), or -1. collectParamSlots dedupes by name, so at most one
// slot can match.
func findRangeHalf(slots []paramSlot, stem, suffix string) int {
	for i, s := range slots {
		st, sf, ok := splitRangeSuffix(s.Name)
		if ok && st == stem && sf == suffix {
			return i
		}
	}
	return -1
}

// matchRangePair scans slots for the first foldable range (ADR-0124 §SD5): two
// slots whose names share a stem, whose suffixes are the two halves of one
// rangeSuffixes entry, and whose types are both DateTime after Nullable unwrap.
//
// Position is not consulted — dedup-by-name means a stem admits at most one
// pair per table entry, so there is nothing for adjacency to disambiguate. The
// scan is anchored on the lo half, so "first" is the first lo in editor order
// and the result is deterministic.
//
// The returned indices are lo-then-hi regardless of editor order, which is the
// order both widgets' Render assumes; a `{to:…}`-before-`{from:…}` query
// therefore still draws its bounds the right way round.
//
// Shared by the pair and range widgets (the range widget additionally gates on
// having an evaluator wired) so the two stay in lockstep on what counts as a
// foldable range.
func matchRangePair(slots []paramSlot) (consumedIdx []int, ok bool) {
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
		if !isDateTimeType(lo.Type) || !isDateTimeType(slots[j].Type) {
			continue
		}
		return []int{i, j}, true
	}
	return
}

func (w *dateTimePairWidget) Matches(slots []paramSlot) (consumedIdx []int, ok bool) {
	return matchRangePair(slots)
}

// findTypeMismatchedPair finds the first stem whose two range halves are both
// present but whose types keep them from folding, with at least one half
// DateTime. Two halves that agree on some other type are somebody's string or
// numeric bounds, not a near-miss, so they are left alone — a picker was never
// plausible there and saying so would be noise.
func findTypeMismatchedPair(slots []paramSlot) (lo, hi paramSlot, found bool) {
	for _, s := range slots {
		stem, suffix, ok := splitRangeSuffix(s.Name)
		if !ok {
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
		a, b := s, slots[j]
		if isDateTimeType(a.Type) == isDateTimeType(b.Type) {
			// Both DateTime: matchRangePair already folded them. Neither:
			// they agree on something else and want no picker.
			continue
		}
		return a, b, true
	}
	return
}

// nearMissNote is the one line the pane says about folds that did not happen
// (ADR-0124 §SD7), or "" when there is nothing worth saying. Pure over the slot
// list, so it is testable without a frame.
//
// unfolded is the slots no group widget claimed. Cases are ordered by how much
// they explain: the ungroup opt-out accounts for every missing fold at once, so
// it wins; a stem whose halves disagree on type is next, being the one case
// where intent is unambiguous; the generic note is the fallback.
func nearMissNote(unfolded []paramSlot, ungroup bool) string {
	if ungroup {
		if _, would := matchRangePair(unfolded); would {
			return `"-- play: ungroup" is in effect — a range pair below is split into plain fields`
		}
		return ""
	}
	if lo, hi, found := findTypeMismatchedPair(unfolded); found {
		return "{" + lo.Name + " : " + lo.Type + "} and {" + hi.Name + " : " + hi.Type +
			"} — a range picker needs both DateTime"
	}
	names := make([]string, 0, len(unfolded))
	for _, s := range unfolded {
		if isDateTimeType(s.Type) {
			names = append(names, s.Name)
		}
	}
	if len(names) < 2 {
		return ""
	}
	return strings.Join(names, ", ") +
		" are DateTime but do not pair — a range picker needs one stem and two bounds" +
		" (from/to, min/max, start/end, lo/hi, since/until — bare or as x_from/x_to)"
}

func (w *dateTimePairWidget) Render(ctx *paramCtx) {
	for range c.Horizontal().KeepIter() {
		for _, s := range ctx.Slots {
			draft, has := ctx.Drafts[s.Name]
			if !has {
				continue
			}
			w.renderOne(ctx.Ids, s, draft)
		}
	}
}

func (w *dateTimePairWidget) renderOne(ids *c.WidgetIdStack, slot paramSlot, draft *string) {
	st, ok := w.state[slot.Name]
	if !ok {
		st = &dateTimePairSlotState{}
		w.state[slot.Name] = st
	}
	// Picker click detected via packed delta since last render:
	// SendRespVal apply runs at end-of-frame, so a click at frame N
	// shows as `packed != lastRenderPacked` at the start of frame
	// N+1. Picker output is always parseable so parseOK flips back
	// on after a click.
	if st.seeded && st.packed != st.lastRenderPacked {
		st.parseOK = true
	}
	// External draft mutation (debounce-driven refresh from a
	// hand-typed SET prelude). updatePackedFromDraft owns the
	// four-way dispatch (successful parse / fresh-state seed-to-now
	// / unparseable-with-stale-packed / no-change).
	updatePackedFromDraft(st, *draft, time.Now())

	for range c.Vertical().KeepIter() {
		for rt := range c.RichTextLabel(slot.Name + " : " + slot.Type) {
			rt.Small().Weak()
		}
		c.DateTimePickerButton(ids.PrepareStr("paramSlotDt-"+slot.Name), st.packed).
			SendRespVal(&st.packed)
	}

	// Capture packed-before-apply so next frame's click detection
	// sees the delta the end-of-frame Sync introduces.
	st.lastRenderPacked = st.packed

	// Mirror packed → draft only when we trust packed reflects user
	// intent. parseOK=false means the user typed something
	// unparseable; leave the draft alone so the typo survives until
	// they fix it or click the picker.
	if st.parseOK {
		formatted := formatChDateTime(c.UnpackDateTimeUtc(st.packed))
		if formatted != *draft {
			*draft = formatted
		}
	}
	st.lastDraft = *draft
}

// updatePackedFromDraft is the pure state transition that runs
// every renderOne: detect external draft changes, parse, update
// packed and parseOK accordingly. now is a parameter so tests can
// inject a fixed clock; renderOne passes time.Now().
//
// Transitions:
//   - Unseeded → run the parse/seed dispatch unconditionally.
//   - draft == lastDraft → no change since last frame, no-op.
//   - parse(draft) succeeds → packed = pack(parsed), parseOK = true.
//   - parse fails, packed == 0 (fresh slot, no value yet) → packed
//     = pack(now), parseOK = true. Picker still has something to
//     show; the next frame's mirror writes it back to draft.
//   - parse fails, packed != 0 (user typed garbage on top of a
//     valid value) → packed unchanged, parseOK = false. The
//     mirror skip preserves the typo.
func updatePackedFromDraft(st *dateTimePairSlotState, draft string, now time.Time) {
	if st.seeded && draft == st.lastDraft {
		return
	}
	if t, parsed := parseChDateTime(draft); parsed {
		st.packed = c.PackDateTimeUtc(t)
		st.parseOK = true
	} else if st.packed == 0 {
		st.packed = c.PackDateTimeUtc(now)
		st.parseOK = true
	} else {
		st.parseOK = false
	}
	st.seeded = true
}

func (w *dateTimePairWidget) ClearStateForAbsent(present map[string]struct{}) {
	for k := range w.state {
		if _, keep := present[k]; !keep {
			delete(w.state, k)
		}
	}
}

func (w *dateTimePairWidget) IsGroup() bool { return true }

// scalarTextWidget is the fallback: one TextEdit per slot, type label
// as the hint. Handles every unmatched slot, so it must register last.
type scalarTextWidget struct{}

func newScalarTextWidget() *scalarTextWidget { return &scalarTextWidget{} }

func (w *scalarTextWidget) Matches(slots []paramSlot) (consumedIdx []int, ok bool) {
	if len(slots) == 0 {
		return
	}
	consumedIdx = []int{0}
	ok = true
	return
}

func (w *scalarTextWidget) Render(ctx *paramCtx) {
	for _, s := range ctx.Slots {
		draft, has := ctx.Drafts[s.Name]
		if !has {
			continue
		}
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(s.Name + " : " + s.Type) {
				rt.Small().Weak()
			}
			c.TextEdit(ctx.Ids.PrepareStr("paramSlotScalar-"+s.Name), *draft, false).
				DesiredWidth(float32(math.Inf(1))).
				HintText("value for {" + s.Name + " : " + s.Type + "}").
				SendRespVal(draft)
		}
	}
}

func (w *scalarTextWidget) ClearStateForAbsent(map[string]struct{}) {}

func (w *scalarTextWidget) IsGroup() bool { return false }

// scanUngroupHint reports whether sql contains the magic single-line
// comment `-- play: ungroup` (case-insensitive on the keyword,
// whitespace-tolerant). Two adjacent from/to DateTime slots fold into
// one pair widget unless this comment opts out — for users who really
// want two independent pickers.
func scanUngroupHint(sql string) bool {
	for line := range strings.SplitSeq(sql, "\n") {
		ln := strings.TrimSpace(line)
		if !strings.HasPrefix(ln, "--") {
			continue
		}
		ln = strings.TrimSpace(strings.TrimPrefix(ln, "--"))
		if strings.EqualFold(ln, "play: ungroup") {
			return true
		}
	}
	return false
}
