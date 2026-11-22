package analysis_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// The condition of an aggregate filter is part of the statement like any
// other expression: in each statement below the only part that reaches out
// or changes state sits inside FILTER (WHERE …), so a classifier that skipped
// the condition would call it a plain read. The canonical form, the If call
// the server runs, classifies alike.
func TestAggregateFilterConditionIsClassified(t *testing.T) {
	cases := []struct {
		name      string
		sql       string
		class     analysis.QuerySecurityClassE
		witnesses []string
	}{
		{"plain", "SELECT quantile(0.05)(L) FILTER (WHERE L BETWEEN 177 AND 1502) AS p05 FROM t",
			analysis.QuerySecurityRead, nil},
		{"url_in_subquery", "SELECT sum(x) FILTER (WHERE y IN (SELECT a FROM url('http://h/x', 'CSV', 'a String'))) FROM t",
			analysis.QuerySecurityReadEgress, []string{"url"}},
		{"remote_in_scalar_subquery", "SELECT count() FILTER (WHERE y > (SELECT max(a) FROM remote('h', 'db', 'u'))) FROM t",
			analysis.QuerySecurityReadEgress, []string{"remote"}},
		{"file_scalar", "SELECT count(*) FILTER (WHERE file('/etc/hostname') != '') FROM t",
			analysis.QuerySecurityReadEgress, []string{"file"}},
		{"ai_scalar_parametric", "SELECT quantile(0.5)(x) FILTER (WHERE aiFilter(note, 'is spam') = 1) FROM t",
			analysis.QuerySecurityReadEgress, []string{"aifilter"}},
		{"state_changing_scalar", "SELECT sum(x) FILTER (WHERE generateSerialID('s') > 0) FROM t",
			analysis.QuerySecurityMutating, []string{"generateserialid"}},
		{"window", "SELECT sum(x) FILTER (WHERE y IN (SELECT a FROM s3('http://h/x', 'CSV'))) OVER (ORDER BY z) FROM t",
			analysis.QuerySecurityReadEgress, []string{"s3"}},
		{"named_window", "SELECT sum(x) FILTER (WHERE y IN (SELECT a FROM s3('http://h/x', 'CSV'))) OVER w FROM t WINDOW w AS (ORDER BY z)",
			analysis.QuerySecurityReadEgress, []string{"s3"}},
		{"distinct_nested", "SELECT count(DISTINCT x) FILTER (WHERE y IN (SELECT a FROM u WHERE b IN (SELECT b FROM url('http://h/x', 'CSV')))) FROM t",
			analysis.QuerySecurityReadEgress, []string{"url"}},
	}
	canonical := passes.CanonicalizeFull(16)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, names := securityOf(t, tc.sql)
			assert.Equal(t, tc.class, class)
			assert.Equal(t, tc.witnesses, names)

			canon, err := canonical.Run(tc.sql)
			require.NoError(t, err)
			require.NotContains(t, canon, "FILTER")
			canonClass, canonNames := securityOf(t, canon)
			assert.Equal(t, tc.class, canonClass, "canonical: %s", canon)
			assert.Equal(t, tc.witnesses, canonNames, "canonical: %s", canon)
		})
	}
}

// A filter on a call the vocabulary names is witnessed by that name, read
// fail-closed as the call written. The server reads fileIf, which does not
// exist (-If combines aggregates only), so the canonical form, which names
// fileIf, is a plain read the server refuses.
func TestAggregateFilterOnAVocabularyName(t *testing.T) {
	class, names := securityOf(t, "SELECT file('/etc/hostname') FILTER (WHERE 1)")
	assert.Equal(t, analysis.QuerySecurityReadEgress, class)
	assert.Equal(t, []string{"file"}, names)
}

func TestAggregateFilterStatementKind(t *testing.T) {
	assert.Equal(t, analysis.KindReadOnly,
		analysis.ClassifyStatementKind("SELECT quantile(0.05)(L) FILTER (WHERE L BETWEEN 177 AND 1502) AS p05 FROM t"))
	// A nested block comment makes the server's reading differ from the
	// lexer's, inside the condition as anywhere.
	assert.Equal(t, analysis.KindUnknown,
		analysis.ClassifyStatementKind("SELECT sum(x) FILTER (WHERE /* /* */ y > 0 */ 1) FROM t"))
}

// A table read only inside the condition's subquery is a table the statement
// reads; a column named only in the condition is a column it reads.
func TestAggregateFilterReadsAreReads(t *testing.T) {
	sql := "SELECT a, sum(x) FILTER (WHERE y IN (SELECT b FROM db.u) AND z > {p:UInt8}) FROM t GROUP BY a"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	assert.Contains(t, analysis.ExtractTables(pr), analysis.TableRef{Database: "db", Table: "u"})

	var columns []string
	for _, c := range analysis.ExtractColumns(pr) {
		columns = append(columns, c.Column)
	}
	assert.Contains(t, columns, "y")
	assert.Contains(t, columns, "z")

	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].Subqueries, 1, "the condition's subquery is a scope of the statement")
	sub := scopes[0].Subqueries[0]
	require.Len(t, sub.Tables, 1)
	assert.Equal(t, "db", sub.Tables[0].Database)
	assert.Equal(t, "u", sub.Tables[0].Table)

	e, _, err := grammarenv.Extract(sql)
	require.NoError(t, err)
	_, slot := e.Params["p"]
	assert.True(t, slot, "the condition's parameter slot is a parameter of the statement")
	assert.True(t, slices.ContainsFunc(nanopass.FlattenScopes(scopes), func(s *nanopass.SelectScope) bool { return s == sub }))
}
