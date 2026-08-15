package env_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractEmptyPrelude(t *testing.T) {
	e, body, err := env.Extract("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", body)
	assert.Empty(t, e.SessionSettings)
	assert.Empty(t, e.Params)
}

func TestExtractSessionSetting(t *testing.T) {
	e, body, err := env.Extract("SET max_threads = 4;\nSELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", body)
	assert.Equal(t, env.Setting{Name: "max_threads", Raw: "4"}, e.SessionSettings["max_threads"])
	assert.Empty(t, e.Params)
}

func TestExtractMultipleSessionSettings(t *testing.T) {
	in := "SET max_threads = 4;\nSET send_logs_level = 'trace';\nSELECT 1"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", body)
	assert.Len(t, e.SessionSettings, 2)
	assert.Equal(t, "4", e.SessionSettings["max_threads"].Raw)
	assert.Equal(t, "'trace'", e.SessionSettings["send_logs_level"].Raw)
}

func TestExtractParamSet(t *testing.T) {
	in := "SET param_a = 5;\nSELECT {a: UInt64}"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	assert.Equal(t, "SELECT {a: UInt64}", body)
	a := e.Params["param_a"]
	assert.Equal(t, "param_a", a.Name)
	assert.Equal(t, "5", a.Raw)
}

func TestExtractParamSlotPopulatesType(t *testing.T) {
	e, _, err := env.Extract("SELECT {a: UInt64}")
	require.NoError(t, err)
	a, ok := e.Params["a"]
	require.True(t, ok, "expected param 'a' in env")
	assert.Equal(t, "UInt64", a.Type)
	assert.Equal(t, "", a.Raw)
	assert.True(t, a.IsUnresolved())
}

func TestExtractParamSlotMergesWithSet(t *testing.T) {
	in := "SET param_a = 5;\nSELECT {param_a: UInt64}"
	e, _, err := env.Extract(in)
	require.NoError(t, err)
	a, ok := e.Params["param_a"]
	require.True(t, ok)
	assert.Equal(t, "UInt64", a.Type)
	assert.Equal(t, "5", a.Raw)
	assert.True(t, a.IsResolved())
}

func TestExtractMixedPrelude(t *testing.T) {
	in := "SET max_threads = 4;\nSET param_a = 5;\nSET param_b = 'foo';\nSELECT {a: UInt64} + {b: String}"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(body, "SELECT"))
	assert.Len(t, e.SessionSettings, 1)
	assert.Equal(t, "4", e.SessionSettings["max_threads"].Raw)
	// Body slots use bare names a/b; SET lines use param_a/param_b. No merge.
	assert.Contains(t, e.Params, "param_a")
	assert.Contains(t, e.Params, "param_b")
	assert.Contains(t, e.Params, "a")
	assert.Contains(t, e.Params, "b")
}

func TestExtractStopsAtFirstNonSetLine(t *testing.T) {
	// A non-SET line ends the prelude; further SET lines stay in body.
	in := "SET max_threads = 4;\nSELECT 1;\nSET ignored = 99;"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	assert.Len(t, e.SessionSettings, 1)
	assert.Contains(t, body, "SET ignored")
}

func TestExtractTolerantToSemicolonAndWhitespace(t *testing.T) {
	in := "  SET  max_threads = 4 ;\n\nSELECT 1"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", body)
	assert.Equal(t, "4", e.SessionSettings["max_threads"].Raw)
}

func TestIntegrateEmpty(t *testing.T) {
	e := env.NewEnvironment()
	sql, err := e.Integrate("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", sql)
}

func TestIntegrateSettingsAndParams(t *testing.T) {
	e := env.NewEnvironment()
	e.SessionSettings["max_threads"] = env.Setting{Name: "max_threads", Raw: "4"}
	e.Params["param_a"] = env.Param{Name: "param_a", Raw: "5"}
	sql, err := e.Integrate("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SET max_threads = 4;\nSET param_a = 5;\nSELECT 1", sql)
}

func TestIntegrateOmitsParamWithoutRaw(t *testing.T) {
	e := env.NewEnvironment()
	// Type-only entry (e.g. populated by slot scan) should not be emitted as SET.
	e.Params["a"] = env.Param{Name: "a", Type: "UInt64"}
	sql, err := e.Integrate("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", sql)
}

func TestRoundTripPreservesResolvedParams(t *testing.T) {
	in := "SET param_a = 5;\nSELECT {param_a: UInt64}"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	out, err := e.Integrate(body)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestExtractSettingsClause(t *testing.T) {
	// SETTINGS clause stays in body; env.StatementSettings is a read-only view.
	e, body, err := env.Extract("SELECT 1 SETTINGS max_threads = 4, send_logs_level = 'trace'")
	require.NoError(t, err)
	assert.Contains(t, body, "SETTINGS")
	assert.Len(t, e.StatementSettings, 2)
	assert.Equal(t, "4", e.StatementSettings["max_threads"].Raw)
	assert.Equal(t, "'trace'", e.StatementSettings["send_logs_level"].Raw)
}

func TestExtractFormatClause(t *testing.T) {
	// FORMAT stays in body; env.Format is a read-only view.
	e, body, err := env.Extract("SELECT 1 FORMAT TabSeparated")
	require.NoError(t, err)
	assert.Contains(t, body, "FORMAT")
	assert.Equal(t, "TabSeparated", e.Format)
}

func TestExtractSettingsAndFormat(t *testing.T) {
	e, body, err := env.Extract("SELECT 1 SETTINGS max_threads = 4 FORMAT JSON")
	require.NoError(t, err)
	assert.Contains(t, body, "SETTINGS")
	assert.Contains(t, body, "FORMAT")
	assert.Equal(t, "4", e.StatementSettings["max_threads"].Raw)
	assert.Equal(t, "JSON", e.Format)
}

func TestExtractFullEnvironment(t *testing.T) {
	in := "SET max_threads = 8;\nSET param_a = 5;\nSELECT {param_a: UInt64} SETTINGS k = 'v' FORMAT CSV"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(body, "SELECT"))
	assert.Equal(t, "8", e.SessionSettings["max_threads"].Raw)
	assert.Equal(t, "5", e.Params["param_a"].Raw)
	assert.Equal(t, "UInt64", e.Params["param_a"].Type)
	assert.Equal(t, "'v'", e.StatementSettings["k"].Raw)
	assert.Equal(t, "CSV", e.Format)
}

func TestIntegrateDoesNotEmitInlineSettingsOrFormat(t *testing.T) {
	// StatementSettings and Format are read-only views; mutations go via
	// body-CST passes. Integrate must NOT re-emit them or we double-write.
	e := env.NewEnvironment()
	e.StatementSettings["k"] = env.Setting{Name: "k", Raw: "1"}
	e.Format = "JSON"
	sql, err := e.Integrate("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", sql)
}

func TestRoundTripFullEnvironment(t *testing.T) {
	in := "SET max_threads = 8;\nSET param_a = 5;\nSELECT {param_a: UInt64} SETTINGS k = 'v' FORMAT CSV"
	e, body, err := env.Extract(in)
	require.NoError(t, err)
	out, err := e.Integrate(body)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestRoundTripDeterministicOrder(t *testing.T) {
	// Two valid orderings of the same prelude should integrate to the same canonical form.
	a := "SET param_b = 2;\nSET param_a = 1;\nSELECT {param_a: UInt64} + {param_b: UInt64}"
	b := "SET param_a = 1;\nSET param_b = 2;\nSELECT {param_a: UInt64} + {param_b: UInt64}"
	ea, bodya, err := env.Extract(a)
	require.NoError(t, err)
	eb, bodyb, err := env.Extract(b)
	require.NoError(t, err)
	outa, _ := ea.Integrate(bodya)
	outb, _ := eb.Integrate(bodyb)
	assert.Equal(t, outa, outb)
}

// BodyOffset must agree with Extract on every input shape: Extract's body is
// a byte-identical suffix of the input, and BodyOffset names where it starts.
// Callers rebase pass-recorded byte ranges onto the original SQL with it, so
// a disagreement silently mis-slices their source text.
func TestBodyOffsetLocatesExtractBody(t *testing.T) {
	inputs := []string{
		"SELECT 1",
		"   SELECT 1",
		"\n\n\tSELECT 1",
		"SET param_a = 1;\nSELECT {a: UInt64}",
		"SET param_a = 1;\nSET max_threads = 8;\n\n  SELECT 1",
		"\nSET param_a = 1;\n\nSELECT 1",
		"SET param_a = 1;\r\nSELECT 1",
		"SET param_a = 1;", // prelude only — body is empty
		"",
		"   ",
		"-- a comment\nSELECT 1", // no prelude; comment stays in the body
		"NOTASET foo\nSELECT 1",  // unparseable prelude line stays in the body
		// Comments the prelude spans. The body starts past the SET they
		// precede, so the offset is what moves, not the suffix rule.
		"-- c\nSET param_a = 1;\nSELECT 1",
		"/* c */\nSET param_a = 1;\nSELECT 1",
		"/* one\ntwo */\nSET param_a = 1;\nSELECT 1",
		"SET param_a = 1; -- why\nSELECT 1",
		"SET a = 1;\n-- mid\nSET param_b = 2;\nSELECT 1",
		"-- only a comment, no SET\nSELECT 1;\nSET x = 1;",
		// Whitespace OUTSIDE the ASCII cutset the body skip uses. These
		// discriminate that cutset from unicode.IsSpace, which the rest of
		// the table cannot — every other input here uses only space, tab,
		// CR or LF, which both sets contain.
		"SET a = 1;\n\v\fSELECT 1",
		"SET a = 1;\n\u00a0SELECT 1",
	}
	for _, in := range inputs {
		_, body, err := env.Extract(in)
		require.NoError(t, err, "input %q", in)
		off := env.BodyOffset(in)
		require.GreaterOrEqual(t, off, 0, "input %q", in)
		require.LessOrEqual(t, off, len(in), "input %q", in)
		assert.Equal(t, body, in[off:],
			"BodyOffset must name where Extract's body starts (input %q)", in)
	}
}

// A comment must not end the prelude before it starts. It used to: the harvest
// took only a leading run of pure SET lines, so a header comment collapsed
// BodyOffset to zero, the buffer read as two statements, and a run-under-cursor
// shipped the body without the `SET param_*` lines that bind it — every
// parameter then reading as unfilled. See ADR-0006's 2026-08-15 Update; the
// repo's own `-- play: …` directives are written exactly here.
func TestPreludeSpansComments(t *testing.T) {
	const body = "SELECT {x:String}"
	for name, in := range map[string]string{
		"line comment above":       "-- c\n" + body2Set + body,
		"slash comment above":      "// c\n" + body2Set + body,
		"block comment above":      "/* c */\n" + body2Set + body,
		"block comment over lines": "/* one\ntwo */\n" + body2Set + body,
		"comment trailing the SET": "SET param_x = '1'; -- why\n" + body,
		"comment between SETs":     "SET a = 1;\n-- mid\n" + body2Set + body,
	} {
		t.Run(name, func(t *testing.T) {
			e, gotBody, err := env.Extract(in)
			require.NoError(t, err)
			assert.Equal(t, body, gotBody, "the prelude is consumed whole")
			assert.Equal(t, "'1'", e.Params["param_x"].Raw, "the binding survives the comment")
			assert.NotEmpty(t, e.PreludeComments, "the comment is kept, not dropped")
		})
	}
}

// body2Set is the one-line prelude the comment cases above wrap.
const body2Set = "SET param_x = '1';\n"

// A leading run with no SET in it is not a prelude. Without this the offset
// would move for every documented query, shifting each pass-recorded range by
// the length of the buffer's own header.
func TestCommentWithoutSetIsNotAPrelude(t *testing.T) {
	for _, in := range []string{
		"-- a comment\nSELECT 1",
		"/* a comment */\nSELECT 1",
		"-- a comment\nSELECT 1;\nSET x = 1;",
	} {
		e, body, err := env.Extract(in)
		require.NoError(t, err)
		assert.Equal(t, 0, env.BodyOffset(in), "input %q", in)
		assert.Equal(t, in, body, "input %q", in)
		assert.Empty(t, e.PreludeComments, "input %q", in)
	}
}

// A comment the prelude absorbs has left the body, and Integrate rebuilds the
// prelude from the Environment alone — so without PreludeComments the harvest
// would silently delete it. Above a prelude it comes back where it was;
// interleaved it floats to the top, the same normalisation the SET lines
// already undergo by being emitted alphabetically.
func TestRoundTripKeepsPreludeComments(t *testing.T) {
	identical := []string{
		"-- c\nSET param_a = 1;\nSELECT {param_a: UInt64}",
		"// c\nSET param_a = 1;\nSELECT {param_a: UInt64}",
		"/* c */\nSET param_a = 1;\nSELECT {param_a: UInt64}",
		"/* one\ntwo */\nSET param_a = 1;\nSELECT {param_a: UInt64}",
		// Below the last SET the comment is body text and never moved.
		"SET param_a = 1;\n-- c\nSELECT {param_a: UInt64}",
	}
	for _, in := range identical {
		e, body, err := env.Extract(in)
		require.NoError(t, err)
		out, err := e.Integrate(body)
		require.NoError(t, err)
		assert.Equal(t, in, out, "round-trip must be byte-identical (input %q)", in)
	}

	normalised := map[string]string{
		"SET param_a = 1; -- why\nSELECT 1":              "-- why\nSET param_a = 1;\nSELECT 1",
		"SET a = 1;\n-- mid\nSET param_b = 2;\nSELECT 1": "-- mid\nSET a = 1;\nSET param_b = 2;\nSELECT 1",
	}
	for in, want := range normalised {
		e, body, err := env.Extract(in)
		require.NoError(t, err)
		out, err := e.Integrate(body)
		require.NoError(t, err)
		assert.Equal(t, want, out, "input %q", in)
	}
}

// The scanner tracks quotes and comments in one walk, so neither can hide
// inside the other. Everything here would have been harvested wrongly — or
// harvested at all — by checking for them separately.
func TestPreludeCommentScannerEdges(t *testing.T) {
	t.Run("a comment marker inside a string value is not a comment", func(t *testing.T) {
		e, body, err := env.Extract("SET param_s = 'a--b';\nSELECT 1")
		require.NoError(t, err)
		assert.Equal(t, "'a--b'", e.Params["param_s"].Raw)
		assert.Equal(t, "SELECT 1", body)
		assert.Empty(t, e.PreludeComments)
	})

	t.Run("a quote inside a comment does not open a string", func(t *testing.T) {
		e, body, err := env.Extract("-- don't\nSET param_a = 1;\nSELECT 1")
		require.NoError(t, err)
		assert.Equal(t, "1", e.Params["param_a"].Raw)
		assert.Equal(t, "SELECT 1", body)
	})

	t.Run("a value spanning lines still ends the prelude", func(t *testing.T) {
		// Unchanged: it cannot be split line-wise, so it stays in the body
		// where the grammar parses it correctly.
		in := "SET param_a = 'x\ny';\nSELECT 1"
		e, body, err := env.Extract(in)
		require.NoError(t, err)
		assert.Equal(t, in, body)
		assert.Empty(t, e.Params)
	})

	t.Run("a block comment mid-statement leaves a separator behind", func(t *testing.T) {
		// Dropping it outright would splice `1/*c*/2` into the value `12`.
		// A space makes ClickHouse reject it instead.
		e, _, err := env.Extract("SET param_a = 1/*c*/2;\nSELECT 1")
		require.NoError(t, err)
		assert.Equal(t, "1 2", e.Params["param_a"].Raw)
	})

	t.Run("hash is deliberately not a comment", func(t *testing.T) {
		// ClickHouse takes `#` as a line comment; grammar1's lexer — which
		// every downstream consumer uses — does not, and env growing a second
		// comment vocabulary would be a worse defect than the one being fixed.
		in := "# c\nSET param_a = 1;\nSELECT 1"
		_, body, err := env.Extract(in)
		require.NoError(t, err)
		assert.Equal(t, in, body, "the line ends the prelude, as any unparseable one does")
	})
}
