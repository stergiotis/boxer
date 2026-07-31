package sqleditor

// Offset arithmetic shared by the caret channel, the overlay rebasing and the
// error underline (ADR-0130 L3, moved here by ADR-0147 §SD1). All of it is pure
// over a buffer and an offset, which is what makes it the widget's rather than
// an embedder's.

import (
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// ByteOffsetOfChar converts a char (rune) offset into a byte offset into s,
// clamping to the buffer.
//
// The clamp is load-bearing, not defensive: the caret arrives one frame late,
// so an offset computed against a longer buffer routinely outruns the copy we
// hold after a deletion. Clamping to the end is the honest answer — the caret
// really is at the end of what we can see.
func ByteOffsetOfChar(s string, chars int) int {
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

// ByteOffsetOfLineCol converts ANTLR's (1-based line, 0-based rune column)
// into a byte offset into sql. Out-of-range positions clamp into the buffer:
// a parser that ran off the end reports the position after the last token,
// and the caller wants the nearest real token, not a failure.
func ByteOffsetOfLineCol(sql string, line, col int) int {
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

// ErrorTokenSpan returns the byte range of the lexical token at a (line,
// column) position, for an error underline.
//
// The lookup is at the lex tier deliberately: the buffer failed to parse, so
// there is no CST to ask, but the lexer still tokenises it (the same
// independence argument the L1 colours rest on). A position landing on
// whitespace — which is what an unexpected-EOF error reports — resolves to
// the nearest real token, preferring the one that follows so the underline
// sits where the user is typing.
func ErrorTokenSpan(sql string, line, col int) (start, stop uint32, ok bool) {
	if sql == "" {
		return
	}
	off := ByteOffsetOfLineCol(sql, line, col)
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

// ShiftRange rebases one range the way [ShiftStyledSections] rebases a list.
// A range falling entirely inside the elided prefix comes back empty.
func ShiftRange(r nanopass.SourceRange, offset, viewLen int) nanopass.SourceRange {
	if r.Empty() || offset == 0 {
		return r
	}
	start, end := r.Start-offset, r.End-offset
	if start < 0 {
		start = 0
	}
	if end > viewLen {
		end = viewLen
	}
	if end <= start {
		return nanopass.SourceRange{}
	}
	return nanopass.SourceRange{Start: start, End: end}
}

// ShiftStyledSections rebases spans expressed in the canonical buffer onto a
// view that starts `offset` bytes into it (play's residual-only mirror behind
// its hide-prelude toggle). Spans that fall entirely inside the elided prefix
// are dropped; one that straddles the boundary is trimmed to the visible part.
func ShiftStyledSections(secs []codeview.StyledSection, offset int, viewLen int) (out []codeview.StyledSection) {
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
