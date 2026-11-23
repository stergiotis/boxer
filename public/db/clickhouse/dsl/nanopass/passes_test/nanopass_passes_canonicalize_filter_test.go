package passes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// aggregateFilterSugar pairs an aggregate filter with the call ClickHouse's
// parser rewrites it to (formatQuery and EXPLAIN SYNTAX on ClickHouse 26.9
// print the second for the first). The If suffix follows any combinator the
// name already carries, DISTINCT becomes the Distinct combinator ahead of
// If, and only a call named exactly count without DISTINCT loses its
// asterisk arguments.
var aggregateFilterSugar = []struct {
	name string
	in   string
	want string
}{
	{"plain", "SELECT sum(x) FILTER (WHERE y > 5) FROM t", "SELECT sumIf(x, y > 5) FROM t"},
	{"parametric", "SELECT quantile(0.05)(L) FILTER (WHERE L BETWEEN 177 AND 1502) AS p05 FROM t",
		"SELECT quantileIf(0.05)(L, L BETWEEN 177 AND 1502) AS p05 FROM t"},
	{"several_parameters", "SELECT quantiles(0.05, 0.95)(L) FILTER (WHERE L BETWEEN 177 AND 1502), topK(3, 2, 'counts')(L) FILTER (WHERE L > 1) FROM t",
		"SELECT quantilesIf(0.05, 0.95)(L, L BETWEEN 177 AND 1502), topKIf(3, 2, 'counts')(L, L > 1) FROM t"},
	{"parametric_several_arguments", "SELECT quantileExactWeighted(0.5)(L, w) FILTER (WHERE w > 0) FROM t",
		"SELECT quantileExactWeightedIf(0.5)(L, w, w > 0) FROM t"},
	{"parametric_no_space", "SELECT quantile(0.05)(L)FILTER(WHERE L>1) FROM t", "SELECT quantileIf(0.05)(L, L>1) FROM t"},
	{"lower_case_no_space", "SELECT avg(x)filter(where y) FROM t", "SELECT avgIf(x, y) FROM t"},
	{"no_arguments", "SELECT count() FILTER (WHERE y) FROM t", "SELECT countIf(y) FROM t"},
	{"count_asterisk", "SELECT count(*) FILTER (WHERE y), count(t.*) FILTER (WHERE y), count(x, *) FILTER (WHERE y) FROM t",
		"SELECT countIf(y), countIf(y), countIf(x, y) FROM t"},
	{"other_asterisk", "SELECT COUNT(*) FILTER (WHERE y), sum(*) FILTER (WHERE y) FROM t",
		"SELECT COUNTIf(*, y), sumIf(*, y) FROM t"},
	{"distinct", "SELECT count(DISTINCT x) FILTER (WHERE y), quantile(0.5)(DISTINCT x, z) FILTER (WHERE y) FROM t",
		"SELECT countDistinctIf(x, y), quantileDistinctIf(0.5)(x, z, y) FROM t"},
	{"distinct_asterisk", "SELECT count(DISTINCT *) FILTER (WHERE y) FROM t", "SELECT countDistinctIf(*, y) FROM t"},
	{"distinct_names_a_column", "SELECT count(DISTINCT) FILTER (WHERE y) FROM t", `SELECT countIf("DISTINCT", y) FROM t`},
	{"combinators", "SELECT sumArray(x) FILTER (WHERE y), uniqExactIf(x, a) FILTER (WHERE b), sumState(x) FILTER (WHERE y) FROM t",
		"SELECT sumArrayIf(x, y), uniqExactIfIf(x, a, b), sumStateIf(x, y) FROM t"},
	{"quoted_and_keyword_names", "SELECT `my agg`(x) FILTER (WHERE y), \"sum\"(x) FILTER (WHERE y), any(x) FILTER (WHERE y) FROM t",
		`SELECT "my aggIf"(x, y), sumIf(x, y), anyIf(x, y) FROM t`},
	{"window", "SELECT sum(x) FILTER (WHERE y) OVER (ORDER BY z), count(*) FILTER (WHERE y) OVER () FROM t",
		"SELECT sumIf(x, y) OVER (ORDER BY z), countIf(y) OVER () FROM t"},
	{"named_window", "SELECT sum(x) FILTER (WHERE y) OVER w FROM t WINDOW w AS (ORDER BY z)",
		"SELECT sumIf(x, y) OVER w FROM t WINDOW w AS (ORDER BY z)"},
	{"nested", "SELECT sum(sum(x) FILTER (WHERE y)) OVER () FROM t", "SELECT sum(sumIf(x, y)) OVER () FROM t"},
	{"subquery_in_condition", "SELECT sum(x) FILTER (WHERE y IN (SELECT a FROM u WHERE d = DATE '2020-01-01')) FROM t",
		"SELECT sumIf(x, y IN (SELECT a FROM u WHERE d = toDate('2020-01-01'))) FROM t"},
	{"alias_in_condition", "SELECT sum(x) FILTER (WHERE y AS yy) FROM t", "SELECT sumIf(x, y AS yy) FROM t"},
	{"param_slot", "SELECT sum(x) FILTER (WHERE y > {p:UInt8}) FROM t", "SELECT sumIf(x, y > {p:UInt8}) FROM t"},
	{"operand", "SELECT -sum(x) FILTER (WHERE y), groupArray(x) FILTER (WHERE y)[1] FROM t",
		"SELECT -sumIf(x, y), groupArrayIf(x, y)[1] FROM t"},
	{"any_call", "SELECT abs(x) FILTER (WHERE y) FROM t", "SELECT absIf(x, y) FROM t"},
}

func TestCanonicalizeSugarAggregateFilter(t *testing.T) {
	for _, tc := range aggregateFilterSugar {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nanopass.FixedPoint(passes.CanonicalizeSugar, 16).Run(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			again, err := passes.CanonicalizeSugar.Run(got)
			require.NoError(t, err)
			assert.Equal(t, got, again, "not idempotent")

			canonical, err := passes.CanonicalizeFull(100).Run(tc.in)
			require.NoError(t, err)
			assert.NotContains(t, canonical, "FILTER")
			_, err = nanopass.ParseCanonical(canonical)
			assert.NoError(t, err, "canonical form outside grammar2: %s", canonical)
			again, err = passes.CanonicalizeFull(100).Run(canonical)
			require.NoError(t, err)
			assert.Equal(t, canonical, again, "pipeline not idempotent")
		})
	}
}

// TestCanonicalizeFullAggregateFilterNames: the If call is quoted like any
// other name, and a column or alias named filter keeps its spelling.
func TestCanonicalizeFullAggregateFilterNames(t *testing.T) {
	got, err := passes.CanonicalizeFull(100).Run("SELECT quantile(0.05)(L) FILTER (WHERE L > 1) AS filter FROM t")
	require.NoError(t, err)
	assert.Equal(t, `SELECT "quantileIf"(0.05)("L", "L" > 1) AS "filter" FROM "t"`, got)
}
