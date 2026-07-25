package play

// Blast radius of the ADR-0130 L3 hygiene fix: updatePreview parses the
// UNTRIMMED buffer (`raw := inst.sql`) and rebases observations out of body
// space with env.BodyOffset. play_editor_spans_test.go pins the span
// consumers; this file pins everything ELSE that receives `raw` — the
// Diagnostics caches, the formattedFor readers, and the run path's still-trimmed
// witness — so a future re-trim breaks a test rather than a pane.
//
// The two coordinate domains are deliberate and must not be crossed:
//
//	preview domain (UNTRIMMED): inst.sql, formattedFor, sigTypesFor, wireFor,
//	    observation Src, param-slot Src, formattedErr's line/column.
//	run domain (TRIMMED): executeRun's sql, lastSentSql, the staleness witness,
//	    shouldAutoRun.
//
// Nothing compares a value from one against a value from the other; the tests
// below are what says so.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Poll bounds for the one cache below that computes off the render thread.
const (
	blastWaitFor  = 2 * time.Second
	blastWaitTick = 5 * time.Millisecond
)

// newResidualDiagDriver is newTestDiagDriver with the real residual builder's
// shape: Client.buildResidual harvests the SET prelude via ExtractParams and
// falls back to the buffer VERBATIM when that parse fails. The fallback is the
// probe's normal case — the probe only arms for buffers boxer's grammar
// rejected, which is exactly when ExtractParams' own parse fails too.
func newResidualDiagDriver(exec nodeExecutorI) *DiagnosticsDriver {
	return &DiagnosticsDriver{
		lane: newNodeLane(exec, memory.NewGoAllocator(), 0),
		buildResidual: func(s string) (string, map[string]string) {
			residual, params, err := ExtractParams(s)
			if err != nil {
				return s, nil
			}
			return residual, params
		},
	}
}

// Item 1: the EXPLAIN probe under an untrimmed buffer.
func TestProbeArmingUnderUntrimmedBuffer(t *testing.T) {
	gErr := errors.New("grammar1: no viable alternative")
	const raw = "\n\n  SHOW TABLES\n"

	t.Run("the probe body is the buffer verbatim and still valid SQL", func(t *testing.T) {
		exec := &diagMockExecutor{}
		d := newResidualDiagDriver(exec)
		defer d.close()

		d.noteParse(raw, gErr)
		waitVerdict(t, d, probeAccepted)
		require.NotEmpty(t, exec.sqls)
		// Leading whitespace lands AFTER the prefix's newline, so it is
		// whitespace inside a statement — it can never fuse with the prefix
		// or split a token.
		assert.Equal(t, diagProbePrefix+raw, exec.sqls[0].SQL)
		assert.True(t, strings.HasSuffix(diagProbePrefix, "\n"))
	})

	t.Run("the probe body preserves the editor's line numbering", func(t *testing.T) {
		// adjustProbeLineNumbers subtracts exactly the one line the prefix
		// occupies. That is only exact if the probed body is the buffer
		// verbatim — trimming the leading blank lines away (the old
		// behaviour) left every corrected position short by their count.
		require.Equal(t, 1, strings.Count(diagProbePrefix, "\n"))
		exec := &diagMockExecutor{}
		d := newResidualDiagDriver(exec)
		defer d.close()
		d.noteParse(raw, gErr)
		body := strings.TrimPrefix(d.probeNode.SQL, diagProbePrefix)
		require.Equal(t, raw, body)
		// "SHOW" sits on buffer line 3, hence probe line 4; the correction
		// takes it back to 3 — the line the editor's gutter shows.
		assert.Equal(t, "(line 3, col 3)", adjustProbeLineNumbers("(line 4, col 3)"))
	})

	t.Run("a settled buffer arms once, not once per frame", func(t *testing.T) {
		exec := &diagMockExecutor{}
		d := newResidualDiagDriver(exec)
		defer d.close()

		d.noteParse(raw, gErr)
		waitVerdict(t, d, probeAccepted)
		// probeFor is only ever assigned from noteParse's own argument, so it
		// cannot mismatch a trimmed value produced elsewhere. Re-noting the
		// same buffer is a no-op and the lane memo-hits.
		for range 5 {
			d.noteParse(raw, gErr)
			v, _ := d.probeView()
			require.Equal(t, probeAccepted, v)
		}
		assert.EqualValues(t, 1, exec.calls.Load(), "no re-arm, no extra round trip")
	})

	t.Run("a whitespace-only edit costs one redundant probe, not a wrong verdict", func(t *testing.T) {
		exec := &diagMockExecutor{}
		d := newResidualDiagDriver(exec)
		defer d.close()

		d.noteParse(strings.TrimSpace(raw), gErr)
		waitVerdict(t, d, probeAccepted)
		require.EqualValues(t, 1, exec.calls.Load())
		// The key now differs by the whitespace, so the memo misses once.
		d.noteParse(raw, gErr)
		waitVerdict(t, d, probeAccepted)
		assert.EqualValues(t, 2, exec.calls.Load(),
			"one extra EXPLAIN AST per whitespace-only edit of a grammar-rejected buffer")
	})
}

// Item 1 (continued): the two synchronous/goroutine caches noteParse also
// drives. Both key on their own copy of `raw` and nothing else, so untrimming
// the key can only cost a recompute.
func TestNoteParseCachesAreContentKeyed(t *testing.T) {
	const flush = "SELECT id FROM users"
	const padded = "\n\n  SELECT id FROM users\n"

	t.Run("column diagnostics recompute but do not change", func(t *testing.T) {
		var calls int
		var seen []string
		d := &DiagnosticsDriver{
			resolveDiag: func(sql string) []passes.ColumnDiagnostic {
				calls++
				seen = append(seen, sql)
				return []passes.ColumnDiagnostic{{Handle: "s:c", Message: "unknown section"}}
			},
		}
		// armColumnDiag runs the resolver on a goroutine; the driver has no
		// lane, so drive it directly and wait on the memo instead.
		d.armColumnDiag(flush, nil)
		require.Eventually(t, func() bool { return len(d.columnDiagnostics()) == 1 }, blastWaitFor, blastWaitTick)
		require.Equal(t, 1, calls)

		d.armColumnDiag(flush, nil) // same buffer: pure cache hit
		assert.Equal(t, 1, calls)

		d.armColumnDiag(padded, nil)
		require.Eventually(t, func() bool { return len(d.columnDiagnostics()) == 1 }, blastWaitFor, blastWaitTick)
		assert.Equal(t, 2, calls, "a whitespace-only key change costs one recompute")
		assert.Equal(t, []string{flush, padded}, seen)
		// …and the resolver's answer is whitespace-insensitive, so the pane
		// renders the same warnings either way.
		assert.Equal(t, "s:c", d.columnDiagnostics()[0].Handle)
	})

	t.Run("the security lenses reach the same verdict either way", func(t *testing.T) {
		read := func(sql string) (analysis.QuerySecurityClassE, bool, []string) {
			d := &DiagnosticsDriver{}
			d.noteParse(sql, nil)
			var names []string
			for _, tbl := range d.securityContext() {
				names = append(names, passthroughTableName(tbl))
			}
			class, _, known := d.securityClass()
			return class, known, names
		}
		classA, knownA, tablesA := read(flush)
		classB, knownB, tablesB := read(padded)
		require.True(t, knownA)
		assert.Equal(t, classA, classB)
		assert.Equal(t, knownA, knownB)
		assert.Equal(t, tablesA, tablesB)
		assert.Equal(t, []string{"users"}, tablesA)
	})
}

// Item 2: formattedFor is assigned inst.sql — as it already was before the
// change — and every reader compares it against inst.sql or against a field
// only ever assigned from it. signalTypeTable's local TrimSpace is a parse
// input, not a key, and what it produces (name → declared types) carries no
// byte offsets, so the trim cannot skew anything.
func TestSignalTypeTableIsWhitespaceAgnostic(t *testing.T) {
	mk := func(sql string) *PlayApp {
		app := &PlayApp{sql: sql, formattedFor: sql}
		return app
	}
	flush := mk("SELECT {a:UInt64}, {b:String}")
	padded := mk("\n\n   SELECT {a:UInt64}, {b:String}\n")
	require.Equal(t, flush.signalTypeTable(), padded.signalTypeTable())
	require.Equal(t, map[string][]string{"a": {"UInt64"}, "b": {"String"}}, flush.signalTypeTable())

	// The memo keys on formattedFor and hits on the next call — a sentinel
	// survives, proving the second call never re-parsed.
	sentinel := map[string][]string{"sentinel": {"X"}}
	padded.sigTypes = sentinel
	assert.Equal(t, sentinel, padded.signalTypeTable())

	// An edit that only adds whitespace still invalidates it (formattedFor
	// moved), which is the honest answer: the spans it sits beside moved too.
	padded.sql += " "
	padded.formattedFor = padded.sql
	assert.NotEqual(t, sentinel, padded.signalTypeTable())
}

// Item 3: the trimmed run domain and the untrimmed preview domain never meet.
// A whitespace-only edit moves the preview witness and deliberately not the
// run one; the converse cannot happen, because TrimSpace differing implies the
// buffers differ.
func TestWhitespaceEditMovesThePreviewNotTheRun(t *testing.T) {
	app := debouncedApp(t, "SELECT 1")
	app.updatePreview()
	require.Equal(t, app.sql, app.formattedFor)
	app.lastSentSql = strings.TrimSpace(app.sql) // as executeRun records it

	app.sql += "\n  " // the user presses Return at the end of the buffer

	// Run domain: unmoved. Not stale, still auto-run eligible, and what a Run
	// would ship is byte-identical.
	assert.Equal(t, app.lastSentSql, strings.TrimSpace(app.sql))
	assert.Equal(t, queryStateRows, app.observeQueryState(false, 3, time.Unix(1_700_000_000, 0), nil),
		"a whitespace-only edit must not mark the result stale")
	runSQL, _, total := runBufferFor(app.sql, app.caretByte)
	assert.Equal(t, app.lastSentSql, runSQL)
	assert.Equal(t, 1, total)

	// Preview domain: moved, so the pipeline re-derives its byte ranges
	// against the buffer that is actually on screen.
	assert.NotEqual(t, app.sql, app.formattedFor)
	app.lastSeenSql = app.sql // the debounce window has elapsed
	app.lastEditAt = app.lastEditAt.Add(-2 * previewDebounce)
	app.updatePreview()
	assert.Equal(t, app.sql, app.formattedFor)

	// A substantive edit moves BOTH witnesses: there is no state in which the
	// run path sees a changed buffer while the preview path does not. The
	// asymmetry only runs the other way (whitespace above), which is the safe
	// direction — the preview re-derives spans it did not need to.
	app.sql = "SELECT 2"
	assert.NotEqual(t, app.lastSentSql, strings.TrimSpace(app.sql), "the run witness moved")
	assert.NotEqual(t, app.formattedFor, app.sql, "…and the preview witness with it")
}

// Item 4: the body→buffer rebase happens exactly once per pipeline run.
func TestObservationSrcIsNotDoubleShifted(t *testing.T) {
	const call = `multiMatchAnyIndex(text, ['foo.*'])`
	app := debouncedApp(t, "SET param_lim = 10;\n\n  SELECT "+call+" FROM t")
	app.updatePreview()
	require.Len(t, app.observations, 1)
	first := app.observations[0].Src
	require.Equal(t, call, app.sql[first.Start:first.End])

	// A second call is a no-op (formattedFor == sql): it must not shift again.
	app.updatePreview()
	require.Len(t, app.observations, 1)
	assert.Equal(t, first, app.observations[0].Src)

	// A re-run against a longer prelude re-derives the offset from scratch
	// rather than accumulating it.
	app.sql = "SET param_lim = 10;\nSET max_threads = 4;\n\n\n    SELECT " + call + " FROM t"
	app.lastSeenSql = app.sql
	app.lastEditAt = app.lastEditAt.Add(-2 * previewDebounce)
	app.updatePreview()
	require.Len(t, app.observations, 1)
	second := app.observations[0].Src
	assert.Equal(t, call, app.sql[second.Start:second.End])
	assert.Greater(t, second.Start, first.Start, "the longer prelude pushed it right")
}

// Item 4 (continued): formattedErr's line/column are the other range the
// untrimmed parse fixes. The error underline maps them into inst.sql, so a
// trimmed parse silently underlined the wrong token.
func TestErrorUnderlineIsInBufferCoordinates(t *testing.T) {
	const stmt = "SELECT 1 FROM t WHERE x = = 2" // the SECOND '=' is the error
	const lead = "\n\n"
	app := debouncedApp(t, lead+stmt)
	app.updatePreview()
	require.Error(t, app.formattedErr)
	require.Equal(t, app.sql, app.formattedFor, "quiescent, so the overlays render")

	secs := app.editorStyledSections()
	require.Len(t, secs, 1, "one error underline, no tint (single statement) and no slots")
	got := app.sql[secs[0].Start:secs[0].Stop]
	assert.Equal(t, "=", got)
	assert.Equal(t, strings.LastIndex(app.sql, "="), int(secs[0].Start))

	// What the trimmed contract produced: the position was measured on the
	// trimmed copy and then sliced against the untrimmed buffer, so line 1 of
	// the buffer was blank and the lookup fell forward onto the first token.
	stale := firstSyntaxError(stmt)
	start, stop, ok := errorTokenSpan(app.sql, stale)
	require.True(t, ok)
	assert.Equal(t, "SELECT", app.sql[start:stop],
		"the pre-fix behaviour underlined the wrong token — this is what the change removes")
}
