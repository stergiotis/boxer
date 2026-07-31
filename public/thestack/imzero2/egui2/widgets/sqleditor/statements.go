package sqleditor

// Multi-statement buffers (ADR-0130 L3, moved here by ADR-0147 §SD1):
// splitting the editor buffer into statements and deciding which one the caret
// is in.
//
// The split is at the LEX tier, not the CST. It has to keep working when a
// sibling statement is broken — which is precisely when running just the one
// under the caret is most useful — and the lexer survives what the parser does
// not. It is also the lexer that makes the split safe: a `;` inside a string
// literal or a comment is part of that token, so it can never terminate a
// statement.

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
)

// StatementRange is one statement of a multi-statement buffer.
type StatementRange struct {
	// Src is the statement's significant extent: first significant token
	// through last, with the terminating `;` and any surrounding whitespace
	// or comments excluded. This is what gets tinted and what ships.
	Src nanopass.SourceRange
	// DelimEnd is the byte just past the segment's terminating `;`, or the
	// buffer end for an unterminated trailing statement. Caret selection
	// compares against this rather than against Src.End — see
	// [ActiveStatementIndex].
	DelimEnd int
}

// BodyStatementRanges splits the prelude-stripped body of sql into statements,
// in sql's own byte coordinates.
//
// The prelude is skipped deliberately: `SET param_x = 1;` lines are authored by
// a parameter surface, not typed as statements, and counting them would make
// every parameterised single-statement buffer look multi-statement. bodyOffset
// is where the body starts, so callers can compose prelude + statement.
//
// Segments with no significant content — a stray `;`, a trailing comment — are
// dropped rather than returned as empty ranges.
func BodyStatementRanges(sql string) (ranges []StatementRange, bodyOffset int) {
	bodyOffset = env.BodyOffset(sql)
	if bodyOffset >= len(sql) {
		return nil, bodyOffset
	}
	body := sql[bodyOffset:]

	firstSig, lastSig := -1, -1
	for _, s := range highlight.HighlightLex(body) {
		if s.Category == highlight.CatWhitespace || s.Category == highlight.CatComment {
			continue
		}
		if s.Text == ";" {
			if firstSig >= 0 {
				ranges = append(ranges, StatementRange{
					Src:      nanopass.SourceRange{Start: bodyOffset + firstSig, End: bodyOffset + lastSig},
					DelimEnd: bodyOffset + s.Stop,
				})
			}
			firstSig, lastSig = -1, -1
			continue
		}
		if firstSig < 0 {
			firstSig = s.Start
		}
		lastSig = s.Stop
	}
	// The trailing statement, if it is not `;`-terminated.
	if firstSig >= 0 {
		ranges = append(ranges, StatementRange{
			Src:      nanopass.SourceRange{Start: bodyOffset + firstSig, End: bodyOffset + lastSig},
			DelimEnd: len(sql),
		})
	}
	return ranges, bodyOffset
}

// ActiveStatementIndex returns the index of the statement the caret is in, or
// -1 when there is none.
//
// The boundary rule is ClickHouse play.html's `getQueryUnderCursor`, read as
// served (ClickHouse 26.6.1) and mirrored: the winner is the FIRST statement
// whose terminating `;` ends at or after the caret. So a caret resting exactly
// on the byte after a `;` still belongs to the statement it closes, while a
// caret anywhere further into the gap already belongs to the next one. A caret
// past the last `;` with only whitespace or comments after it falls back to the
// last statement rather than resolving to nothing.
func ActiveStatementIndex(ranges []StatementRange, caret int) int {
	if len(ranges) == 0 {
		return -1
	}
	for i, r := range ranges {
		if r.DelimEnd >= caret {
			return i
		}
	}
	return len(ranges) - 1
}

// SelectStatement resolves the caret against an already-split buffer. Split
// out from [ActiveStatement] so the per-frame callers can run it against the
// memo instead of re-splitting: the ranges depend on the buffer alone, the
// caret only picks among them.
func SelectStatement(ranges []StatementRange, caret int) (stmt StatementRange, index, total int, ok bool) {
	total = len(ranges)
	index = ActiveStatementIndex(ranges, caret)
	if index < 0 {
		return stmt, -1, total, false
	}
	return ranges[index], index, total, true
}

// ActiveStatement resolves the caret to a statement of sql, reporting how many
// statements the body holds. total > 1 is the multi-statement condition every
// L3 consumer gates on: the tint renders, and a run ships one statement instead
// of the buffer.
func ActiveStatement(sql string, caret int) (stmt StatementRange, index, total int, ok bool) {
	ranges, _ := BodyStatementRanges(sql)
	return SelectStatement(ranges, caret)
}

// RunBufferFor returns what a run ships for buffer sql with the caret at
// `caret`, plus the 1-based statement number and the statement count.
//
// A single-statement body ships the whole trimmed buffer, not a statement
// slice. That is a deliberate byte-identical-to-before path rather than an
// accident of the split, and it is stated here because an embedder would
// otherwise have to rediscover it: total <= 1 is the "nothing changes" case.
// A multi-statement body ships the SET prelude plus the statement under the
// caret — the prelude rides along unchanged because its `SET param_*` bindings
// are what make any of the statements executable, and scoping those per
// statement is deferred (ADR-0130 §Updates 2026-07-25).
func RunBufferFor(sql string, caret int) (run string, number, total int) {
	ranges, bodyOffset := BodyStatementRanges(sql)
	return ComposeRunBuffer(sql, ranges, bodyOffset, caret)
}

// ComposeRunBuffer is [RunBufferFor] against an already-split buffer, so the
// per-frame caller can compose from the memo rather than re-splitting.
func ComposeRunBuffer(sql string, ranges []StatementRange, bodyOffset, caret int) (run string, number, total int) {
	trimmed := strings.TrimSpace(sql)
	total = len(ranges)
	if total <= 1 {
		return trimmed, total, total
	}
	i := ActiveStatementIndex(ranges, caret)
	if i < 0 {
		return trimmed, 0, total
	}
	stmt := ranges[i]
	return WithPrelude(sql, bodyOffset, sql[stmt.Src.Start:stmt.Src.End]), i + 1, total
}

// WithPrelude puts a statement back behind the buffer's SET prelude. The
// prelude rides along with whatever ships because its `SET param_*` bindings
// are what make any of it executable.
func WithPrelude(sql string, bodyOffset int, body string) string {
	prelude := strings.TrimRight(sql[:bodyOffset], " \t\r\n")
	if prelude == "" {
		return body
	}
	return prelude + "\n" + body
}

// PreludeRange is the SET prelude's extent in sql, trailing whitespace and
// delimiters excluded. Empty when the buffer has no prelude. It is part of what
// a narrowed run carries, so an embedder that tints the carried environment
// reads it from here.
func PreludeRange(sql string, bodyOffset int) (r nanopass.SourceRange) {
	end := len(strings.TrimRight(sql[:bodyOffset], " \t\r\n;"))
	if end <= 0 {
		return
	}
	return nanopass.SourceRange{Start: 0, End: end}
}

// Statements is [BodyStatementRanges] memoised on the buffer it describes.
//
// The split costs a full lex of the body — ~26 µs at 180 B and ~280 µs at
// 2.5 KB, roughly linear (ADR-0130's measurements) — and its consumers run per
// frame. Without the memo a 25 KB buffer would spend milliseconds per frame
// re-deriving a value that only changes when the buffer does. The caret moving
// does not invalidate it; only an edit does, which is the same key the colour
// job uses.
//
// Exported because an embedder's own caret-derived helpers should share this
// memo rather than keep a second one: two memos over one pure function is not
// a correctness risk, but it is two things to invalidate and one of them will
// eventually be missed. Callers must not mutate the returned slice.
func (inst *Editor) Statements(sql string) (ranges []StatementRange, bodyOffset int) {
	if inst.stmtOk && inst.stmtFor == sql {
		return inst.stmtRanges, inst.stmtOffset
	}
	inst.stmtRanges, inst.stmtOffset = BodyStatementRanges(sql)
	inst.stmtFor = sql
	inst.stmtOk = true
	return inst.stmtRanges, inst.stmtOffset
}
