package play

// Byte-range hygiene for the editor's span consumers (ADR-0130 §Updates
// 2026-07-25, precondition 1). The debounced pipeline parses the UNTRIMMED
// buffer, so every range it records — observation Src, param-slot Src —
// indexes inst.sql directly. Trimming ahead of the parse would skew all of
// them by the leading whitespace, silently mis-slicing the affordance
// arguments and the styled-section spans built on top.

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/require"
)

// debouncedApp builds an app whose editor buffer is sql with the preview
// debounce already elapsed, so a single updatePreview call runs the pipeline.
func debouncedApp(t *testing.T, sql string) *PlayApp {
	t.Helper()
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	app.sql = sql
	app.lastSeenSql = sql
	app.lastEditAt = time.Now().Add(-2 * previewDebounce)
	return app
}

// A buffer with a leading SET prelude or leading whitespace: the
// observation's Src must slice inst.sql to exactly the call text. The
// prelude case is the one that bites in practice — the parameter widgets
// author exactly such a prelude above the user's statement.
func TestObservationSrcIndexesUntrimmedBuffer(t *testing.T) {
	const call = `multiMatchAnyIndex(text, ['foo.*'])`
	leads := []string{
		"",
		" ",
		"\n\n",
		"  \n\t ",
		"SET param_lim = 10;\n",
		"\nSET param_lim = 10;\nSET max_threads = 4;\n\n  ",
	}
	for _, lead := range leads {
		sql := lead + "SELECT " + call + " FROM t"
		app := debouncedApp(t, sql)
		app.updatePreview()

		require.NoError(t, app.formattedErr, "buffer %q must parse", sql)
		require.Len(t, app.observations, 1, "one call site in %q", sql)
		src := app.observations[0].Src
		require.False(t, src.Empty())
		require.GreaterOrEqual(t, src.Start, 0)
		require.LessOrEqual(t, src.End, len(app.sql))
		require.Equal(t, call, app.sql[src.Start:src.End],
			"Src must slice the untrimmed buffer to the call text (lead=%q)", lead)
	}
}

// The affordance's own arg extractor slices with the same range, so it stays
// correct under a prelude — the end-to-end shape of the skew this
// precondition removes.
func TestExtractCallArgsSurvivesLeadingWhitespace(t *testing.T) {
	sql := "SET param_lim = 10;\n\n   SELECT multiMatchAnyIndex(text, ['foo.*', 'bar.*']) FROM t"
	app := debouncedApp(t, sql)
	app.updatePreview()
	require.Len(t, app.observations, 1)

	args := extractCallArgs(app.sql, app.observations[0].Src)
	require.Len(t, args, 2)
	require.Equal(t, "text", strings.TrimSpace(args[0].Text))
	require.False(t, args[0].Literal, "a column ref is not a literal")
}

// Param-slot Src (ADR-0124 §SD1) indexes the same buffer.
func TestParamSlotSrcIndexesUntrimmedBuffer(t *testing.T) {
	sql := "\n  SELECT * FROM t WHERE q = {q:String}"
	slots, _, err := extractSlotsAndParams(sql)
	require.NoError(t, err)
	require.Len(t, slots, 1)
	require.Equal(t, "{q:String}", sql[slots[0].Src.Start:slots[0].Src.End])
}

// A whitespace-only buffer is still treated as empty: no canonical form, no
// error banner, no slots.
func TestWhitespaceOnlyBufferIsEmpty(t *testing.T) {
	app := debouncedApp(t, "   \n\t ")
	app.updatePreview()
	require.Equal(t, "", app.formatted)
	require.NoError(t, app.formattedErr)
	require.Empty(t, app.paramSlots)
}

// The canonical-form preview itself is unaffected: CanonicalizeWhitespace
// trims, so an indented buffer previews identically to a flush one.
func TestCanonicalPreviewUnaffectedByLeadingWhitespace(t *testing.T) {
	flush := debouncedApp(t, "select 1 from t")
	flush.updatePreview()
	indented := debouncedApp(t, "\n\n   select 1 from t")
	indented.updatePreview()
	require.NoError(t, flush.formattedErr)
	require.NoError(t, indented.formattedErr)
	require.Equal(t, flush.formatted, indented.formatted)
}

// The rebase checks its own arithmetic rather than trusting the invariant it
// depends on. That invariant — Extract's body is the suffix of the buffer
// starting at env.BodyOffset — lives in another package, and the way it fails
// here is silent: a range still inside the buffer but pointing at the wrong
// bytes slices plausible text and nothing errors. A range that LEAVES the
// buffer is the observable half, so it is dropped rather than passed on.
func TestShiftObservationsToBufferBoundsCheck(t *testing.T) {
	const buf = "SET param_a = 1;\nSELECT 1"
	const off = 17 // len("SET param_a = 1;\n")
	require.Equal(t, "SELECT 1", buf[off:])

	t.Run("a range that stays inside the buffer shifts", func(t *testing.T) {
		obs := []nanopass.Observation{{Name: "f", Src: nanopass.SourceRange{Start: 0, End: 8}}}
		shiftObservationsToBuffer(obs, buf, off)
		require.Equal(t, nanopass.SourceRange{Start: 17, End: 25}, obs[0].Src)
		require.Equal(t, "SELECT 1", buf[obs[0].Src.Start:obs[0].Src.End])
	})

	t.Run("a range that leaves the buffer is emptied, not shifted", func(t *testing.T) {
		obs := []nanopass.Observation{
			{Name: "ok", Src: nanopass.SourceRange{Start: 0, End: 6}},
			{Name: "past", Src: nanopass.SourceRange{Start: 0, End: 99}},
		}
		shiftObservationsToBuffer(obs, buf, off)
		require.Equal(t, nanopass.SourceRange{Start: 17, End: 23}, obs[0].Src,
			"a sibling in range is unaffected")
		require.True(t, obs[1].Src.Empty(), "the out-of-range one is dropped")
		// Which is exactly what the one real consumer already treats as
		// "no source text".
		require.Nil(t, extractCallArgs(buf, obs[1].Src))
	})

	t.Run("an offset past the buffer drops everything", func(t *testing.T) {
		obs := []nanopass.Observation{{Name: "f", Src: nanopass.SourceRange{Start: 0, End: 8}}}
		shiftObservationsToBuffer(obs, buf, len(buf)+50)
		require.True(t, obs[0].Src.Empty())
	})

	t.Run("a zero offset and empty ranges are untouched", func(t *testing.T) {
		obs := []nanopass.Observation{
			{Name: "f", Src: nanopass.SourceRange{Start: 3, End: 9}},
			{Name: "g"},
		}
		shiftObservationsToBuffer(obs, buf, 0)
		require.Equal(t, nanopass.SourceRange{Start: 3, End: 9}, obs[0].Src)
		require.True(t, obs[1].Src.Empty())

		shiftObservationsToBuffer(obs, buf, off)
		require.True(t, obs[1].Src.Empty(), "an already-empty Src stays empty")
	})
}
