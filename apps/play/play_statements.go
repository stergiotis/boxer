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

// selectStatement resolves the caret against an already-split buffer. Split
// out from activeStatement so the per-frame callers can run it against the
// memo instead of re-splitting: the ranges depend on the buffer alone, the
// caret only picks among them.
func selectStatement(ranges []statementRange, caret int) (stmt statementRange, index, total int, ok bool) {
	total = len(ranges)
	index = activeStatementIndex(ranges, caret)
	if index < 0 {
		return stmt, -1, total, false
	}
	return ranges[index], index, total, true
}

// activeStatement resolves the caret to a statement of sql, reporting how many
// statements the body holds. total > 1 is the multi-statement condition every
// L3 consumer gates on: the tint renders, and Run ships one statement instead
// of the buffer.
func activeStatement(sql string, caret int) (stmt statementRange, index, total int, ok bool) {
	ranges, _ := bodyStatementRanges(sql)
	return selectStatement(ranges, caret)
}

// statementRanges is bodyStatementRanges memoised on the buffer it describes.
//
// The split costs a full lex of the body — ~26 µs at 180 B and ~280 µs at
// 2.5 KB, roughly linear (ADR-0130's measurements) — and its two callers both
// run per frame: the styled overlays on every quiescent frame, and the wire
// preview's cache key whenever "as sent" is on. Without the memo a 25 KB
// buffer would spend milliseconds per frame re-deriving a value that only
// changes when the buffer does. The caret moving does not invalidate it; only
// an edit does, which is the same key the colour job uses.
func (inst *PlayApp) statementRanges() (ranges []statementRange, bodyOffset int) {
	if inst.stmtRangesOk && inst.stmtRangesFor == inst.sql {
		return inst.stmtRanges, inst.stmtRangesOffset
	}
	inst.stmtRanges, inst.stmtRangesOffset = bodyStatementRanges(inst.sql)
	inst.stmtRangesFor = inst.sql
	inst.stmtRangesOk = true
	return inst.stmtRanges, inst.stmtRangesOffset
}

// caretStatement is activeStatement against the memo and the app's caret.
func (inst *PlayApp) caretStatement() (stmt statementRange, index, total int, ok bool) {
	ranges, _ := inst.statementRanges()
	return selectStatement(ranges, inst.caretByte)
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
	ranges, bodyOffset := bodyStatementRanges(sql)
	return composeRunBuffer(sql, ranges, bodyOffset, caret)
}

// composeRunBuffer is runBufferFor against an already-split buffer, so the
// per-frame caller can compose from the memo rather than re-splitting.
func composeRunBuffer(sql string, ranges []statementRange, bodyOffset, caret int) (run string, number, total int) {
	trimmed := strings.TrimSpace(sql)
	total = len(ranges)
	if total <= 1 {
		return trimmed, total, total
	}
	i := activeStatementIndex(ranges, caret)
	if i < 0 {
		return trimmed, 0, total
	}
	stmt := ranges[i]
	return withPrelude(sql, bodyOffset, sql[stmt.Src.Start:stmt.Src.End]), i + 1, total
}

// withPrelude puts a statement back behind the buffer's SET prelude. The
// prelude rides along with whatever ships because its `SET param_*` bindings
// are what make any of it executable.
func withPrelude(sql string, bodyOffset int, body string) string {
	prelude := strings.TrimRight(sql[:bodyOffset], " \t\r\n")
	if prelude == "" {
		return body
	}
	return prelude + "\n" + body
}

// runBuffer is runBufferFor against the app's current buffer and caret.
func (inst *PlayApp) runBuffer() (run string, number, total int) {
	ranges, bodyOffset := inst.statementRanges()
	return composeRunBuffer(inst.sql, ranges, bodyOffset, inst.caretByte)
}

// statementSubqueries is parseSubqueryUnits memoised on the statement text —
// the same one-entry shape, and for the same reason, as statementSyntaxError:
// the caret is inside one statement at a time, so the parse runs when that
// statement's text changes or the caret leaves it, not once per frame.
func (inst *PlayApp) statementSubqueries(text string) []subqueryUnit {
	if inst.subqOk && inst.subqFor == text {
		return inst.subqUnits
	}
	inst.subqFor = text
	inst.subqUnits = parseSubqueryUnits(text)
	inst.subqOk = true
	return inst.subqUnits
}

// caretSubquery resolves the caret to the innermost query of its statement,
// reporting the statement it lives in so callers can rebase its offsets.
//
// ok is false when there is no statement, or when the statement does not parse
// — a buffer mid-edit has no CST to narrow within, and the caller falls back
// to shipping the statement whole.
func (inst *PlayApp) caretSubquery() (unit subqueryUnit, stmt statementRange, ok bool) {
	stmt, _, _, haveStmt := inst.caretStatement()
	if !haveStmt {
		return unit, stmt, false
	}
	text := inst.sql[stmt.Src.Start:stmt.Src.End]
	caret := inst.caretByte - stmt.Src.Start
	if caret < 0 {
		caret = 0
	} else if caret > len(text) {
		caret = len(text)
	}
	unit, ok = pickSubquery(inst.statementSubqueries(text), caret)
	return unit, stmt, ok
}

// runScopeE is what a run actually shipped, for the status line. The gesture
// degrades rather than refusing, so the difference between asking for a
// subquery and getting one is not otherwise visible.
type runScopeE uint8

const (
	// runScopeWhole is an ordinary Run: the buffer, or the caret's statement.
	runScopeWhole runScopeE = iota
	// runScopeSubquery is a run-subquery that narrowed.
	runScopeSubquery
	// runScopeNoSubquery is a run-subquery that found nothing narrower and
	// shipped the whole query instead.
	runScopeNoSubquery
)

// runSubqueryBuffer returns what the run-subquery gesture ships: the SET
// prelude plus the innermost query the caret is in, with the WITH items of
// every enclosing scope hoisted in front of it.
//
// scope is runScopeNoSubquery when the caret resolved to the statement's own
// query, or to no query at all because the statement does not parse. run is
// then exactly what runBuffer returns — the gesture degrades to an ordinary Run
// rather than refusing, and the status line says so.
func (inst *PlayApp) runSubqueryBuffer() (run string, scope runScopeE) {
	unit, stmt, ok := inst.caretSubquery()
	if !ok || unit.Root {
		run, _, _ = inst.runBuffer()
		return run, runScopeNoSubquery
	}
	_, bodyOffset := inst.statementRanges()
	text := inst.sql[stmt.Src.Start:stmt.Src.End]
	return withPrelude(inst.sql, bodyOffset, unit.compose(text)), runScopeSubquery
}

// preludeRange is the SET prelude's extent in inst.sql, trailing whitespace
// excluded. Empty when the buffer has no prelude. It is part of what a narrowed
// run carries, so the editor tints it with the WITH items.
func (inst *PlayApp) preludeRange() nanopass.SourceRange {
	_, bodyOffset := inst.statementRanges()
	end := len(strings.TrimRight(inst.sql[:bodyOffset], " \t\r\n;"))
	if end <= 0 {
		return nanopass.SourceRange{}
	}
	return nanopass.SourceRange{Start: 0, End: end}
}
