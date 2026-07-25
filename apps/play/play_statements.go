package play

// Multi-statement buffers (ADR-0130 L3): splitting the editor buffer into
// statements and deciding which one the caret is in.
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

// statementRange is one statement of a multi-statement buffer.
type statementRange struct {
	// Src is the statement's significant extent: first significant token
	// through last, with the terminating `;` and any surrounding whitespace
	// or comments excluded. This is what gets tinted and what ships.
	Src nanopass.SourceRange
	// delimEnd is the byte just past the segment's terminating `;`, or the
	// buffer end for an unterminated trailing statement. Caret selection
	// compares against this rather than against Src.End — see
	// activeStatementIndex.
	delimEnd int
}

// bodyStatementRanges splits the prelude-stripped body of sql into statements,
// in sql's own byte coordinates.
//
// The prelude is skipped deliberately: `SET param_x = 1;` lines are authored by
// the parameter widgets, not typed as statements, and counting them would make
// every parameterised single-statement buffer look multi-statement. bodyOffset
// is where the body starts, so callers can compose prelude + statement.
//
// Segments with no significant content — a stray `;`, a trailing comment — are
// dropped rather than returned as empty ranges.
func bodyStatementRanges(sql string) (ranges []statementRange, bodyOffset int) {
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
				ranges = append(ranges, statementRange{
					Src:      nanopass.SourceRange{Start: bodyOffset + firstSig, End: bodyOffset + lastSig},
					delimEnd: bodyOffset + s.Stop,
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
		ranges = append(ranges, statementRange{
			Src:      nanopass.SourceRange{Start: bodyOffset + firstSig, End: bodyOffset + lastSig},
			delimEnd: len(sql),
		})
	}
	return ranges, bodyOffset
}

// activeStatementIndex returns the index of the statement the caret is in, or
// -1 when there is none.
//
// The boundary rule is ClickHouse play.html's `getQueryUnderCursor`, read as
// served (ClickHouse 26.6.1) and mirrored: the winner is the FIRST statement
// whose terminating `;` ends at or after the caret. So a caret resting exactly
// on the byte after a `;` still belongs to the statement it closes, while a
// caret anywhere further into the gap already belongs to the next one. A caret
// past the last `;` with only whitespace or comments after it falls back to the
// last statement rather than resolving to nothing.
func activeStatementIndex(ranges []statementRange, caret int) int {
	if len(ranges) == 0 {
		return -1
	}
	for i, r := range ranges {
		if r.delimEnd >= caret {
			return i
		}
	}
	return len(ranges) - 1
}

// activeStatement resolves the caret to a statement of sql, reporting how many
// statements the body holds. total > 1 is the multi-statement condition every
// L3 consumer gates on: the tint renders, and Run ships one statement instead
// of the buffer.
func activeStatement(sql string, caret int) (stmt statementRange, index, total int, ok bool) {
	ranges, _ := bodyStatementRanges(sql)
	total = len(ranges)
	index = activeStatementIndex(ranges, caret)
	if index < 0 {
		return stmt, -1, total, false
	}
	return ranges[index], index, total, true
}

// runBufferFor returns what a Run ships for buffer sql with the caret at
// `caret`, plus the 1-based statement number and the statement count.
//
// A single-statement body ships the whole trimmed buffer, byte-identical to
// what shipped before this existed — total <= 1 is the "nothing changes" path.
// A multi-statement body ships the SET prelude plus the statement under the
// caret: the prelude rides along unchanged because its `SET param_*` bindings
// are what make any of the statements executable, and scoping those per
// statement is deferred (ADR-0130 §Updates 2026-07-25).
func runBufferFor(sql string, caret int) (run string, number, total int) {
	trimmed := strings.TrimSpace(sql)
	ranges, bodyOffset := bodyStatementRanges(sql)
	total = len(ranges)
	if total <= 1 {
		return trimmed, total, total
	}
	i := activeStatementIndex(ranges, caret)
	if i < 0 {
		return trimmed, 0, total
	}
	stmt := ranges[i]
	body := sql[stmt.Src.Start:stmt.Src.End]
	prelude := strings.TrimRight(sql[:bodyOffset], " \t\r\n")
	if prelude == "" {
		return body, i + 1, total
	}
	return prelude + "\n" + body, i + 1, total
}

// runBuffer is runBufferFor against the app's current buffer and caret.
func (inst *PlayApp) runBuffer() (run string, number, total int) {
	return runBufferFor(inst.sql, inst.caretByte)
}
