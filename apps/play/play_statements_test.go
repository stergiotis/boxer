package play

// The lex-tier statement splitter and the caret's statement (ADR-0130 L3).

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stretchr/testify/require"
)

// texts renders the ranges as the substrings they cover, which is what the
// assertions actually care about.
func stmtTexts(sql string) (out []string) {
	ranges, _ := bodyStatementRanges(sql)
	for _, r := range ranges {
		out = append(out, sql[r.Src.Start:r.Src.End])
	}
	return
}

func TestBodyStatementRanges(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"single statement", "SELECT 1", []string{"SELECT 1"}},
		{"single with trailing semicolon", "SELECT 1;", []string{"SELECT 1"}},
		{"two statements", "SELECT 1; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"two, both terminated", "SELECT 1;\nSELECT 2;\n", []string{"SELECT 1", "SELECT 2"}},
		// The lexer owns strings and comments, so a `;` inside either can
		// never split — the property that makes a lex-tier split safe.
		{"semicolon in a string", "SELECT 'a;b' FROM t", []string{"SELECT 'a;b' FROM t"}},
		{"semicolon in a line comment", "SELECT 1 -- a; b\n", []string{"SELECT 1"}},
		{"semicolon in a block comment", "SELECT /* a; b */ 1", []string{"SELECT /* a; b */ 1"}},
		// Delimiter-only and comment-only segments carry nothing to run.
		{"empty segments", "SELECT 1;;;SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"leading semicolon", ";SELECT 1", []string{"SELECT 1"}},
		{"trailing comment only", "SELECT 1; -- done\n", []string{"SELECT 1"}},
		{"empty buffer", "", nil},
		{"whitespace only", "  \n\t ", nil},
		{"semicolons only", ";;;", nil},
		// The SET prelude is not a statement: it is authored by the parameter
		// widgets, and counting it would make every parameterised buffer look
		// multi-statement.
		{"prelude is skipped", "SET param_a = 1;\nSELECT {a:UInt64}", []string{"SELECT {a:UInt64}"}},
		{"prelude plus two", "SET param_a = 1;\nSELECT 1; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		// A broken sibling must not stop the healthy one from being found —
		// the whole reason the split is lexical.
		{"broken sibling", "SELCT 1; SELECT 2", []string{"SELCT 1", "SELECT 2"}},
		{"unterminated string tail", "SELECT 1; SELECT 'oops", []string{"SELECT 1", "SELECT 'oops"}},
		// Comments and whitespace are trimmed off the extent so the tint sits
		// on the statement, not on the gap around it.
		{"extent excludes surrounding trivia", "SELECT 1 ;   \n  SELECT 2  ",
			[]string{"SELECT 1", "SELECT 2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, stmtTexts(tc.sql))
		})
	}
}

// The boundary rule mirrored from play.html's getQueryUnderCursor: the first
// statement whose `;` ends at or after the caret wins.
func TestActiveStatementBoundaries(t *testing.T) {
	//        0         1         2
	//        0123456789012345678901
	sql := "SELECT 1; SELECT 22"
	cases := []struct {
		name  string
		caret int
		want  string
	}{
		{"start of buffer", 0, "SELECT 1"},
		{"inside the first", 4, "SELECT 1"},
		{"on the semicolon", 8, "SELECT 1"},
		{"just past the semicolon", 9, "SELECT 1"},
		{"in the gap after it", 10, "SELECT 22"},
		{"inside the second", 15, "SELECT 22"},
		{"at the end", len(sql), "SELECT 22"},
		// A caret past the end (stale by one frame after a deletion) still
		// resolves rather than falling off.
		{"past the end", len(sql) + 50, "SELECT 22"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stmt, _, total, ok := activeStatement(sql, tc.caret)
			require.True(t, ok)
			require.Equal(t, 2, total)
			require.Equal(t, tc.want, sql[stmt.Src.Start:stmt.Src.End])
		})
	}
}

// Cursor past the last `;` with only trivia after it falls back to the last
// statement instead of resolving to nothing.
func TestActiveStatementFallsBackPastTheLastDelimiter(t *testing.T) {
	sql := "SELECT 1; SELECT 2;   \n  -- done\n"
	stmt, index, total, ok := activeStatement(sql, len(sql))
	require.True(t, ok)
	require.Equal(t, 2, total)
	require.Equal(t, 1, index)
	require.Equal(t, "SELECT 2", sql[stmt.Src.Start:stmt.Src.End])
}

// A caret sitting in the SET prelude resolves to the first body statement.
func TestActiveStatementWithCaretInPrelude(t *testing.T) {
	sql := "SET param_a = 1;\nSELECT 1; SELECT 2"
	stmt, index, total, ok := activeStatement(sql, 4)
	require.True(t, ok)
	require.Equal(t, 2, total)
	require.Equal(t, 0, index)
	require.Equal(t, "SELECT 1", sql[stmt.Src.Start:stmt.Src.End])
}

func TestActiveStatementDegenerate(t *testing.T) {
	_, _, total, ok := activeStatement("", 0)
	require.False(t, ok)
	require.Equal(t, 0, total)
	_, _, _, ok = activeStatement("  \n ", 2)
	require.False(t, ok)

	// A single statement reports total == 1, which is what suppresses both
	// the tint and run-under-cursor.
	stmt, index, total, ok := activeStatement("SELECT 1", 3)
	require.True(t, ok)
	require.Equal(t, 1, total)
	require.Equal(t, 0, index)
	require.Equal(t, "SELECT 1", "SELECT 1"[stmt.Src.Start:stmt.Src.End])
}

// --- run-under-cursor (ADR-0130 L3, M6) ---

// A single-statement buffer is byte-identical to what shipped before this
// existed — the "nothing changes" guarantee.
func TestRunBufferSingleStatementIsUnchanged(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1",
		"  SELECT 1  ",
		"SELECT 1;",
		"SET param_a = 1;\nSELECT {a:UInt64}",
		"SELECT 'a;b' FROM t",
		"", // empty buffer
	} {
		for _, caret := range []int{0, 3, len(sql)} {
			run, _, total := runBufferFor(sql, caret)
			require.LessOrEqual(t, total, 1, "buffer %q must read as one statement", sql)
			require.Equal(t, strings.TrimSpace(sql), run,
				"a single-statement buffer ships verbatim (caret=%d)", caret)
		}
	}
}

// The prelude rides along with whichever statement the caret picks.
func TestRunBufferShipsPreludePlusActiveStatement(t *testing.T) {
	sql := "SET param_a = 1;\nSELECT 1; SELECT {a:UInt64}"
	run, number, total := runBufferFor(sql, 20) // caret inside "SELECT 1"
	require.Equal(t, 2, total)
	require.Equal(t, 1, number)
	require.Equal(t, "SET param_a = 1;\nSELECT 1", run)

	run, number, total = runBufferFor(sql, len(sql)) // caret in the second
	require.Equal(t, 2, total)
	require.Equal(t, 2, number)
	require.Equal(t, "SET param_a = 1;\nSELECT {a:UInt64}", run)
}

// A broken sibling does not stop the healthy statement under the caret from
// running — the reason the split is lexical rather than CST-based.
func TestRunBufferRunsTheHealthyStatementBesideABrokenOne(t *testing.T) {
	sql := "SELCT 1; SELECT 2"
	run, number, total := runBufferFor(sql, len(sql))
	require.Equal(t, 2, total)
	require.Equal(t, 2, number)
	require.Equal(t, "SELECT 2", run)
	// …and the broken one still ships when the caret is in it, so the server
	// reports the error the user is looking at.
	run, number, _ = runBufferFor(sql, 2)
	require.Equal(t, 1, number)
	require.Equal(t, "SELCT 1", run)
}

// No prelude: the composed buffer must not gain leading whitespace.
func TestRunBufferWithoutPrelude(t *testing.T) {
	sql := "SELECT 1;\nSELECT 2"
	run, _, _ := runBufferFor(sql, 0)
	require.Equal(t, "SELECT 1", run)
}

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
		run, _, _ := runBufferFor(sql, 20)
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

			wantStmt, wantIdx, wantTotal, wantOk := activeStatement(sql, caret)
			gotStmt, gotIdx, gotTotal, gotOk := app.caretStatement()
			require.Equal(t, wantOk, gotOk, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantIdx, gotIdx, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantTotal, gotTotal, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantStmt, gotStmt, "sql=%q caret=%d", sql, caret)

			wantRun, wantNum, wantTot := runBufferFor(sql, caret)
			gotRun, gotNum, gotTot := app.runBuffer()
			require.Equal(t, wantRun, gotRun, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantNum, gotNum, "sql=%q caret=%d", sql, caret)
			require.Equal(t, wantTot, gotTot, "sql=%q caret=%d", sql, caret)
		}
	}
}
