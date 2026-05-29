//go:build llm_generated_opus47

package play

import (
	"context"
	"strconv"
	"strings"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/presets"
)

// dateTimeRangeWidget is the full Grafana 7.5-style picker —
// from/to expression fields, Apply, presets, IANA tz dropdown —
// backed by the ADR-0016 Phase-4 evaluator routed through ADR-0028's
// chlocalbroker pool. Matches returns ok=false until the host wires
// an evaluator via SetTimeRangeEvaluator (PlayApp.SetCapabilities
// does this); the simpler dateTimePairWidget (registered after this
// in NewPlayApp) claims the from/to slots in CLI / test contexts.
//
// State is a single pair record — the widget folds at most one
// {from:DateTime, to:DateTime} pair per query (paramSlot dedup
// already prunes repeated names). Slot rename or removal triggers a
// state reset in ClearStateForAbsent / Render's identity check.
//
// Eval runs synchronously from Render on each packedRange change
// (Apply click, preset click, tz change). The warm chlocalbroker
// pool typically responds in <10 ms; the 5 s context budget is the
// hard ceiling and matches the demo's wiring.
type dateTimeRangeWidget struct {
	eval    timeRangeEvaluatorI
	presets *presets.Registry
	state   *dateTimeRangeState
}

// dateTimeRangeState carries everything one from/to pair needs
// across frames: the wire payload the picker writes back, the
// unpacked expressions, the active IANA timezone, the last
// resolved millis pair, and any evaluator error to surface.
//
// lastEvalSet distinguishes "evaluator hasn't run yet" from
// "evaluator returned epoch-zero" (which is a real instant —
// 1970-01-01 UTC — and would alias to the EvaluatedRange struct
// zero if we only looked at FromEpochMS / ToEpochMS).
//
// lastMirroredFrom / To track the literal draft values the widget
// last wrote. An external draft mutation (debounce-driven refresh
// from a hand-typed SET prelude) breaks equality and triggers
// resyncFromDrafts.
//
// idGeneration bumps on each external resync; the picker's Rust
// side treats fromInitial / toInitial as one-shot seeds (see
// time_range_picker.rs:9-13), so a new widget id forces it to
// re-seed from the new initial values. Between syncs the id is
// stable, so user interactions (calendar pops, text edits) survive.
type dateTimeRangeState struct {
	fromName         string
	toName           string
	packedRange      string
	fromExpr         string
	toExpr           string
	tzName           string
	tzID             uint16
	lastEval         timerangepicker.EvaluatedRange
	lastEvalSet      bool
	lastEvalErr      error
	lastMirroredFrom string
	lastMirroredTo   string
	idGeneration     uint32
}

func newDateTimeRangeWidget() *dateTimeRangeWidget {
	return &dateTimeRangeWidget{
		presets: presets.DefaultGrafana75(),
	}
}

// Compile-time assertion that the widget satisfies the optional
// evaluator-injection sub-interface used by PlayApp.SetCapabilities.
var _ evaluatorAwareI = (*dateTimeRangeWidget)(nil)

func (w *dateTimeRangeWidget) SetTimeRangeEvaluator(ev timeRangeEvaluatorI) {
	w.eval = ev
}

func (w *dateTimeRangeWidget) IsGroup() bool { return true }

func (w *dateTimeRangeWidget) Matches(slots []paramSlot) (consumedIdx []int, ok bool) {
	if w.eval == nil {
		return
	}
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

func (w *dateTimeRangeWidget) Render(ctx *paramCtx) {
	if len(ctx.Slots) != 2 || w.eval == nil {
		return
	}
	fromSlot, toSlot := ctx.Slots[0], ctx.Slots[1]

	// Pair identity is (fromName, toName) — anything else means the
	// parser produced a different placeholder set and the state is
	// stale. Drop and reseed from drafts so prelude-driven values
	// (user-typed SET lines) survive a re-attach.
	if w.state == nil || w.state.fromName != fromSlot.Name || w.state.toName != toSlot.Name {
		w.state = newDateTimeRangeState(fromSlot.Name, toSlot.Name, ctx.Drafts)
	}

	// Bi-directional sync direction (a): SQL → widget. Detect when
	// drafts moved out from under us (user-typed SET prelude, or
	// host-driven prelude refresh) and re-seed the picker.
	w.resyncFromDrafts(ctx.Drafts)

	pickerID := "paramSlotRange-" + fromSlot.Name + "-" + strconv.FormatUint(uint64(w.state.idGeneration), 10)
	fluid := c.TimeRangePicker(
		ctx.Ids.PrepareStr(pickerID),
		w.state.fromExpr, w.state.toExpr).
		Tz(w.state.tzName)
	for _, p := range w.presets.All() {
		fluid = fluid.AddPreset(p.Label(), p.FromSQL(), p.ToSQL())
	}
	fluid.SendRespVal(&w.state.packedRange)

	// Bi-directional sync direction (b): widget → SQL. Apply /
	// preset click sets packedRange; the empty-payload skip avoids
	// running Eval before the user has interacted with the picker
	// even once.
	tzWire, from, to := timerangepicker.UnpackRange(w.state.packedRange)
	rangeChanged := w.state.packedRange != "" &&
		(from != w.state.fromExpr || to != w.state.toExpr || tzWire != w.state.tzName)
	if rangeChanged {
		w.state.fromExpr = from
		w.state.toExpr = to
		if tzWire != "" {
			if id, lerr := timerangepicker.LookupTz(tzWire); lerr == nil {
				w.state.tzID = id
				w.state.tzName = tzWire
			}
		}
		w.runEval()
	}

	// Mirror resolved bounds into drafts so SyncParamPrelude picks
	// them up on the post-render drift check. Skipped on error so
	// the previously-good SET line isn't overwritten with garbage.
	// lastMirroredFrom / To are kept in lock-step so the next
	// frame's resyncFromDrafts doesn't false-positive on our own
	// write.
	if w.state.lastEvalSet && w.state.lastEvalErr == nil {
		fromStr := formatChDateTimeMilli(w.state.lastEval.AsFromTime())
		toStr := formatChDateTimeMilli(w.state.lastEval.AsToTime())
		writeDraft(ctx.Drafts, fromSlot.Name, fromStr)
		writeDraft(ctx.Drafts, toSlot.Name, toStr)
		w.state.lastMirroredFrom = fromStr
		w.state.lastMirroredTo = toStr
	}

	w.renderStatusLine()
}

func (w *dateTimeRangeWidget) ClearStateForAbsent(present map[string]struct{}) {
	if w.state == nil {
		return
	}
	_, hasFrom := present[w.state.fromName]
	_, hasTo := present[w.state.toName]
	if !hasFrom || !hasTo {
		w.state = nil
	}
}

// runEval is the synchronous Eval call. Errors land on
// state.lastEvalErr so the status line can render them; on success,
// lastEval carries the resolved millis bundle and lastEvalSet flips.
func (w *dateTimeRangeWidget) runEval() {
	evalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fromMs, toMs, err := w.eval.Eval(evalCtx, time.Now(), w.state.tzID, w.state.fromExpr, w.state.toExpr)
	if err != nil {
		w.state.lastEvalErr = err
		w.state.lastEval = timerangepicker.EvaluatedRange{}
		w.state.lastEvalSet = false
		return
	}
	w.state.lastEval = timerangepicker.EvaluatedRange{
		FromEpochMS: fromMs,
		ToEpochMS:   toMs,
		TzID:        w.state.tzID,
	}
	w.state.lastEvalSet = true
	w.state.lastEvalErr = nil
}

// renderStatusLine surfaces either the eval error, the resolved
// human-readable bounds (RFC3339), or the literal draft values if
// the picker hasn't run an Eval yet but the SQL prelude carries
// values. Empty only when nothing meaningful is known.
func (w *dateTimeRangeWidget) renderStatusLine() {
	switch {
	case w.state.lastEvalErr != nil:
		for rt := range c.RichTextLabel("eval: " + w.state.lastEvalErr.Error()) {
			rt.Small().Weak()
		}
	case w.state.lastEvalSet:
		from := w.state.lastEval.AsFromTime().Format(time.RFC3339)
		to := w.state.lastEval.AsToTime().Format(time.RFC3339)
		for rt := range c.RichTextLabel("from: " + from + "   to: " + to) {
			rt.Small().Weak()
		}
	case w.state.lastMirroredFrom != "" || w.state.lastMirroredTo != "":
		// SQL-sourced bounds (external SET prelude) — show them as-is.
		// Click Apply to re-resolve relative to the current anchor.
		for rt := range c.RichTextLabel("from: " + w.state.lastMirroredFrom + "   to: " + w.state.lastMirroredTo) {
			rt.Small().Weak()
		}
	}
}

// newDateTimeRangeState seeds a fresh pair state. If the drafts
// already carry prelude values (user-typed SET lines or a previous
// widget instance), they win over the default `anchor_now`
// expressions — keeps the picker showing what the buffer says.
// lastMirroredFrom / To are seeded from the same drafts so the very
// next render doesn't fire resyncFromDrafts on the same values.
func newDateTimeRangeState(fromName, toName string, drafts map[string]*string) *dateTimeRangeState {
	st := &dateTimeRangeState{
		fromName: fromName,
		toName:   toName,
		tzID:     timerangepicker.TzIDUTC,
		tzName:   "UTC",
		fromExpr: "anchor_now - INTERVAL 1 HOUR",
		toExpr:   "anchor_now",
	}
	if d, ok := drafts[fromName]; ok && d != nil && *d != "" {
		st.fromExpr = quoteForCH(*d)
		st.lastMirroredFrom = *d
	}
	if d, ok := drafts[toName]; ok && d != nil && *d != "" {
		st.toExpr = quoteForCH(*d)
		st.lastMirroredTo = *d
	}
	return st
}

// resyncFromDrafts detects an external draft mutation (debounce
// refresh from a hand-typed SET prelude that the picker wasn't
// involved in) and re-seeds the picker's expression fields + bumps
// the id generation so Rust treats the next render as a fresh
// widget instance. Returns true on detected change.
//
// Eval state is invalidated rather than re-run synchronously —
// blocking the render thread on an external SQL edit would be
// surprising. The status line falls back to showing the literal
// draft values until the user clicks Apply.
func (w *dateTimeRangeWidget) resyncFromDrafts(drafts map[string]*string) bool {
	fStr := derefDraft(drafts, w.state.fromName)
	tStr := derefDraft(drafts, w.state.toName)
	if fStr == w.state.lastMirroredFrom && tStr == w.state.lastMirroredTo {
		return false
	}
	if fStr != "" {
		w.state.fromExpr = quoteForCH(fStr)
	}
	if tStr != "" {
		w.state.toExpr = quoteForCH(tStr)
	}
	w.state.lastMirroredFrom = fStr
	w.state.lastMirroredTo = tStr
	w.state.lastEval = timerangepicker.EvaluatedRange{}
	w.state.lastEvalSet = false
	w.state.lastEvalErr = nil
	w.state.packedRange = ""
	w.state.idGeneration++
	return true
}

func derefDraft(drafts map[string]*string, name string) string {
	if p, ok := drafts[name]; ok && p != nil {
		return *p
	}
	return ""
}

// quoteForCH wraps a literal datetime in single quotes when the
// draft value is already a parseable datetime — the picker's
// expression fields expect SQL, so `2026-05-24 12:00:00` becomes
// `'2026-05-24 12:00:00'`. Values that already look like
// expressions (start with `(` or are bare `anchor_now`-prefixed
// identifiers) pass through unchanged.
func quoteForCH(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return s
	}
	if s[0] == '\'' || s[0] == '(' {
		return s
	}
	if _, ok := parseChDateTime(s); ok {
		return "'" + s + "'"
	}
	return s
}

// writeDraft is the safe-update helper for `*ptr` drafts — no-op
// when the draft slot was never registered (defensive against a
// dispatcher bug rather than a real possibility today).
func writeDraft(drafts map[string]*string, name, value string) {
	if ptr, ok := drafts[name]; ok && ptr != nil {
		*ptr = value
	}
}

// formatChDateTimeMilli renders t as the canonical CH DateTime64(3)
// literal — `YYYY-MM-DD HH:MM:SS.mmm`, UTC. CH accepts this verbatim
// inside `SET param_X = ...` regardless of the slot's DateTime vs
// DateTime64 declaration; sub-second precision is truncated to zero
// for plain DateTime targets without loss.
func formatChDateTimeMilli(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}
