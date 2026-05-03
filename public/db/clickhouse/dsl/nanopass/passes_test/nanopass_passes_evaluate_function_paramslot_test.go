//go:build llm_generated_opus47

package passes_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Direct Apply ---

// When Param.Value is already populated, the evaluator must fold the slot
// like a literal. Exercises the useAny=false path (myAdd) plus the
// ResolvedParamSlot unwrap above the useAny branch.
func TestEvalFunctionsParamSlot_ApplyDirectUseAnyFalse(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	e := env.NewEnvironment()
	e.Params["p1"] = env.Param{Name: "p1", Type: "Int64", Raw: "2", Value: int64(2)}

	got, err := pass.Apply(e, "SELECT myAdd({p1: Int64}, 3)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 5", got)
}

// useAny=true counterpart (myMul). Also verifies lazy hydration: Value is
// nil on entry, the evaluator deserialises Raw on demand and caches back.
func TestEvalFunctionsParamSlot_ApplyDirectUseAnyTrueLazyHydrate(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	e := env.NewEnvironment()
	e.Params["p1"] = env.Param{Name: "p1", Type: "Int64", Raw: "4"} // Value left nil on purpose

	got, err := pass.Apply(e, "SELECT myMul({p1: Int64}, 3)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 12", got)

	cached := e.Params["p1"]
	require.NotNil(t, cached.Value, "lazy hydration must populate Value")
	_, isTyped := cached.Value.(marshalling.TypedLiteral)
	assert.True(t, isTyped, "hydrated Value should be a marshalling.TypedLiteral, got %T", cached.Value)
}

// --- Unresolved slot ---

// A slot whose env has no SET binding stays unresolved → outer call is
// non-evaluable, body unchanged. Guards against accidental folding when a
// param is missing.
func TestEvalFunctionsParamSlot_Unresolved(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	e := env.NewEnvironment() // no Params entry for "p1"

	got, err := pass.Apply(e, "SELECT myAdd({p1: Int64}, 3)")
	require.NoError(t, err)
	assert.Contains(t, got, "myAdd")
	assert.Contains(t, got, "{p1")
}

// --- Canonical full pipeline ---

// SET prelude binds the param, body has the slot. Pass.Run extracts → eval →
// integrate, with the slot folded to a literal. The bound SET lines remain
// in the prelude (PruneUnreferencedParams is a separate concern).
func TestEvalFunctionsParamSlot_FullPipeline(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	input := "SET p1 = 2;\nSET p2 = 3;\nSELECT myAdd({p1: Int64}, {p2: Int64})"
	got, err := pass.Run(input)
	require.NoError(t, err)

	assert.Contains(t, got, "SELECT 5", "slot-arg myAdd was not folded; got: %q", got)
}

// --- End-to-end with extract and prune ---

// ExtractLiterals → FunctionEvaluator → PruneUnreferencedParams. The
// evaluator folds the slot, the prune pass drops the now-orphaned params.
// This is the canonical pipeline composition the placeholder-aware
// evaluator was designed for.
func TestEvalFunctionsParamSlot_EndToEndWithExtractAndPrune(t *testing.T) {
	eval := newTestEvaluator()

	cfg := passes.NewExtractLiteralsConfig(0)
	cfg.SetMinINListSize(0)

	sql := "SELECT myAdd(2, 3)"

	extracted, err := passes.ExtractLiterals(cfg).Run(sql)
	require.NoError(t, err)
	require.Contains(t, extracted, "SET param_x_")
	require.Contains(t, extracted, "{param_x_")

	evaluated, err := eval.Pass().Run(extracted)
	require.NoError(t, err)
	require.Contains(t, evaluated, "SELECT 5")

	pruned, err := passes.PruneUnreferencedParams("").Run(evaluated)
	require.NoError(t, err)

	assert.NotContains(t, strings.ToUpper(pruned), "SET ", "prune should drop orphaned param SETs; got: %q", pruned)
	assert.Equal(t, "SELECT 5", strings.TrimSpace(pruned))
}
