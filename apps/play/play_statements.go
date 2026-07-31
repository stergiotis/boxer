package play

// Multi-statement buffers, play's half.
//
// The splitting, the caret-to-statement boundary rule and the run-buffer
// composition moved to widgets/sqleditor with the editor (ADR-0147 §SD1): all
// of it is pure over (buffer, caret), which is the test SD2 sets for what the
// widget owns. What is left here reads the editor's published Result, plus the
// two things that genuinely need play's model — subquery narrowing, which
// wants a nanopass parse of the statement, and the run-scope reporting the
// status line does.

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// statementRange is the editor's range type under play's older name, so the
// consumers that only ever read `.Src` did not have to change with the move.
type statementRange = sqleditor.StatementRange

// statementRanges is the split of the current buffer, served from the
// editor's memo rather than a second one of play's own: two memos over one
// pure function is not a correctness risk, but it is two things to invalidate
// and one of them would eventually be missed.
//
// These helpers derive from inst.caretByte rather than from the editor's
// published Result, and that is deliberate. Result describes the frame the
// editor last rendered, while several consumers here — the param pane, the Run
// gate, updatePreview — run BEFORE the editor's turn in the frame. Reading the
// live buffer plus the caret field keeps them describing the buffer they are
// actually looking at.
func (inst *PlayApp) statementRanges() (ranges []statementRange, bodyOffset int) {
	return inst.editor.Statements(inst.sql)
}

// caretStatement is the statement the caret is in, with its 0-based index, the
// statement count, and whether one resolved at all.
func (inst *PlayApp) caretStatement() (stmt statementRange, index, total int, ok bool) {
	ranges, _ := inst.statementRanges()
	return sqleditor.SelectStatement(ranges, inst.caretByte)
}

// runBuffer is what an ordinary Run ships, with the 1-based statement number
// and the statement count.
func (inst *PlayApp) runBuffer() (run string, number, total int) {
	ranges, bodyOffset := inst.statementRanges()
	return sqleditor.ComposeRunBuffer(inst.sql, ranges, bodyOffset, inst.caretByte)
}

// preludeRange is the SET prelude's extent in inst.sql, trailing whitespace
// excluded. Empty when the buffer has no prelude. It is part of what a narrowed
// run carries, so the editor tints it with the WITH items.
func (inst *PlayApp) preludeRange() nanopass.SourceRange {
	_, bodyOffset := inst.statementRanges()
	return sqleditor.PreludeRange(inst.sql, bodyOffset)
}

// statementSubqueries is parseSubqueryUnits memoised on the statement text —
// the same one-entry shape, and for the same reason, as statementSyntaxError:
// the caret is inside one statement at a time, so the parse runs when that
// statement's text changes or the caret leaves it, not once per frame.
//
// This did not move with the editor: it needs a nanopass parse of the
// statement, which is a subsystem rather than a buffer-and-caret derivation.
// The editor is told the resulting range through Decoration.SubqueryMark.
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
	return sqleditor.WithPrelude(inst.sql, bodyOffset, unit.compose(text)), runScopeSubquery
}
