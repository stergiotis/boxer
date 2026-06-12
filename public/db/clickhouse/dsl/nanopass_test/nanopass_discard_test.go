//go:build llm_generated_opus47

package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// analyticalPass is a test fixture: a body-only pass that returns the
// discard marker (typically embedded in a SQL comment; the runner's
// quote-aware scan finds it anywhere outside string literals and quoted
// identifiers). Optionally counts invocations so tests can assert
// single-shot semantics.
func analyticalPass(name string, calls *int) nanopass.Pass {
	return nanopass.LiftBodyPass(name,
		func(sql string) (string, error) {
			if calls != nil {
				*calls++
			}
			return nanopass.PassDiscardOutputMarker + "\n" + sql, nil
		},
		nanopass.PassProperties{},
	)
}

// rewritePass is a test fixture that performs an actual transformation,
// so we can prove the runner picked the analytical pass's input over its
// rewritten output.
func rewritePass(name string, suffix string) nanopass.Pass {
	return nanopass.LiftBodyPass(name,
		func(sql string) (string, error) {
			return sql + suffix, nil
		},
		nanopass.PassProperties{},
	)
}

func TestPassRunDiscardsAnalyticalOutput(t *testing.T) {
	calls := 0
	p := analyticalPass("observe", &calls)
	out, err := p.Run("select a from t")
	require.NoError(t, err)
	assert.Equal(t, "select a from t", out, "Run must return the input unchanged")
	assert.Equal(t, 1, calls, "analytical pass should have run exactly once")
}

func TestSequenceForwardsInputThroughAnalyticalPass(t *testing.T) {
	// observe is analytical; rewrite appends " /* tail */"; the Sequence
	// must hand rewrite the *original* input, not observe's marker output.
	calls := 0
	pipe := nanopass.Sequence("observe+rewrite",
		analyticalPass("observe", &calls),
		rewritePass("rewrite", " /* tail */"),
	)
	out, err := pipe.Run("select a from t")
	require.NoError(t, err)
	assert.Equal(t, "select a from t /* tail */", out)
	assert.Equal(t, 1, calls)
}

func TestSequenceAnalyticalDoesNotPropagateMarker(t *testing.T) {
	// Two analytical passes followed by a rewrite. None of the markers
	// should leak into the rewrite's input.
	pipe := nanopass.Sequence("two-observe+rewrite",
		analyticalPass("observe1", nil),
		analyticalPass("observe2", nil),
		rewritePass("rewrite", "!"),
	)
	out, err := pipe.Run("x")
	require.NoError(t, err)
	assert.Equal(t, "x!", out)
}

func TestFixedPointTerminatesOnAnalyticalPass(t *testing.T) {
	// An analytical pass returns a marker every iteration. Without the
	// discard short-circuit this would never converge (each iteration's
	// "next" differs from "result"). With the short-circuit, the loop
	// exits after one iteration and never calls Apply again.
	calls := 0
	pass := nanopass.FixedPoint(analyticalPass("observe", &calls), 5)
	out, err := pass.Run("hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", out)
	assert.Equal(t, 1, calls, "fixed-point should not iterate past the first discard")
}

func TestSequenceMixesAnalyticalAndIdempotentTransform(t *testing.T) {
	// Realistic shape: an analytical observation pass alongside the
	// existing canonicalisation pipeline. The transform output must equal
	// running the canonicalisation alone.
	calls := 0
	pipe := nanopass.Sequence("observe+canonical",
		analyticalPass("observe", &calls),
		passes.CanonicalizeKeywordCase,
	)
	out, err := pipe.Run("select a from t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t", out)
	assert.Equal(t, 1, calls)
}
