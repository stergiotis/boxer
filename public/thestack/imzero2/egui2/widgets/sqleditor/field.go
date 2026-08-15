package sqleditor

// A SQL editing surface for a fragment rather than a statement — the control
// ADR-0187 (proposed) §SD7 puts under a panel's filter, colour block or table
// source, in place of the plain TextEdit those carry today.

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// FieldFrame is one frame's binding for a [Field]: what it is bound to, how it
// is sized, and what the embedder wants marked.
type FieldFrame struct {
	// IDSlot is the stable widget-id slot. Two fields in one app need two
	// slots; it is an identity, not a label.
	IDSlot string
	// Value is the bound fragment. The widget writes edits back through it
	// (SendRespVal), so it must outlive the frame. A nil Value makes
	// [Field.Render] a no-op.
	Value *string
	// Hint is the empty-buffer placeholder.
	Hint string
	// Rows is the TextEdit's desired_rows. Zero means one — a fragment is a
	// line — and one selects egui's single-line form, in which Enter inserts
	// nothing and needs no disarming. Above one selects the multi-line form,
	// which wraps.
	Rows uint32
	// Width is the desired width in points; zero leaves egui's default.
	Width float32
	// Mark is a byte range within Value to underline as an error, empty when
	// there is none. The tone is the widget's ([ToneError]) rather than the
	// embedder's, for the reason the tones are exported at all: a second
	// definition would compile, look right, and drift.
	Mark nanopass.SourceRange
}

// Field is a SQL editing surface for a FRAGMENT — a predicate, a scalar
// expression, an aliased column list, a table source — as opposed to [Editor],
// which edits statements.
//
// That distinction is why it exists rather than being an [Editor] with
// `Rows: 1` (ADR-0187 (proposed) §SD7). A fragment has no statement split, no
// SET prelude, no run buffer and no gutter, so nearly all of [Frame] and
// [Result] would be inapplicable rather than merely unused. What the two share
// is this package: the tones have one definition between them, and so will the
// completion catalog when it arrives.
//
// It carries no caret channel. Nothing it draws is derived from the caret, so
// one Render call is the whole contract — unlike [Editor], whose overlays are
// computed from the caret and therefore force the Bind-then-Render split.
//
// Render-thread-only, like every stateful widget (ADR-0013).
type Field struct {
	// Lex-tier colour job, keyed by the fragment it describes.
	job    typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	jobFor string
	jobOk  bool
}

// NewField returns a field. The zero value is also usable; NewField exists so
// a construction site reads as one.
func NewField() (inst *Field) { return &Field{} }

// Render draws the field. A nil [FieldFrame.Value] makes it a no-op.
func (inst *Field) Render(ids *c.WidgetIdStack, f FieldFrame) {
	if f.Value == nil {
		return
	}
	view := *f.Value
	rows := f.Rows
	if rows == 0 {
		rows = 1
	}
	b := c.TextEdit(ids.PrepareStr(f.IDSlot), view, rows > 1).
		CodeEditor().
		DesiredRows(rows).
		HintText(f.Hint)
	if f.Width > 0 {
		b = b.DesiredWidth(f.Width)
	}
	// The overlays ride the highlight job's layouter, so they reach the buffer
	// only when one installed — the same ordering [Editor] keeps, and the only
	// case where their spans have been reconciled against a live edit.
	if job, ok := inst.highlightJob(view); ok {
		b = b.HighlightJob(job)
		if styled, styledOk := codeview.BuildStyledSections(markSections(len(view), f.Mark)); styledOk {
			b = b.SectionStyled(styled)
		}
	}
	b.SendRespVal(f.Value)
}

// highlightJob returns the retained CodeViewJob for the fragment, rebuilding
// the lex tier only when it changed since the last frame (idle frames re-splice
// the retained holder for free).
//
// There is no semantic tier here, and it is not an omission: [Editor]'s L2 runs
// highlight.Highlight, which needs a parse, and a fragment is not a statement —
// the lex tier is the only one that answers on one at all. An empty fragment
// renders plain, since the hint text has no bytes to colour.
func (inst *Field) highlightJob(src string) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	if src == "" {
		return job, false
	}
	if !inst.jobOk || inst.jobFor != src {
		inst.job = codeview.BuildSqlLex(src)
		inst.jobFor = src
		inst.jobOk = true
	}
	return inst.job, true
}

// markSections turns an error range into the field's one overlay, clamped to
// the fragment it describes.
//
// The clamp is load-bearing rather than defensive. The range is the embedder's,
// derived from a parse of a buffer that can be one frame behind the one being
// drawn — the FFI carries an edit back at end of frame — so a fragment the user
// has just shortened routinely arrives with a range past its end. Rust-side
// normalization drops an inverted section, but an out-of-range one would
// describe bytes that are not there.
func markSections(srcLen int, mark nanopass.SourceRange) (out []codeview.StyledSection) {
	if mark.Empty() {
		return
	}
	start, stop := max(mark.Start, 0), min(mark.End, srcLen)
	if stop <= start {
		return
	}
	return []codeview.StyledSection{{
		Start: uint32(start),
		Stop:  uint32(stop),
		Flags: codeview.StyleUnderline,
		Color: ToneError,
	}}
}
