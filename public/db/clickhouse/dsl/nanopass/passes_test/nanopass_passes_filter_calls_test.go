package passes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// A call with a FILTER clause is, to the server, the call fIf(args, c), not
// f. A pass that rewrites a call by its name leaves such a call as written
// rather than replace it with what f alone would be and drop the clause, or
// read the clause's call as f's.
func TestFilteredCallIsNotTheBareCall(t *testing.T) {
	unchanged := []struct {
		name string
		pass nanopass.Pass
		in   string
	}{
		{"evaluate", newTestEvaluator().Pass(), "SELECT myAdd(1, 2) FILTER (WHERE y) FROM t"},
		{"constructors_to_literal", passes.CanonicalizeConstructors(passes.ConstructorFormLiteral), "SELECT tuple(1, 2) FILTER (WHERE y) FROM t"},
		{"multiif_to_if", passes.CanonicalizeMultiIf, "SELECT multiIf(a, b, c) FILTER (WHERE d) FROM t"},
	}
	for _, tc := range unchanged {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.pass.Run(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.in, got)
		})
	}

	t.Run("extract_literals_composite", func(t *testing.T) {
		got, err := passes.ExtractLiterals(passes.NewExtractLiteralsConfig(1)).Run("SELECT array('a', 'b', 'c', 'd') FILTER (WHERE y) FROM t")
		require.NoError(t, err)
		assert.Contains(t, got, ") FILTER (WHERE y) FROM t")
		assert.NotContains(t, got, "['a', 'b', 'c', 'd']", "the call became one array parameter")
	})

	t.Run("selection_conditions_connective", func(t *testing.T) {
		p := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
			Schema: passes.NewStaticSchemaProvider(map[string][]string{"tt": {"a", "b", "c"}}),
		})
		got, err := p.Run("SELECT a FROM tt WHERE or(a = 1, b = 2) FILTER (WHERE c = 3)")
		require.NoError(t, err)
		assert.Equal(t, "SELECT a, (or(a = 1, b = 2) FILTER (WHERE c = 3)) AS cond_1 FROM tt WHERE cond_1", got)
	})
}

func TestFilteredCallIsNotALiteral(t *testing.T) {
	_, err := marshalling.UnmarshalCompositeLiteral("array(1, 2) FILTER (WHERE 1)")
	assert.Error(t, err)
}
