package sqleditor

import (
	"sync/atomic"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// DefaultRows is the editor's height before an embedder computes one from the
// pane's available size.
const DefaultRows uint32 = 10

const (
	// rowHeightSeedFactor converts the font's pt size into the frame-0 row
	// height, before the measurement lands. Real egui row heights run ≈1.2–1.3×
	// the pt size; 1.45 is deliberately generous (see [Editor.measureRowHeight]
	// on seed direction). Same constant, same reason, as the treemap's label
	// gates.
	rowHeightSeedFactor float64 = 1.45
	// rowProbeText is the row-height probe. Any non-empty string measures the
	// same single-line galley height.
	rowProbeText = "Mg"
)

// Frame is one frame's binding: what the editor is bound to and how it is
// sized. See the package doc for the coordinate contract Offset establishes.
type Frame struct {
	// IDSlot is the stable widget-id slot for the TextEdit and its gutter.
	// Two editors in one app need two slots; switching an editor's slot
	// mid-session resets its caret channel, so treat it as an identity.
	IDSlot string
	// Value is the bound buffer. The widget writes edits back through it
	// (SendRespVal), so it must outlive the frame.
	Value *string
	// Offset is where Value starts inside the canonical buffer. Zero when
	// Value IS the canonical buffer, which is the ordinary case.
	Offset int
	// Canonical is the buffer Offset indexes into. Empty means Value is it.
	// An embedder binding a suffix view (play's residual-only mirror behind
	// its hide-prelude toggle) sets both this and Offset.
	Canonical string
	// Hint is the empty-buffer placeholder.
	Hint string
	// Rows is the TextEdit's desired_rows. Zero means DefaultRows.
	Rows uint32
	// Insert is text to splice at the caret this frame, once
	// (TextEditFluid.InsertAtCursor, ADR-0063). Empty means no insert.
	Insert string
	// Density resolves the design system's spacing tokens; the zero value is
	// the default preset.
	Density styletokens.DensityE
}

// Decoration is the sparse overlay channel the embedder contributes — the
// decorations only it can derive, such as a parse error's underline or a
// placeholder its own run gate is waiting on.
//
// Spans are in CANONICAL coordinates; the widget rebases them onto the bound
// view. The statement tint is deliberately absent: it follows from buffer and
// caret alone, so the widget emits it (ADR-0147 §SD2) and an embedder that
// added its own would draw it twice.
type Decoration struct {
	// Styled are the overlay sections, applied in list order — later
	// sections compose on top of earlier ones.
	Styled []codeview.StyledSection
	// SubqueryMark is a range within the active statement that a narrowed run
	// would ship, empty when there is none. It travels beside Styled rather
	// than inside it because the gutter marks it whether or not the embedder
	// drew a tint for it.
	SubqueryMark nanopass.SourceRange
}

// Result is what [Editor.Bind] publishes: everything that follows from the
// buffer and the caret, in canonical coordinates.
type Result struct {
	// Buffer is the canonical buffer this frame — Frame.Canonical when the
	// embedder bound a suffix view, else the bound buffer itself.
	Buffer string
	// Caret is the caret's byte offset into Buffer.
	//
	// It is one frame late by construction: the caret crosses the FFI after
	// the editor rendered, so this resolves LAST frame's packed caret against
	// THIS frame's buffer, clamping when an edit shortened it.
	Caret int
	// Statement is the statement the caret is in; Ok is false when the body
	// holds none (an empty or comment-only buffer).
	Statement StatementRange
	// Ok reports whether Statement was resolved.
	Ok bool
	// Number is Statement's 1-based position, and Total the statement count.
	// Total > 1 is the multi-statement condition: the tint renders, and a run
	// ships one statement instead of the whole buffer.
	Number int
	Total  int
	// Run is what a run-under-cursor ships: the whole trimmed buffer for a
	// single-statement body, else the SET prelude plus the caret's statement.
	// See [RunBufferFor] for why the single-statement case is not a slice.
	Run string
	// Prelude is the SET prelude's extent in Buffer, empty when there is none.
	Prelude nanopass.SourceRange
	// BodyOffset is where the body starts inside Buffer — past the SET
	// prelude. It is what [WithPrelude] takes, so an embedder composing its
	// own narrowed run (play's run-subquery) puts the prelude back the same
	// way [RunBufferFor] does rather than re-deriving the boundary.
	BodyOffset int
	// Entity is what the caret is pointing at, lexically: the name it sits on
	// and the calls enclosing it. EntityOk is false when it is on nothing
	// nameable and inside no call.
	//
	// Published rather than left to each consumer because it is derived from
	// buffer and caret alone (ADR-0147 §SD2), and because two consumers
	// deriving it separately would eventually disagree about which frame's
	// caret they used. Documentation lookup reads it today; completion is the
	// next reader of the same walk.
	Entity   highlight.CaretEntity
	EntityOk bool
}

// Editor is one SQL editing surface's cross-frame state: the caret channel,
// the colour tiers and the statement-split memo. Construct with [New]; call
// [Editor.Bind] then [Editor.Render] once per frame, in that order.
//
// Render-thread-only, like every stateful widget (ADR-0013).
type Editor struct {
	// caretPacked is the packed cursor range the TextEdit reported last
	// frame, in char offsets into the buffer it was bound to.
	caretPacked uint64

	// frame/result are what Bind resolved, held for Render.
	frame  Frame
	result Result
	bound  bool

	// Statement-split memo, keyed by the buffer it describes.
	stmtRanges []StatementRange
	stmtOffset int
	stmtFor    string
	stmtOk     bool

	// Whole-buffer lex spans, keyed by the buffer they describe (see spans).
	spanCache []highlight.Span
	spanFor   string
	spanOk    bool

	// L1 lex-tier colour job, keyed by the buffer it describes.
	lexJob typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	lexSrc string
	lexOk  bool

	// sem is the L2 semantic tier (async, see tiers.go).
	sem semanticTier

	// paneW/paneH are what the pane probe reported last, held across frames so
	// a tab that was hidden — its probe absent from the R21 drain, so
	// unreadable on the frame it comes back — reopens at the size it had
	// rather than at the fallback.
	paneW float32
	paneH float32

	// rowPx is the measured monospace row height, refreshed each Sync through
	// an r9 databinding; rowFontPt is the size it describes, so a density
	// change re-seeds rather than carrying the old font's answer.
	rowPx     float64
	rowFontPt float32

	// probeSalt is this editor's share of the register slot map, minted per
	// construction. See [nextEditorSalt].
	probeSalt uint64
}

// editorSeq numbers Editor constructions in this process; nextEditorSalt spaces
// them out so each editor owns its register slots.
var editorSeq atomic.Uint64

// nextEditorSalt mints a per-editor slot salt. The IDSlot alone cannot separate
// two editors that are not in the same app: embedders pass a constant
// ("sqlEditor"), so two windows of one app hash to the same seq and read each
// other's pane and row-height measurements — the r18 shape, surviving in the
// seq-keyed register for as long as the seq ignores the instance. The tag keeps
// another package's equal counter off these slots.
func nextEditorSalt() (salt uint64) {
	const editorSaltTag = 0x53716c45_64697421 // "SqlEdit!"
	return (editorSeq.Add(1) * 0x9e3779b97f4a7c15) ^ editorSaltTag
}

// slotId derives a stable per-editor register slot from the IDSlot and a role,
// so two editors read their own measurements and not each other's — whether
// they sit in one app or in two windows of it.
func (inst *Editor) slotId(idSlot, role string) (id uint64) {
	if inst.probeSalt == 0 {
		// The zero-value Editor is a documented construction; mint on first use.
		inst.probeSalt = nextEditorSalt()
	}
	return c.ProbeSeq("sqleditor#"+idSlot, role) ^ inst.probeSalt
}

// PaneHeight is the height that was free for the editor where it last rendered
// — the pane minus whatever the embedder drew above it, and NOT minus what it
// draws below. Zero before the first [Editor.Render], and one frame stale after
// that, like every register read.
//
// Published because [Frame.Rows] is the embedder's to choose and it cannot
// measure this itself: the only unscoped way to ask egui for free space is the
// single-slot r18 register, which the last capture of a frame wins. The editor
// is already probing its own pane for the width, so it is the honest place for
// the answer. An embedder filling the pane subtracts what it will render below
// the editor and divides by [Editor.RowHeight].
func (inst *Editor) PaneHeight() (px float32) { return inst.paneH }

// RowHeight is the editor's measured monospace row height — what one unit of
// [Frame.Rows] is worth, so an embedder can convert [Editor.PaneHeight] into a
// row count. Analytically seeded before the first measurement lands, and
// floored at the font size, since no row is shorter than its em box.
//
// Measured rather than assumed because the host may leave the monospace face
// unconfigured, and a hardcoded guess is off by enough to matter: at ~14 pt the
// real row height here is ≈11.4 px, so the previous constant 16 left a quarter
// of the pane blank once the height it divided was the editor's own.
func (inst *Editor) RowHeight() (px float32) {
	return max(float32(inst.rowPx), inst.rowFontPt)
}

// measureRowHeight (re-)arms the row-height probe. Re-emitted every frame
// because Sync resets databindings, and re-seeded when the density changes the
// font size. A single non-wrapped line's galley height IS the font's row
// height, so the probe string is arbitrary and the answer is a constant after
// the first Sync — the one-frame lag is a one-time warm-up, not a lag.
//
// The seed over-estimates on purpose: too tall means too few rows and a strip
// of unused pane for one frame, where too short means an editor taller than its
// pane, which the embedder's layout has to absorb.
func (inst *Editor) measureRowHeight(f Frame) {
	pt := styletokens.ScaledPt(styletokens.BodyPt, f.Density)
	if inst.rowFontPt != pt {
		inst.rowFontPt = pt
		inst.rowPx = float64(pt) * rowHeightSeedFactor
	}
	c.MeasureTextSizeBind(inst.slotId(f.IDSlot, "row-w"), inst.slotId(f.IDSlot, "row-h"),
		rowProbeText, pt, true, nil, &inst.rowPx)
}

// New returns an editor. The zero value is also usable; New exists so a
// construction site reads as one.
func New() (inst *Editor) {
	inst = &Editor{probeSalt: nextEditorSalt()}
	return
}

// Bind resolves last frame's caret against this frame's buffer and derives
// everything that follows from the two. Call it once per frame, BEFORE
// composing the [Decoration] that [Editor.Render] consumes — the overlays and
// the run gate must all see one caret per frame or they disagree about which
// statement is active.
//
// A nil Frame.Value returns the zero Result and makes Render a no-op.
func (inst *Editor) Bind(f Frame) (res Result) {
	inst.frame = f
	inst.bound = f.Value != nil
	inst.result = Result{}
	if !inst.bound {
		return
	}
	view := *f.Value
	canonical := f.Canonical
	if canonical == "" {
		canonical = view
	}
	// The caret is reported in char offsets into the buffer that RENDERED, so
	// it is resolved against that same view and then lifted into canonical
	// coordinates — never against the canonical buffer directly, which would
	// be short by the elided prefix whenever a suffix view is bound.
	start, _ := c.UnpackCursorRange(inst.caretPacked)
	caret := f.Offset + ByteOffsetOfChar(view, start)

	ranges, bodyOffset := inst.Statements(canonical)
	stmt, _, total, ok := SelectStatement(ranges, caret)
	run, number, _ := ComposeRunBuffer(canonical, ranges, bodyOffset, caret)
	entity, entityOk := highlight.EntityAt(inst.spans(canonical), caret)

	inst.result = Result{
		Buffer:     canonical,
		Caret:      caret,
		Statement:  stmt,
		Ok:         ok,
		Number:     number,
		Total:      total,
		Run:        run,
		Prelude:    PreludeRange(canonical, bodyOffset),
		BodyOffset: bodyOffset,
		Entity:     entity,
		EntityOk:   entityOk,
	}
	return inst.result
}

// Result returns what the last [Editor.Bind] published, for a consumer that
// runs outside the render call — a status line, a run gate, a docs pane.
func (inst *Editor) Result() (res Result) { return inst.result }

// statementTint is the active statement's background, the one overlay the
// widget owns (ADR-0147 §SD2 — it follows from buffer and caret alone).
//
// Multi-statement buffers only: the common single-statement buffer stays
// visually unchanged, and a full-width wash over the only statement there is
// would say nothing. Emitted FIRST so the embedder's narrower decorations —
// an error underline, a placeholder mark — compose on top of it.
func (inst *Editor) statementTint() (secs []codeview.StyledSection) {
	r := inst.result
	if !r.Ok || r.Total <= 1 {
		return
	}
	return []codeview.StyledSection{{
		Start: uint32(r.Statement.Src.Start),
		Stop:  uint32(r.Statement.Src.End),
		Flags: codeview.StyleBackground,
		Color: ToneStatementTint,
	}}
}

// Render draws the editor with the decoration the embedder composed from the
// [Editor.Bind] result. It must follow a Bind in the same frame.
//
// One horizontal row. The gutter and the editor share the enclosing VERTICAL
// scroll scope — they are siblings in it, so a line's number stays on its line
// — but the editor owns the HORIZONTAL one: no-wrap makes the galley as wide
// as the longest line, and a gutter that slid out of view on the first long
// line would not be a gutter. The editor's own scroll area is therefore inside
// the row, with the gutter pinned outside it.
func (inst *Editor) Render(ids *c.WidgetIdStack, d Decoration) {
	if !inst.bound {
		return
	}
	f := inst.frame
	view := *f.Value
	rows := f.Rows
	if rows == 0 {
		rows = DefaultRows
	}

	// The widget's own tint first, then the embedder's sections, then rebase
	// the whole list onto the bound view in one pass. Rebasing once here is
	// what keeps the gutter and the text decoration in the same coordinates.
	styled := append(inst.statementTint(), d.Styled...)
	styled = ShiftStyledSections(styled, f.Offset, len(view))
	subq := ShiftRange(d.SubqueryMark, f.Offset, len(view))

	inst.measureRowHeight(f)

	// The pane, from this editor's OWN seq-keyed probe (R21). Emitted here,
	// before anything is placed, because the op reports the space left for the
	// NEXT widget: from this point it is the editor's own pane, and it does not
	// move when the editor draws into it, so sizing off it cannot ratchet.
	//
	// Not CaptureAvailableSize: that register is a single process-wide slot
	// that the last capture of a frame wins, so an editor reading it is sized
	// by whichever unrelated panel captured after it — play's Detail pane,
	// whose timeline captures the narrow side column, was the case that
	// surfaced this. One-frame lag, like every register read.
	if w, h, ok := c.CapturePaneSize(inst.slotId(f.IDSlot, "pane")); ok && w > 0 {
		inst.paneW, inst.paneH = w, h
	}
	paneW := inst.paneW
	if paneW <= 0 {
		paneW = editorFallbackWidthPx
	}
	m := buildGutterModel(view, styled, subq, f.Density)
	editorW := editorWidthPx(view, m.charPx, paneW-m.widthPx())

	// Both children carry IDSlot in their own r7 key, so two editors in one
	// app are already isolated without an enclosing IdScope — which is why
	// IDSlot is documented as an identity rather than a label.
	for range c.Horizontal().KeepIter() {
		for range c.Vertical().KeepIter() {
			// Nudge the gutter down by the TextEdit's inner top margin so
			// row 1 sits on line 1 rather than on the frame.
			c.AddSpace(textEditTopMarginPx)
			renderGutter(ids, f.IDSlot+"Gutter", m)
		}
		// AutoShrink(false, false): the row must keep its full width and
		// height rather than collapsing onto the content, which is the
		// standing quirk of scroll areas in this toolkit.
		for range c.ScrollArea().Hscroll(true).Vscroll(false).AutoShrink(false, false).KeepIter() {
			inst.textField(ids, f, view, rows, styled, editorW)
		}
	}
}

// textField is the multi-line CodeEditor TextEdit chain.
//
// widthPx is the editor's desired width. No-wrap layout (the gutter's
// alignment contract) makes the galley as wide as the longest line, and egui
// caps a TextEdit's allocation at its desired width — so this has to be the
// content width, not +Inf, or the tail of a long line is clipped and the
// enclosing scroll area never learns it is there.
func (inst *Editor) textField(ids *c.WidgetIdStack, f Frame, view string, rows uint32, styled []codeview.StyledSection, widthPx float32) {
	b := c.TextEdit(ids.PrepareStr(f.IDSlot), view, true).
		CodeEditor().
		NoWrapLayout().
		DesiredRows(rows).
		DesiredWidth(widthPx).
		HintText(f.Hint)
	// Hand a pending splice to the editor so the Rust side applies it at the
	// caret next frame (TextEditFluid.InsertAtCursor, ADR-0063).
	if f.Insert != "" {
		b = b.InsertAtCursor(f.Insert)
	}
	// Lex/semantic syntax colour (ADR-0130): sections describe the buffer as
	// of this frame's binding; the Rust layouter applies them advisorily.
	if job, ok := inst.highlightJob(view); ok {
		b = b.HighlightJob(job)
		// The overlays ride the same layouter, so they only reach the buffer
		// when a highlight job installed one — which is also the only case
		// where their spans have been reconciled against a live edit.
		if job, ok := codeview.BuildStyledSections(styled); ok {
			b = b.SectionStyled(job)
		}
	}
	b.ReportCursor().SendRespValCursor(f.Value, &inst.caretPacked)
}

// spans is [highlight.HighlightLex] over the whole buffer, memoised.
//
// A second lex per edit, beside the one [Editor.Statements] runs over the
// body: they cover different extents (the split skips the SET prelude, the
// caret does not) and merging them would mean re-deriving the prelude boundary
// inside the entity walk. At ~26 µs for a statement-sized buffer, once per
// edit rather than once per frame, the duplicate is cheaper than the coupling.
func (inst *Editor) spans(sql string) (out []highlight.Span) {
	if inst.spanOk && inst.spanFor == sql {
		return inst.spanCache
	}
	inst.spanCache = highlight.HighlightLex(sql)
	inst.spanFor = sql
	inst.spanOk = true
	return inst.spanCache
}
