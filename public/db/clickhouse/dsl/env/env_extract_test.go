//go:build llm_generated_opus47

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
