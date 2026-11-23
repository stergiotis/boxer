package analysis_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// Every ARRAY JOIN clause of a statement is read, not the first alone: in
// each statement below the only part that reaches out or changes state sits
// in the second or third clause. The canonical form classifies alike.
func TestEveryArrayJoinClauseIsClassified(t *testing.T) {
	cases := []struct {
		name      string
		sql       string
		class     analysis.QuerySecurityClassE
		witnesses []string
	}{
		{"plain", "SELECT x, T, tau FROM t ARRAY JOIN [1.0, 1.5, 2.0] AS T ARRAY JOIN [0.5, 0.7] AS tau",
			analysis.QuerySecurityRead, nil},
		{"subquery_in_second", "SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN (SELECT groupArray(k) FROM url('http://h/x', 'CSV', 'k UInt8')) AS y",
			analysis.QuerySecurityReadEgress, []string{"url"}},
		{"lambda_in_third", "SELECT x FROM t ARRAY JOIN a AS x LEFT ARRAY JOIN b AS y ARRAY JOIN arrayFilter(i -> i IN (SELECT k FROM remote('h', 'db', 'u')), c) AS z",
			analysis.QuerySecurityReadEgress, []string{"remote"}},
		{"file_scalar_in_second", "SELECT x FROM t ARRAY JOIN a AS x INNER ARRAY JOIN splitByChar(',', file('/etc/hostname')) AS y",
			analysis.QuerySecurityReadEgress, []string{"file"}},
		{"state_changing_in_second", "SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN [generateSerialID('s')] AS y",
			analysis.QuerySecurityMutating, []string{"generateserialid"}},
		{"exists_in_second", "SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN [exists(SELECT 1 FROM s3('http://h/x', 'CSV'))] AS y",
			analysis.QuerySecurityReadEgress, []string{"s3"}},
	}
	canonical := passes.CanonicalizeFull(16)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, names := securityOf(t, tc.sql)
			assert.Equal(t, tc.class, class)
			assert.Equal(t, tc.witnesses, names)

			canon, err := canonical.Run(tc.sql)
			require.NoError(t, err)
			_, err = nanopass.ParseCanonical(canon)
			require.NoError(t, err, canon)
			canonClass, canonNames := securityOf(t, canon)
			assert.Equal(t, tc.class, canonClass, "canonical: %s", canon)
			assert.Equal(t, tc.witnesses, canonNames, "canonical: %s", canon)
		})
	}
}

// A table read only in a later ARRAY JOIN clause is a table the statement
// reads and its subquery a scope of the statement; a column named only
// there is a column it reads; and a statement with any ARRAY JOIN returns no
// table's rows as they are.
func TestEveryArrayJoinClauseIsRead(t *testing.T) {
	sql := "SELECT * FROM t ARRAY JOIN a AS x ARRAY JOIN arrayFilter(i -> i IN (SELECT k FROM db.u), b) AS y LEFT ARRAY JOIN (SELECT groupArray(k) FROM db.v) AS z"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	assert.ElementsMatch(t, []analysis.TableRef{{Table: "t"}, {Database: "db", Table: "u"}, {Database: "db", Table: "v"}}, analysis.ExtractTables(pr))

	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	var inner []string
	for _, sub := range scopes[0].Subqueries {
		for _, ts := range sub.Tables {
			inner = append(inner, ts.Database+"."+ts.Table)
		}
	}
	assert.ElementsMatch(t, []string{"db.u", "db.v"}, inner, "the subqueries of the later clauses are scopes of the statement")

	var columns []string
	for _, c := range analysis.ExtractColumns(pr) {
		columns = append(columns, c.Column)
	}
	assert.Contains(t, columns, "b")

	for _, s := range []string{
		"SELECT * FROM t ARRAY JOIN a AS x",
		"SELECT * FROM t ARRAY JOIN a AS x ARRAY JOIN b AS y",
	} {
		c := analysis.Classify(s, "")
		require.True(t, c.Parsed, s)
		assert.Equal(t, analysis.KindReadOnly, c.Kind, s)
		assert.Empty(t, c.Passthrough, s)
	}
	one := analysis.Classify("SELECT * FROM t ARRAY JOIN a AS x", "")
	two := analysis.Classify("SELECT * FROM t ARRAY JOIN a AS x ARRAY JOIN b AS y", "")
	assert.Equal(t, one.Stratum, two.Stratum)

	assert.Equal(t, analysis.KindUnknown,
		analysis.ClassifyStatementKind("SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN (INSERT INTO t SELECT 1) AS y"))
	assert.Equal(t, analysis.KindUnknown,
		analysis.ClassifyStatementKind("SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN /* /* */ b */ c AS y"))
}
