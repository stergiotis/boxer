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
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Overlay tones, all from the design system (ADR-0037 palette) so the editor
// agrees with the banners and pane chrome that report the same conditions.
// The statement tint is one faint step above the code editor's extreme
// background rather than a colour of its own — it marks a region, it does not
// carry a severity.
var (
	styleErrorTone   = color.Hex(styletokens.ErrorDefault.AsHex())
	styleWarningTone = color.Hex(styletokens.WarningDefault.AsHex())
	styleStmtTint    = color.Hex(styletokens.NeutralBgFaint.AsHex())
	// styleCaretRowMark outlines the PARAMETERS row holding the placeholder the
	// caret is in — the pane's counterpart to the editor's statement tint.
	//
	// An outline, not a fill, and this is empirical: the pane's own background
	// is a surface tone, so a fill has to thread between invisible and
	// illegible. AccentSubtle and NeutralBgSurface both vanished against it,
	// and AccentDefault as a fill washed out the row's own text. A one-pixel
	// accent outline reads unambiguously whatever the backdrop, and cannot
	// touch the contrast of what it surrounds.
	styleCaretRowMark = color.Hex(styletokens.AccentDefault.AsHex())
)

// byteOffsetOfChar converts a char (rune) offset into a byte offset into s,
// clamping to the buffer.
//
// The clamp is load-bearing, not defensive: the caret arrives one frame late,
// so an offset computed against a longer buffer routinely outruns the copy we
// hold after a deletion. Clamping to the end is the honest answer — the caret
// really is at the end of what we can see.
func byteOffsetOfChar(s string, chars int) int {
	if chars <= 0 {
		return 0
	}
	off := 0
	for n := 0; n < chars; n++ {
		if off >= len(s) {
			return len(s)
		}
		_, sz := utf8.DecodeRuneInString(s[off:])
		off += sz
	}
	return off
}

// refreshCaret converts the packed caret the editor reported last frame into a
// byte offset into the buffer we currently hold. Called once per editor render
// so every consumer within the frame agrees.
//
// `offset` is where the reporting editor's buffer starts inside inst.sql — 0
// for the plain editor, the elided prelude's length for the residual mirror —
// so caretByte is always in inst.sql coordinates.
func (inst *PlayApp) refreshCaret(buf string, offset int) {
	start, _ := c.UnpackCursorRange(inst.caretPacked)
	inst.caretByte = offset + byteOffsetOfChar(buf, start)
}

// byteOffsetOfLineCol converts ANTLR's (1-based line, 0-based rune column)
// into a byte offset into sql. Out-of-range positions clamp into the buffer:
// a parser that ran off the end reports the position after the last token,
// and the caller wants the nearest real token, not a failure.
func byteOffsetOfLineCol(sql string, line, col int) int {
	if line < 1 {
		line = 1
	}
	off := 0
	for l := 1; l < line; l++ {
		nl := strings.IndexByte(sql[off:], '\n')
		if nl < 0 {
			// Fewer lines than reported — clamp to the end.
			return len(sql)
		}
		off += nl + 1
	}
	// Walk `col` runes from the start of the line, stopping at its end.
	for r := 0; r < col && off < len(sql); r++ {
		if sql[off] == '\n' {
			break
		}
		_, sz := utf8.DecodeRuneInString(sql[off:])
		off += sz
	}
	if off > len(sql) {
		off = len(sql)
	}
	return off
}

// errorTokenSpan returns the byte range of the lexical token a syntax error
// points at, for the error underline.
//
// The lookup is at the lex tier deliberately: the buffer failed to parse, so
// there is no CST to ask, but the lexer still tokenises it (the same
// independence argument the L1 colours rest on). A position landing on
// whitespace — which is what an unexpected-EOF error reports — resolves to
// the nearest real token, preferring the one that follows so the underline
// sits where the user is typing.
func errorTokenSpan(sql string, pos syntaxErrorPos) (start, stop uint32, ok bool) {
	if !pos.Ok || sql == "" {
		return
	}
	off := byteOffsetOfLineCol(sql, pos.Line, pos.Column)
	spans := highlight.HighlightLex(sql)
	if len(spans) == 0 {
		return
	}
	real := func(s highlight.Span) bool {
		return s.Category != highlight.CatWhitespace && s.Stop > s.Start
	}
	// The covering span, if it is a real token.
	for i, s := range spans {
		if off < s.Start || off >= s.Stop {
			continue
		}
		if real(s) {
			return uint32(s.Start), uint32(s.Stop), true
		}
		// Whitespace: look forward, then back.
		for j := i + 1; j < len(spans); j++ {
			if real(spans[j]) {
				return uint32(spans[j].Start), uint32(spans[j].Stop), true
			}
		}
		for j := i - 1; j >= 0; j-- {
			if real(spans[j]) {
				return uint32(spans[j].Start), uint32(spans[j].Stop), true
			}
		}
		return
	}
	// Past the last span (position at EOF): the last real token.
	for j := len(spans) - 1; j >= 0; j-- {
		if real(spans[j]) {
			return uint32(spans[j].Start), uint32(spans[j].Stop), true
		}
	}
	return
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
	// Active-statement tint, multi-statement buffers only — the common
	// single-statement buffer stays visually unchanged. Emitted first so the
	// error underline, which is narrower, composes on top of it.
	if haveStmt && total > 1 {
		out = append(out, codeview.StyledSection{
			Start: uint32(stmt.Src.Start), Stop: uint32(stmt.Src.End),
			Flags: codeview.StyleBackground,
			Color: styleStmtTint,
		})
	}
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

// shiftStyledSections rebases spans expressed in inst.sql onto a view that
// starts `offset` bytes into it (the residual mirror behind the hide-prelude
// toggle). Spans that fall entirely inside the elided prefix are dropped;
// one that straddles the boundary is trimmed to the visible part.
func shiftStyledSections(secs []codeview.StyledSection, offset int, viewLen int) (out []codeview.StyledSection) {
	if offset == 0 {
		return secs
	}
	for _, s := range secs {
		start, stop := int(s.Start)-offset, int(s.Stop)-offset
		if start < 0 {
			start = 0
		}
		if stop > viewLen {
			stop = viewLen
		}
		if stop <= start {
			continue
		}
		s.Start, s.Stop = uint32(start), uint32(stop)
		out = append(out, s)
	}
	return
}
