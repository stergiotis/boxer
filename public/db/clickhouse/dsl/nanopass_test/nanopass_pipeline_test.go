//go:build llm_generated_opus47

package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipeline(t *testing.T) {
	pipe := nanopass.Sequence("kw+qualify",
		passes.CanonicalizeKeywordCase,
		passes.QualifyTables("mydb"),
	)
	result, err := pipe.Run("select a from t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM mydb.t", result)
}

func TestPipelineWithValidation(t *testing.T) {
	pipe := nanopass.Sequence("validate+kw+validate",
		nanopass.ValidateGrammar1,
		passes.CanonicalizeKeywordCase,
		nanopass.ValidateGrammar1,
	)
	result, err := pipe.Run("select a from t where a > 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t WHERE a > 1", result)
}

func TestFixedPoint(t *testing.T) {
	// A pass that is already idempotent should converge in 1 iteration.
	pass := nanopass.FixedPoint(passes.CanonicalizeKeywordCase, 5)
	result, err := pass.Run("select a from t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t", result)
}

func TestFixedPointConverges(t *testing.T) {
	// RemoveRedundantParens on deeply nested parens:
	// First pass removes outer layer, second pass removes next, etc.
	// With fixed-point it should fully converge.
	pass := nanopass.FixedPoint(passes.RemoveRedundantParens, 10)
	result, err := pass.Run("SELECT ((((a)))) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t", result)

	_, err = nanopass.Parse(result)
	require.NoError(t, err)
}

func TestFixedPointPipeline(t *testing.T) {
	// Replaces the old FixedPointPipeline: wrap a Sequence in FixedPoint.
	pipe := nanopass.FixedPoint(
		nanopass.Sequence("kw+qualify+ws",
			passes.CanonicalizeKeywordCase,
			passes.QualifyTables("mydb"),
			passes.CanonicalizeWhitespaceSingleLine,
		),
		5,
	)
	result, err := pipe.Run("select  a  from  t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM mydb.t", result)
}

func TestFixedPointDoesNotConverge(t *testing.T) {
	// A pass that always changes the SQL never converges.
	counter := 0
	neverSettles := nanopass.Pass{
		Name: "neverSettles",
		Apply: func(_ *env.Environment, body string) (string, error) {
			counter++
			return body + " ", nil
		},
	}
	pass := nanopass.FixedPoint(neverSettles, 3)
	_, err := pass.Run("SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not converge")
}
