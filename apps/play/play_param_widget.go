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

func (w *dateTimePairWidget) Matches(slots []paramSlot) (consumedIdx []int, ok bool) {
	for i := 0; i+1 < len(slots); i++ {
		a, b := slots[i], slots[i+1]
		if !strings.EqualFold(a.Name, "from") || !strings.EqualFold(b.Name, "to") {
			continue
		}
		if !isDateTimeType(a.Type) || !isDateTimeType(b.Type) {
			continue
		}
		consumedIdx = []int{i, i + 1}
		ok = true
		return
	}
	return
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
