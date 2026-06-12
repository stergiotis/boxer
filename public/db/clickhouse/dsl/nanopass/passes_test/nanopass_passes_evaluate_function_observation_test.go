package passes_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observerEvaluator returns an evaluator with one registered handler:
// `multiMatchIndexAny`, returning ControlFlow{PassDiscardOutput} so the
// pass leaves the call alone in the output but the discard mechanism
// short-circuits any downstream rewriting. Models the analytical-pass
// shape pebble2impl will use for SQL function affordances.
func observerEvaluator(t *testing.T, sentinel uuid.UUID, sink *[]nanopass.Observation) *passes.FunctionEvaluator {
	t.Helper()
	eval := passes.NewFunctionEvaluator()
	eval.RegisterBuiltins()
	eval.Register("multiMatchIndexAny", func(args []any) (any, error) {
		return marshalling.ControlFlow{Sentinel: sentinel}, nil
	}, true)
	eval.OnObservation(func(obs nanopass.Observation) {
		*sink = append(*sink, obs)
	})
	return eval
}

func TestOnObservationFiresForRegisteredCall(t *testing.T) {
	var observed []nanopass.Observation
	eval := observerEvaluator(t, nanopass.PassDiscardOutput, &observed)

	out, err := eval.Pass().Run(
		"SELECT multiMatchIndexAny(text, 'foo.*', 'bar.*') FROM t")
	require.NoError(t, err)

	require.Len(t, observed, 1)
	assert.Equal(t, "multimatchindexany", observed[0].Name,
		"name arrives lower-cased")
	assert.Contains(t, out, "multiMatchIndexAny",
		"discard mechanism preserved the call site in the output")
}

// Column refs are not literals, so a call mixing them with literals
// should produce an Evaluated=false observation.
func TestOnObservationNonLiteralArgs(t *testing.T) {
	var observed []nanopass.Observation
	eval := observerEvaluator(t, nanopass.PassDiscardOutput, &observed)

	_, err := eval.Pass().Run(
		"SELECT multiMatchIndexAny(text, 'foo.*') FROM t")
	require.NoError(t, err)

	require.Len(t, observed, 1)
	obs := observed[0]
	assert.False(t, obs.Evaluated,
		"text is a column ref; tryEval should bail and report not-evaluated")
	assert.Nil(t, obs.Args,
		"Args is nil when Evaluated=false")
}

// All-literal args should populate Args for inspection.
func TestOnObservationLiteralArgsPopulated(t *testing.T) {
	var observed []nanopass.Observation
	eval := observerEvaluator(t, nanopass.PassDiscardOutput, &observed)

	_, err := eval.Pass().Run(
		"SELECT multiMatchIndexAny('haystack', 'foo.*', 'bar.*')")
	require.NoError(t, err)

	require.Len(t, observed, 1)
	obs := observed[0]
	assert.True(t, obs.Evaluated)
	require.Len(t, obs.Args, 3)
	assert.Equal(t, "haystack", obs.Args[0])
	assert.Equal(t, "foo.*", obs.Args[1])
	assert.Equal(t, "bar.*", obs.Args[2])
}

// Multiple calls should each produce one observation, in source order.
func TestOnObservationMultipleCallsInOrder(t *testing.T) {
	var observed []nanopass.Observation
	eval := observerEvaluator(t, nanopass.PassDiscardOutput, &observed)

	_, err := eval.Pass().Run(
		"SELECT multiMatchIndexAny('a', 'r1') AND multiMatchIndexAny('b', 'r2')")
	require.NoError(t, err)

	require.Len(t, observed, 2)
	assert.Equal(t, "r1", observed[0].Args[1])
	assert.Equal(t, "r2", observed[1].Args[1])
	assert.Less(t, observed[0].Src.Start, observed[1].Src.Start,
		"observations arrive in source order")
}

// Source ranges must point at the call expression, not the entire body.
func TestOnObservationSourceRangePointsAtCall(t *testing.T) {
	var observed []nanopass.Observation
	eval := observerEvaluator(t, nanopass.PassDiscardOutput, &observed)

	body := "SELECT multiMatchIndexAny('a', 'r1') FROM t"
	_, err := eval.Pass().Run(body)
	require.NoError(t, err)

	require.Len(t, observed, 1)
	obs := observed[0]
	require.False(t, obs.Src.Empty(), "src must be set")
	slice := body[obs.Src.Start:obs.Src.End]
	assert.True(t, strings.HasPrefix(slice, "multiMatchIndexAny"),
		"src.Start should point at the function name; got slice=%q", slice)
	assert.True(t, strings.HasSuffix(slice, ")"),
		"src.End should be just past the closing paren; got slice=%q", slice)
}

// Observation + discard mechanism interact correctly: every visited call
// fires the callback and the rewritten body is discarded by the runner.
func TestOnObservationComposesWithDiscard(t *testing.T) {
	var observed []nanopass.Observation
	eval := observerEvaluator(t, nanopass.PassDiscardOutput, &observed)

	body := "SELECT multiMatchIndexAny('x', 'r')"
	out, err := eval.Pass().Run(body)
	require.NoError(t, err)

	assert.Equal(t, body, out, "discard returns the original body")
	require.Len(t, observed, 1)
	assert.True(t, observed[0].Evaluated)
}

// Unregistered names produce no observations.
func TestOnObservationOnlyForRegisteredNames(t *testing.T) {
	var observed []nanopass.Observation
	eval := observerEvaluator(t, nanopass.PassDiscardOutput, &observed)

	_, err := eval.Pass().Run("SELECT lower('X'), upper('y')")
	require.NoError(t, err)

	assert.Len(t, observed, 0,
		"lower/upper aren't registered; no observations expected")
}
