//go:build llm_generated_opus47

package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// TestAffordanceEvaluatorEndToEnd proves the wire from
// newAffordanceEvaluator() through the analytical pass through the
// canonicalisation pipeline back to the editor preview. Substitutes a
// test-controlled OnObservation in place of the zerolog one so we can
// assert what arrived; the production builder
// (newAffordanceEvaluator) is exercised in the play GUI's debounced
// preview.
func TestAffordanceEvaluatorEndToEnd(t *testing.T) {
	var observed []nanopass.Observation
	eval := passes.NewFunctionEvaluator()
	eval.Register("multiMatchIndexAny", func(args []any) (any, error) {
		return marshalling.ControlFlow{Sentinel: nanopass.PassDiscardOutput}, nil
	}, true)
	eval.OnObservation(func(obs nanopass.Observation) {
		observed = append(observed, obs)
	})

	pipe := nanopass.Sequence("sqlPreview",
		eval.Pass(),
		passes.StripComments,
		passes.CanonicalizeKeywordCase,
		passes.CanonicalizeWhitespace,
		passes.RemoveRedundantParens,
	)

	out, err := pipe.Run(
		"select multiMatchIndexAny(text, 'foo.*', 'bar.*') from t")
	if err != nil {
		t.Fatalf("preview pipeline failed: %v", err)
	}

	// Canonicalisation still ran (KEYWORD_CASE upcased SELECT/FROM).
	if !strings.HasPrefix(out, "SELECT ") {
		t.Errorf("CanonicalizeKeywordCase did not run; out=%q", out)
	}
	// The call site survives the analytical pass — discard mechanism
	// forwarded its input to the canonicalisers.
	if !strings.Contains(out, "multiMatchIndexAny") {
		t.Errorf("multiMatchIndexAny was rewritten away; out=%q", out)
	}

	// And the observation arrived.
	if got := len(observed); got != 1 {
		t.Fatalf("expected 1 observation, got %d", got)
	}
	if observed[0].Name != "multimatchindexany" {
		t.Errorf("observation name=%q, want multimatchindexany",
			observed[0].Name)
	}
	if observed[0].Src.Empty() {
		t.Errorf("observation src is empty")
	}
}
