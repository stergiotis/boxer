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

// The query of an EXISTS is part of the statement like any other subquery:
// in each statement below the only part that reaches out or changes state
// sits inside the predicate, so a classifier that skipped it would call the
// statement a plain read. The canonical form, the call the server holds,
// classifies alike.
func TestExistsQueryIsClassified(t *testing.T) {
	cases := []struct {
		name      string
		sql       string
		class     analysis.QuerySecurityClassE
		witnesses []string
	}{
		{"plain", "SELECT a FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.k = t.k)",
			analysis.QuerySecurityRead, nil},
		{"url_table_function", "SELECT a FROM t WHERE EXISTS (SELECT 1 FROM url('http://h/x', 'CSV', 'a String'))",
			analysis.QuerySecurityReadEgress, []string{"url"}},
		{"call_spelling_remote", "SELECT exists(SELECT 1 FROM remote('h', 'db', 'u')) AS e",
			analysis.QuerySecurityReadEgress, []string{"remote"}},
		{"select_star_from_s3", "SELECT exists(SELECT * FROM s3('http://h/x', 'CSV'))",
			analysis.QuerySecurityReadEgress, []string{"s3"}},
		{"file_scalar", "SELECT a FROM t WHERE NOT EXISTS (SELECT 1 WHERE file('/etc/hostname') != '')",
			analysis.QuerySecurityReadEgress, []string{"file"}},
		{"ai_scalar", "SELECT exists(SELECT 1 FROM u WHERE aiFilter(note, 'is spam') = 1)",
			analysis.QuerySecurityReadEgress, []string{"aifilter"}},
		{"state_changing_scalar", "SELECT exists(SELECT generateSerialID('s'))",
			analysis.QuerySecurityMutating, []string{"generateserialid"}},
		{"nested", "SELECT exists(SELECT 1 FROM u WHERE EXISTS (SELECT 1 FROM v WHERE b IN (SELECT b FROM url('http://h/x', 'CSV'))))",
			analysis.QuerySecurityReadEgress, []string{"url"}},
		{"in_a_union_arm", "SELECT exists(SELECT 1 FROM u UNION ALL SELECT 1 FROM mysql('h:3306', 'db', 'u', 'usr', 'pw'))",
			analysis.QuerySecurityReadEgress, []string{"mysql"}},
		{"in_a_with", "SELECT exists(WITH c AS (SELECT 1 FROM hdfs('hdfs://h/x', 'CSV')) SELECT * FROM c)",
			analysis.QuerySecurityReadEgress, []string{"hdfs"}},
		{"inside_an_aggregate_filter", "SELECT count() FILTER (WHERE EXISTS (SELECT 1 FROM url('http://h/x', 'CSV'))) FROM t",
			analysis.QuerySecurityReadEgress, []string{"url"}},
		{"own_parentheses", "SELECT exists((SELECT 1 FROM url('http://h/x', 'CSV')))",
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
			require.Contains(t, canon, `"exists"((`)
			canonClass, canonNames := securityOf(t, canon)
			assert.Equal(t, tc.class, canonClass, "canonical: %s", canon)
			assert.Equal(t, tc.witnesses, canonNames, "canonical: %s", canon)
		})
	}
}

// Nothing but a query stands in the parentheses: a write there does not
// parse, so the statement has no class but the closed one and no kind but
// unknown.
func TestExistsHoldsNoWrite(t *testing.T) {
	for _, sql := range []string{
		"SELECT exists(INSERT INTO t SELECT 1)",
		"SELECT exists(INSERT INTO t VALUES (1))",
		"SELECT exists(SELECT 1 INTO OUTFILE '/tmp/x')",
		"SELECT exists(DROP TABLE t)",
		"SELECT exists(SET max_threads = 1; SELECT 1)",
		"SELECT exists(SELECT 1); DROP TABLE t",
	} {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, sql)
		assert.Equal(t, analysis.KindUnknown, analysis.ClassifyStatementKind(sql), sql)
		c := analysis.Classify(sql, "")
		assert.False(t, c.Parsed, sql)
		assert.Equal(t, analysis.QuerySecurityMutating, c.Security, sql)
	}
}

func TestExistsStatementKind(t *testing.T) {
	assert.Equal(t, analysis.KindReadOnly,
		analysis.ClassifyStatementKind("SELECT a FROM t WHERE exists(SELECT 1 FROM u WHERE u.k = t.k)"))
	// A nested block comment makes the server's reading differ from the
	// lexer's, inside the predicate as anywhere.
	assert.Equal(t, analysis.KindUnknown,
		analysis.ClassifyStatementKind("SELECT exists(SELECT /* /* */ 1 FROM u */ 1)"))
	// The INSERT wrapper around a statement with EXISTS is still a write.
	assert.Equal(t, analysis.KindMutating,
		analysis.ClassifyStatementKind("INSERT INTO t SELECT a FROM u WHERE EXISTS (SELECT 1 FROM v)"))
}

// A table read only inside an EXISTS is a table the statement reads, in
// every spelling — also the one whose words read as an expression, a product
// of columns named SELECT and FROM — and is not a table whose rows the
// statement returns.
func TestExistsReadsAreReads(t *testing.T) {
	for _, sql := range []string{
		"SELECT a FROM t WHERE EXISTS (SELECT 1 FROM db.u WHERE u.k = t.k AND u.z > {p:UInt8})",
		"SELECT a FROM t WHERE exists(SELECT * FROM db.u)",
		"SELECT a FROM t WHERE NOT exists((SELECT * FROM db.u))",
		"SELECT a, exists(SELECT * FROM db.u) FROM t",
	} {
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err, sql)

		assert.ElementsMatch(t, []analysis.TableRef{{Table: "t"}, {Database: "db", Table: "u"}}, analysis.ExtractTables(pr), sql)

		scopes, err := nanopass.BuildScopes(pr, "")
		require.NoError(t, err, sql)
		require.Len(t, scopes, 1, sql)
		require.Len(t, scopes[0].Subqueries, 1, "the query of the EXISTS is a scope of the statement: %s", sql)
		sub := scopes[0].Subqueries[0]
		require.Len(t, sub.Tables, 1, sql)
		assert.Equal(t, "db", sub.Tables[0].Database, sql)
		assert.Equal(t, "u", sub.Tables[0].Table, sql)
		assert.True(t, slices.ContainsFunc(nanopass.FlattenScopes(scopes), func(s *nanopass.SelectScope) bool { return s == sub }), sql)

		passthrough, err := analysis.ExtractPassthroughTables(pr, "")
		require.NoError(t, err, sql)
		assert.NotContains(t, passthrough, analysis.TableRef{Database: "db", Table: "u"}, sql)

		canon, err := passes.CanonicalizeFull(16).Run(sql)
		require.NoError(t, err, sql)
		cpr, err := nanopass.Parse(canon)
		require.NoError(t, err, canon)
		assert.Equal(t, analysis.ExtractTables(pr), analysis.ExtractTables(cpr), canon)
	}

	sql := "SELECT a FROM t WHERE EXISTS (SELECT 1 FROM db.u WHERE u.k = t.k AND u.z > {p:UInt8})"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	var columns []string
	for _, c := range analysis.ExtractColumns(pr) {
		columns = append(columns, c.Column)
	}
	assert.Contains(t, columns, "k")
	assert.Contains(t, columns, "z")
	e, _, err := grammarenv.Extract(sql)
	require.NoError(t, err)
	_, slot := e.Params["p"]
	assert.True(t, slot, "a parameter slot inside the EXISTS is a parameter of the statement")
}

// The predicate is the call exists to whoever asks which functions a
// statement calls, in the authored text and in its canonical form.
func TestExistsIsTheCallExists(t *testing.T) {
	for _, sql := range []string{
		"SELECT a FROM t WHERE EXISTS (SELECT 1 FROM u)",
		"SELECT a FROM t WHERE Exists(SELECT 1 FROM u)",
		"SELECT a FROM t WHERE exists((SELECT 1 FROM u))",
	} {
		c := analysis.Classify(sql, "")
		require.True(t, c.Parsed, sql)
		assert.Equal(t, analysis.KindReadOnly, c.Kind, sql)
		assert.Equal(t, analysis.QuerySecurityRead, c.Security, sql)
		assert.Equal(t, []analysis.FunctionRef{{Name: "exists"}}, c.Functions, sql)

		canon, err := passes.CanonicalizeFull(16).Run(sql)
		require.NoError(t, err, sql)
		cc := analysis.Classify(canon, "")
		assert.Equal(t, c.Functions, cc.Functions, canon)
		assert.Equal(t, c.Stratum, cc.Stratum, canon)
	}
}
