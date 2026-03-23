//go:build llm_generated_opus46

package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipeline(t *testing.T) {
	sql := "select a from t"
	result, err := nanopass.Pipeline(sql,
		passes.NormalizeKeywordCase,
		passes.QualifyTables("mydb"),
	)
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM mydb.t", result)
}

func TestPipelineWithValidation(t *testing.T) {
	sql := "select a from t where a > 1"
	result, err := nanopass.Pipeline(sql,
		nanopass.Validate,
		passes.NormalizeKeywordCase,
		nanopass.Validate,
	)
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t WHERE a > 1", result)
}

func TestFixedPoint(t *testing.T) {
	// A pass that is already idempotent should converge in 1 iteration
	pass := nanopass.FixedPoint(passes.NormalizeKeywordCase, 5)
	result, err := pass("select a from t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t", result)
}

func TestFixedPointConverges(t *testing.T) {
	// RemoveRedundantParens on deeply nested parens:
	// First pass removes outer layer, second pass removes next, etc.
	// With fixed-point it should fully converge.
	pass := nanopass.FixedPoint(passes.RemoveRedundantParens, 10)
	result, err := pass("SELECT ((((a)))) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t", result)

	_, err = nanopass.Parse(result)
	require.NoError(t, err)
}

func TestFixedPointPipeline(t *testing.T) {
	pass := nanopass.FixedPointPipeline(5,
		passes.NormalizeKeywordCase,
		passes.QualifyTables("mydb"),
		passes.NormalizeWhitespaceSingleLine,
	)
	result, err := pass("select  a  from  t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM mydb.t", result)
}

func TestFixedPointDoesNotConverge(t *testing.T) {
	// A pass that always changes the SQL will never converge
	counter := 0
	neverSettles := func(sql string) (string, error) {
		counter++
		return sql + " ", nil // always appends a space
	}
	pass := nanopass.FixedPoint(neverSettles, 3)
	_, err := pass("SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not converge")
}
