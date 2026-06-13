package env

// Regression tests for the 2026-06-12 hostile review of the env package.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Multiple SET statements on one line must each become an entry — the
// previous line-based parser stuffed everything after the first `=` into
// the first entry's Raw ("1; SET param_b = 2"), corrupting param hydration
// and CTE injection downstream.
func TestRegressionMultiSetPerLine(t *testing.T) {
	e, body, err := Extract("SET param_a = 1; SET param_b = 2;\nSELECT {a: UInt64}")
	require.NoError(t, err)
	assert.Equal(t, "1", e.Params["param_a"].Raw)
	assert.Equal(t, "2", e.Params["param_b"].Raw)
	assert.Equal(t, "SELECT {a: UInt64}", body)
}

// A semicolon inside a quoted value must not terminate the statement.
func TestRegressionSemicolonInsideValue(t *testing.T) {
	e, body, err := Extract("SET param_s = 'a;b'; SET param_t = 'c';\nSELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "'a;b'", e.Params["param_s"].Raw)
	assert.Equal(t, "'c'", e.Params["param_t"].Raw)
	assert.Equal(t, "SELECT 1", body)
}

// `=` without surrounding spaces and case-variant SET both classify.
func TestRegressionSetSpellingTolerance(t *testing.T) {
	e, _, err := Extract("set param_x=5;\nSeT max_threads = 4;\nSELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "5", e.Params["param_x"].Raw)
	assert.Equal(t, "4", e.SessionSettings["max_threads"].Raw)
}

// A line that is not entirely SET statements is left to the body whole —
// no half-harvesting.
func TestRegressionPartialLineNotHarvested(t *testing.T) {
	e, body, err := Extract("SET a = 1; SELECT 2")
	require.NoError(t, err)
	assert.Empty(t, e.SessionSettings)
	assert.True(t, strings.HasPrefix(body, "SET a = 1; SELECT 2"))
}

// Round-trip: harvested prelude re-integrates equivalently.
func TestRegressionPreludeRoundTrip(t *testing.T) {
	e, body, err := Extract("SET param_a = 1; SET b = 'x;y';\nSELECT {a: UInt64}")
	require.NoError(t, err)
	out, err := e.Integrate(body)
	require.NoError(t, err)
	e2, body2, err := Extract(out)
	require.NoError(t, err)
	assert.Equal(t, body, body2)
	assert.Equal(t, e.Params["param_a"].Raw, e2.Params["param_a"].Raw)
	assert.Equal(t, e.SessionSettings["b"].Raw, e2.SessionSettings["b"].Raw)
}

// A garbage SET key (not identifier-shaped) rejects the whole line so it
// stays in the body and fails loudly through the parser, rather than
// round-tripping as invalid SET output.
func TestRegressionGarbageSetKeyNotHarvested(t *testing.T) {
	_, body, err := Extract("SET 0 = 1;\nSELECT 1")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(body, "SET 0 = 1"))
}

// A SET value with an unterminated quote on the line (a string spanning
// lines) is left to the body, not half-harvested.
func TestRegressionMultilineValueNotHarvested(t *testing.T) {
	_, body, err := Extract("SET a = 'x\ny';\nSELECT 1")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(body, "SET a = 'x"))
}

// A keyword-shaped SET key is harvested (the grammar's identifier rule
// tolerates keywords); emission quoting is the unparser's concern.
func TestRegressionKeywordSetKeyHarvested(t *testing.T) {
	e, _, err := Extract(`SET "AS" = '';` + "\nSELECT 1")
	require.NoError(t, err)
	_, ok := e.SessionSettings[`"AS"`]
	assert.True(t, ok, "quoted keyword setting name preserved verbatim")
}
