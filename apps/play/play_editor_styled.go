package play

// Styled overlays for the SQL editor (ADR-0130 L3, `sectionStyled`).
//
// These are the sparse decoration channel that rides alongside the lexical
// colours: an error-toned underline on the token a failed parse pointed at,
// a background tint on the statement under the caret, and warning-toned
// underlines on placeholders the Run gate is still waiting for. All spans are
// byte ranges into inst.sql; the editor wiring shifts them when it binds a
// sliced view of that buffer instead.
//
// Unlike the colour job, the list is rebuilt every frame rather than cached.
// It is a handful of sections, and its inputs move on caret travel — not only
// on edits — so a text-keyed cache would miss more often than it hit.

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// Overlay tones. The editor's own vocabulary is the widget's (ADR-0147) so
// play cannot disagree with the gutter about what a tone means — the marks
// lane recognises a section BY its tone, and a second definition here would
// compile, look right, and silently stop earning marks the day either moved.
var (
	styleErrorTone    = sqleditor.ToneError
	styleWarningTone  = sqleditor.ToneWarning
	styleCarriedTone  = sqleditor.ToneCarried
	styleSubqueryTint = sqleditor.ToneSubqueryTint
	// styleCaretRowMark outlines the PARAMETERS row holding the placeholder the
	// caret is in — the pane's counterpart to the editor's statement tint. It
	// stays play's because the pane is play's.
	//
	// An outline, not a fill, and this is empirical: the pane's own background
	// is a surface tone, so a fill has to thread between invisible and
	// illegible. AccentSubtle and NeutralBgSurface both vanished against it,
	// and AccentDefault as a fill washed out the row's own text. A one-pixel
	// accent outline reads unambiguously whatever the backdrop, and cannot
	// touch the contrast of what it surrounds.
	styleCaretRowMark = color.Hex(styletokens.AccentDefault.AsHex())
)

// errorTokenSpan returns the byte range of the lexical token a syntax error
// points at, for the error underline.
//
// The lookup is at the lex tier deliberately: the buffer failed to parse, so
// there is no CST to ask, but the lexer still tokenises it (the same
// independence argument the L1 colours rest on). A position landing on
// whitespace — which is what an unexpected-EOF error reports — resolves to
// the nearest real token, preferring the one that follows so the underline
// sits where the user is typing.
// The resolution itself is the widget's ([sqleditor.ErrorTokenSpan]); what
// stays here is the syntaxErrorPos shape play's parse pipeline reports in.
func errorTokenSpan(sql string, pos syntaxErrorPos) (start, stop uint32, ok bool) {
	if !pos.Ok {
		return
	}
	return sqleditor.ErrorTokenSpan(sql, pos.Line, pos.Column)
}

// editorStyledSections assembles this frame's overlay list for the buffer the
// editor is bound to, in inst.sql coordinates.
//
// Every producer is gated on quiescence — inst.sql == inst.formattedFor, i.e.
// the debounced pipeline has seen exactly this buffer. While the user types,
// the recorded spans describe the previous buffer, and the Rust-side
// reconcile only compensates for a single edit region; showing nothing is
// honest, and the overlay reappears within the debounce window.
func (inst *PlayApp) editorStyledSections() (out []codeview.StyledSection) {
	if inst.sql == "" || inst.sql != inst.formattedFor {
		return nil
	}
	stmt, _, total, haveStmt := inst.caretStatement()
	// The active-statement tint is deliberately absent: it follows from the
	// buffer and the caret alone, so the widget emits it (ADR-0147 §SD2).
	// Emitting it here too would draw it twice — and would put it back under
	// this function's quiescence gate, which it never needed.
	out = append(out, inst.subqueryModeSections()...)
	// Unfilled-placeholder underlines (ADR-0124 §SD8's `Src` consumers). The
	// set is unfilledSet — the SAME set the Run gate and the pane's
	// "needs a value" mark read, so the editor cannot disagree with either
	// about what is blocking the Run. Spans come from the slots' Src, which
	// after the M1 hygiene fix index the buffer directly.
	if unfilled := inst.unfilledSet(); len(unfilled) > 0 {
		for _, s := range inst.paramSlots {
			if !unfilled[s.Name] || s.Src.Empty() || s.Src.End > len(inst.sql) {
				continue
			}
			out = append(out, codeview.StyledSection{
				Start: uint32(s.Src.Start), Stop: uint32(s.Src.End),
				Flags: codeview.StyleUnderline,
				Color: styleWarningTone,
			})
		}
	}
	// Error underline: the token a parse tripped on.
	//
	// Which parse depends on the buffer. grammar1's QueryStmt is
	// single-statement, so a multi-statement buffer ALWAYS fails the
	// whole-buffer parse — at the second statement's first token, which says
	// nothing about either statement. There the underline comes from parsing
	// the caret's statement on its own, which is also the statement Run
	// ships; a broken sibling then stays the sibling's problem.
	if start, stop, ok := inst.errorSpan(stmt, total, haveStmt); ok {
		out = append(out, codeview.StyledSection{
			Start: start, Stop: stop,
			Flags: codeview.StyleUnderline,
			Color: styleErrorTone,
		})
	}
	return out
}

// subqueryModeSections is the Subquery toggle's decoration: what
// Ctrl+Shift+Enter would ship, and what does or does not travel with it.
//
// Three channels, because they are three different claims:
//
//   - The query itself gets a BACKGROUND — it is a region, and the only one
//     here that is.
//   - Its environment — the WITH items in scope and the SET prelude — gets an
//     info-toned UNDERLINE. A second background would read as a second region,
//     when the point is that these are lines elsewhere in the buffer that the
//     query depends on.
//   - References that will NOT resolve get the error tone, the same one a
//     syntax error uses, because the consequence is the same: the server
//     rejects it. This is the correlated-subquery case the composition cannot
//     repair.
//
// All three answer the same question — "the query the caret is in" — so all
// three are drawn from the same unit, root or not. What gates the tint is not
// whether the unit is the statement's own query but whether it is a PROPER
// SUBSET of the statement: the tint marks the query as distinct from what
// surrounds it, and a unit covering the whole statement has nothing to be
// distinct from. That is the plain `SELECT …` buffer, where a full-width wash
// would say nothing; a `WITH …` clause, a trailing FORMAT, or an enclosing
// query all put something outside the unit and earn the tint.
//
// "What Ctrl+Shift+Enter would run" is a different question, and the gutter's
// `|` mark answers it — present only where running the query alone differs
// from Run. So a tinted region without a `|` reads as "this is the query, and
// it is already what runs".
func (inst *PlayApp) subqueryModeSections() (out []codeview.StyledSection) {
	if !inst.subqueryMode {
		return nil
	}
	sub, stmt, ok := inst.caretSubquery()
	if !ok {
		return nil
	}
	base := stmt.Src.Start
	if sub.Src.Start > 0 || sub.Src.End < stmt.Src.End-base {
		out = append(out, codeview.StyledSection{
			Start: uint32(base + sub.Src.Start), Stop: uint32(base + sub.Src.End),
			Flags: codeview.StyleBackground,
			Color: styleSubqueryTint,
		})
	}
	if p := inst.preludeRange(); !p.Empty() {
		out = append(out, codeview.StyledSection{
			Start: uint32(p.Start), Stop: uint32(p.End),
			Flags: codeview.StyleUnderline,
			Color: styleCarriedTone,
		})
	}
	for _, r := range sub.WithItems {
		out = append(out, codeview.StyledSection{
			Start: uint32(base + r.Start), Stop: uint32(base + r.End),
			Flags: codeview.StyleUnderline,
			Color: styleCarriedTone,
		})
	}
	// Last, so a reference that sits inside the tinted query composes on top.
	for _, r := range sub.Unresolved {
		out = append(out, codeview.StyledSection{
			Start: uint32(base + r.Start), Stop: uint32(base + r.End),
			Flags: codeview.StyleUnderline,
			Color: styleErrorTone,
		})
	}
	return out
}

// errorSpan resolves the error underline's span in inst.sql coordinates.
func (inst *PlayApp) errorSpan(stmt statementRange, total int, haveStmt bool) (start, stop uint32, ok bool) {
	if total <= 1 {
		// The debounced pipeline's own verdict on the whole buffer.
		pos, isPos := inst.formattedErr.(syntaxErrorPos)
		if !isPos {
			return
		}
		return errorTokenSpan(inst.sql, pos)
	}
	if !haveStmt {
		return
	}
	text := inst.sql[stmt.Src.Start:stmt.Src.End]
	pos := inst.statementSyntaxError(text)
	if !pos.Ok {
		return
	}
	start, stop, ok = errorTokenSpan(text, pos)
	return start + uint32(stmt.Src.Start), stop + uint32(stmt.Src.Start), ok
}

// statementSyntaxError parses one statement of a multi-statement buffer,
// memoised on its exact text.
//
// The memo is what makes this affordable from a per-frame producer: the parse
// runs when the caret moves to a different statement or that statement's text
// changes, not on every frame. A one-entry memo is enough — the caret is in
// one statement at a time.
func (inst *PlayApp) statementSyntaxError(text string) syntaxErrorPos {
	if inst.stmtErrOk && inst.stmtErrFor == text {
		return inst.stmtErrPos
	}
	inst.stmtErrFor = text
	inst.stmtErrPos = firstSyntaxError(text)
	inst.stmtErrOk = true
	return inst.stmtErrPos
}

// caretSubqueryRange is the query run-subquery would ship, in inst.sql
// coordinates — empty when the caret is at statement level, when the statement
// does not parse, or while the buffer is mid-edit.
//
// Unlike the styled sections this ignores the Subquery toggle: the gutter marks
// it either way, so the gesture is never entirely without an affordance.
func (inst *PlayApp) caretSubqueryRange() (r nanopass.SourceRange) {
	if inst.sql == "" || inst.sql != inst.formattedFor {
		return r
	}
	sub, stmt, ok := inst.caretSubquery()
	if !ok || sub.Root {
		return r
	}
	return nanopass.SourceRange{
		Start: stmt.Src.Start + sub.Src.Start,
		End:   stmt.Src.Start + sub.Src.End,
	}
}
