package play

// What run-under-cursor means for the REST of play's model, and the agreement
// between play's memoised helpers and the widget's pure ones (ADR-0147 §SD1
// moved the splitter itself to widgets/sqleditor).

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
	"github.com/stretchr/testify/require"
)

// Run-under-cursor deliberately leaves the rest of the buffer's semantics
// alone in this first cut (ADR-0130 §Updates 2026-07-25). These assert the
// choices so a later widening is a deliberate act, not a drift.
func TestRunUnderCursorLeavesBufferWideSemanticsAlone(t *testing.T) {
	const sql = "SET param_a = 1;\nSELECT {a:UInt64}; SELECT {b:UInt64}"
	app := debouncedApp(t, sql)
	app.updatePreview()

	t.Run("param extraction is still whole-buffer", func(t *testing.T) {
		// grammar1's QueryStmt is single-statement, so a multi-statement
		// buffer has never yielded param slots and still does not. Scoping
		// extraction per statement is the deferred half of run-under-cursor
		// (ADR-0130 §Updates 2026-07-25); asserted so widening it is a
		// deliberate act rather than a drift.
		require.Empty(t, app.paramSlots)
		app.caretByte = 20
		require.Empty(t, app.unfilledInputs())
	})

	t.Run("signals keep their buffer-wide scope", func(t *testing.T) {
		// On a buffer the grammar does parse, both slots resolve regardless of
		// where the caret sits — the Run ships one statement, but the inputs
		// it resolves are the buffer's.
		one := debouncedApp(t, "SELECT {a:UInt64} + {b:UInt64}")
		one.updatePreview()
		require.ElementsMatch(t, []string{"a", "b"}, one.unfilledInputs())
		one.caretByte = 3
		require.ElementsMatch(t, []string{"a", "b"}, one.unfilledInputs())
	})

	t.Run("a caret move does not flip the staleness witness", func(t *testing.T) {
		app.lastSentSql = strings.TrimSpace(sql)
		app.caretByte = 20
		require.Equal(t, strings.TrimSpace(app.sql), app.lastSentSql)
		app.caretByte = len(sql) // moved to the other statement
		require.Equal(t, strings.TrimSpace(app.sql), app.lastSentSql,
			"the witness keys on the buffer, so a caret move cannot make it stale")
	})

	t.Run("history restores the whole buffer", func(t *testing.T) {
		run, _, _ := sqleditor.RunBufferFor(sql, 20)
		require.NotEqual(t, strings.TrimSpace(sql), run, "the run is a fragment…")
		entry := HistoryEntry{SQL: run, Buffer: sql}
		restored := &PlayApp{graph: newLiveQueryGraph(nil, memory.NewGoAllocator(), 10)}
		restored.restoreHistoryEntry(entry)
		require.Equal(t, sql, restored.sql, "…but restoring gives the buffer back")
	})

	t.Run("a single-statement run records no separate buffer", func(t *testing.T) {
		one := debouncedApp(t, "SELECT 1")
		runSQL, _, _ := one.runBuffer()
		require.Equal(t, "SELECT 1", runSQL)
		// executeRun passes "" in this case, and an empty Buffer restores SQL.
		restored := &PlayApp{graph: newLiveQueryGraph(nil, memory.NewGoAllocator(), 10)}
		restored.restoreHistoryEntry(HistoryEntry{SQL: "SELECT 1"})
		require.Equal(t, "SELECT 1", restored.sql)
	})
}

// --- the statement-split memo ---

// The split depends on the buffer alone; the caret only picks among the
// ranges. So caret travel must not re-derive it — the two consumers both run
// per frame, and the split costs a full lex of the body.
func TestStatementRangesMemo(t *testing.T) {
	app := debouncedApp(t, "SELECT 1; SELECT 2")
	first, off := app.statementRanges()
	require.Len(t, first, 2)
	require.Equal(t, 0, off)

	// Same buffer, moved caret: the identical backing array comes back, which
	// is what says no lex ran.
	app.caretByte = 15
	again, _ := app.statementRanges()
	require.Same(t, &first[0], &again[0], "a caret move must not re-split")

	// An edit invalidates it — the same key the colour job uses.
	app.sql = "SELECT 1; SELECT 2; SELECT 3"
	third, _ := app.statementRanges()
	require.Len(t, third, 3)
	require.NotSame(t, &first[0], &third[0], "an edit must re-split")
}

// The memoised path and the pure path must agree, or the per-frame consumers
// would drift from what the tests pin.
func TestMemoisedPathMatchesThePureOne(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1",
		"SELECT 1; SELECT 2",
		"SET param_a = 1;\nSELECT 1; SELECT {a:UInt64}",
		"SELCT 1; SELECT 2",
		"",
		"   ",
	} {
		app := debouncedApp(t, sql)
		for _, caret := range []int{0, 5, len(sql), len(sql) + 40} {
			app.caretByte = caret

			wantStmt, wantIdx, wantTotal, wantOk := sqleditor.ActiveStatement(sql, caret)
			gotStmt, gotIdx, gotTotal, gotOk := app.caretStatement()
			require.Equal(t, wantOk, gotOk, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantIdx, gotIdx, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantTotal, gotTotal, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantStmt, gotStmt, "sql=%q caret=%d", sql, caret)

			wantRun, wantNum, wantTot := sqleditor.RunBufferFor(sql, caret)
			gotRun, gotNum, gotTot := app.runBuffer()
			require.Equal(t, wantRun, gotRun, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantNum, gotNum, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantTot, gotTot, "sql=%q caret=%d", sql, caret)
		}
	}
}
