package distsql

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stretchr/testify/require"
)

// testGrid keeps goldens readable; the real pass uses GridLevels(), whose
// shape is pinned by TestGridLevelsPinnedShape — the composition is the
// one-line closure in ExpandDescriptiveStatistics.
var testGrid = []float64{0.25, 0.5, 0.75}

// branchGolden is the hand-written §SD1 select list for column expression
// col over testGrid with the tdigest default.
func branchGolden(col, qcall, token string) string {
	return "'" + col + "' AS series, count(" + col + ") AS n, toUInt64(count() - count(" + col + ")) AS n_null, " +
		"min(" + col + ") AS x_min, max(" + col + ") AS x_max, avg(" + col + ") AS mean, stddevSamp(" + col + ") AS sd, " +
		"skewSamp(" + col + ") AS skew, kurtSamp(" + col + ") AS kurt, [0.25,0.5,0.75] AS ps, " +
		qcall + "(" + col + ") AS qs, '" + token + "' AS estimator"
}

func TestExpandGolden(t *testing.T) {
	tests := []struct{ name, input, expected string }{
		{
			name:     "basic tdigest default",
			input:    "SELECT descriptiveStatistics(x) FROM t",
			expected: "SELECT " + branchGolden("x", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM t",
		},
		{
			name:  "two columns union",
			input: "SELECT descriptiveStatistics(x, y) FROM t WHERE x > 0",
			expected: "SELECT " + branchGolden("x", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM t WHERE x > 0" +
				" UNION ALL SELECT " + branchGolden("y", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM t WHERE x > 0",
		},
		{
			name:     "exact estimator",
			input:    "SELECT descriptiveStatistics('exact', x) FROM t",
			expected: "SELECT " + branchGolden("x", "quantilesExactInclusive(0.25,0.5,0.75)", "exact-hf7") + " FROM t",
		},
		{
			name:     "gk estimator carries accuracy",
			input:    "SELECT descriptiveStatistics('gk', x) FROM t",
			expected: "SELECT " + branchGolden("x", "quantilesGK(1000,0.25,0.5,0.75)", "gk:1000") + " FROM t",
		},
		{
			name:     "dd estimator carries relative accuracy",
			input:    "SELECT descriptiveStatistics('dd', x) FROM t",
			expected: "SELECT " + branchGolden("x", "quantilesDD(0.01,0.25,0.5,0.75)", "dd:0.01") + " FROM t",
		},
		{
			name:  "group keys fold into series",
			input: "SELECT descriptiveStatistics(x) FROM t GROUP BY svc, region",
			expected: "SELECT concat('x', ' · ', ifNull(toString(svc), '∅'), ' · ', ifNull(toString(region), '∅'))" +
				strings.TrimPrefix(branchGolden("x", "quantilesTDigest(0.25,0.5,0.75)", "tdigest"), "'x'") +
				" FROM t GROUP BY svc, region",
		},
		{
			name:     "settings ride the tail",
			input:    "SELECT descriptiveStatistics(x) FROM t SETTINGS max_threads = 4",
			expected: "SELECT " + branchGolden("x", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM t SETTINGS max_threads = 4",
		},
		{
			name:     "WITH prefix rides every branch",
			input:    "WITH c AS (SELECT 1 AS v) SELECT descriptiveStatistics(v) FROM c",
			expected: "WITH c AS (SELECT 1 AS v) SELECT " + branchGolden("v", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM c",
		},
		{
			name:     "expression argument quoted into the label",
			input:    "SELECT descriptiveStatistics(x + 1) FROM t",
			expected: "SELECT " + branchGolden("x + 1", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM t",
		},
		{
			name:     "case-insensitive name",
			input:    "SELECT DESCRIPTIVESTATISTICS(x) FROM t",
			expected: "SELECT " + branchGolden("x", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM t",
		},
		{
			name:     "quoted canonical spelling",
			input:    `SELECT "descriptiveStatistics"(x) FROM t`,
			expected: "SELECT " + branchGolden("x", "quantilesTDigest(0.25,0.5,0.75)", "tdigest") + " FROM t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandDescriptiveStatistics(tt.input, testGrid)
			require.NoError(t, err)
			require.Equal(t, tt.expected, got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

func TestExpandPassThroughIsByteIdentical(t *testing.T) {
	in := "SELECT a, b FROM t WHERE a > 1 ORDER BY b LIMIT 5"
	got, err := expandDescriptiveStatistics(in, testGrid)
	require.NoError(t, err)
	require.Equal(t, in, got)
}

func TestExpandRejects(t *testing.T) {
	tests := []struct{ name, input, wantSub string }{
		{"mixed select items", "SELECT a, descriptiveStatistics(x) FROM t", "sole select item"},
		{"aliased", "SELECT descriptiveStatistics(x) AS d FROM t", "stand alone"},
		{"nested call", "SELECT descriptiveStatistics(descriptiveStatistics(x)) FROM t", "exactly one call"},
		{"inside subquery", "SELECT (SELECT descriptiveStatistics(x) FROM t) FROM u", "top-level"},
		{"inside union", "SELECT descriptiveStatistics(x) FROM t UNION ALL SELECT 1", "UNION"},
		{"order by", "SELECT descriptiveStatistics(x) FROM t ORDER BY x", "ORDER BY"},
		{"limit", "SELECT descriptiveStatistics(x) FROM t LIMIT 5", "LIMIT"},
		{"having", "SELECT descriptiveStatistics(x) FROM t GROUP BY g HAVING count() > 1", "HAVING"},
		{"positional group by", "SELECT descriptiveStatistics(x) FROM t GROUP BY 1", "positional"},
		{"rollup", "SELECT descriptiveStatistics(x) FROM t GROUP BY g WITH ROLLUP", "ROLLUP"},
		{"unknown estimator", "SELECT descriptiveStatistics('median', x) FROM t", "unknown estimator"},
		{"no arguments", "SELECT descriptiveStatistics() FROM t", "at least one column"},
		{"estimator only", "SELECT descriptiveStatistics('exact') FROM t", "at least one column"},
		{"parametric spelling", "SELECT descriptiveStatistics('gk')(x) FROM t", "parametric spelling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := expandDescriptiveStatistics(tt.input, testGrid)
			require.Error(t, err)
			require.Contains(t, ebtest.Text(t, err), tt.wantSub)
		})
	}
}

// TestExpandRealGridParses runs the shipped pass (87-level grid) end to end.
func TestExpandRealGridParses(t *testing.T) {
	got, err := ExpandDescriptiveStatistics.Run("SELECT descriptiveStatistics('exact', x, y) FROM t GROUP BY svc")
	require.NoError(t, err)
	_, err = nanopass.Parse(got)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(got, " UNION ALL "))
	require.Equal(t, 2, strings.Count(got, "quantilesExactInclusive("))
	require.Contains(t, got, "0.0001")   // deepest tail level present
	require.Contains(t, got, "0.984375") // dyadic mirror present (1 - 2^-6)
}

func TestExpandProperties(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	corpus := make([]string, 0, len(entries)+3)
	for _, e := range entries {
		corpus = append(corpus, e.SQL)
	}
	// The corpus carries no macro call, and AssertProperties rejects a
	// vacuous property — exercise the expansion explicitly.
	corpus = append(corpus,
		"SELECT descriptiveStatistics(x) FROM t",
		"SELECT descriptiveStatistics('gk', x, y) FROM t WHERE x > 0 GROUP BY svc",
		"WITH c AS (SELECT 1 AS v) SELECT descriptiveStatistics(v) FROM c",
	)
	nanopass.AssertProperties(t, ExpandDescriptiveStatistics, corpus)
}
