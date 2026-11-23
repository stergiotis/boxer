package analysis_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// The body of a common table expression marked MATERIALIZED is part of the
// statement like the body of any other: in each statement below the only
// part that reaches out or changes state sits in a marked body, so a
// classifier that skipped it would call the statement a plain read. The
// canonical form keeps the marker and classifies alike.
func TestMaterializedCTEBodyIsClassified(t *testing.T) {
	cases := []struct {
		name      string
		sql       string
		class     analysis.QuerySecurityClassE
		witnesses []string
	}{
		{"plain", "WITH a AS MATERIALIZED (SELECT k FROM t), b AS MATERIALIZED (SELECT k FROM a) SELECT count() FROM b",
			analysis.QuerySecurityRead, nil},
		{"url_table_function", "WITH a AS MATERIALIZED (SELECT k FROM url('http://h/x', 'CSV', 'k String')) SELECT count() FROM a",
			analysis.QuerySecurityReadEgress, []string{"url"}},
		{"second_definition", "WITH a AS MATERIALIZED (SELECT k FROM t), b AS materialized (SELECT k FROM remote('h', 'db', 'u')) SELECT count() FROM a",
			analysis.QuerySecurityReadEgress, []string{"remote"}},
		{"unreferenced", "WITH a AS MATERIALIZED (SELECT k FROM s3('http://h/x', 'CSV')) SELECT 1",
			analysis.QuerySecurityReadEgress, []string{"s3"}},
		{"file_scalar", "WITH a AS MATERIALIZED (SELECT file('/etc/hostname') AS k) SELECT k FROM a",
			analysis.QuerySecurityReadEgress, []string{"file"}},
		{"state_changing_scalar", "WITH a AS MATERIALIZED (SELECT generateSerialID('s') AS k) SELECT k FROM a",
			analysis.QuerySecurityMutating, []string{"generateserialid"}},
		{"nested_definition", "WITH a AS MATERIALIZED (WITH i AS MATERIALIZED (SELECT k FROM mysql('h:3306', 'db', 'u', 'usr', 'pw')) SELECT k FROM i) SELECT k FROM a",
			analysis.QuerySecurityReadEgress, []string{"mysql"}},
		{"in_a_subquery", "SELECT x IN (WITH a AS MATERIALIZED (SELECT k FROM hdfs('hdfs://h/x', 'CSV')) SELECT k FROM a) FROM t",
			analysis.QuerySecurityReadEgress, []string{"hdfs"}},
		{"in_an_exists", "SELECT exists(WITH a AS MATERIALIZED (SELECT k FROM url('http://h/x', 'CSV')) SELECT k FROM a)",
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
			require.Contains(t, canon, "AS MATERIALIZED (")
			canonClass, canonNames := securityOf(t, canon)
			assert.Equal(t, tc.class, canonClass, "canonical: %s", canon)
			assert.Equal(t, tc.witnesses, canonNames, "canonical: %s", canon)
		})
	}
}

// The marker takes a query and nothing else: a write in its parentheses
// does not parse, and the statement has the closed class and no proven kind.
// Under the INSERT wrapper a statement with a marked definition is the
// write it was.
func TestMaterializedCTEHoldsNoWrite(t *testing.T) {
	for _, sql := range []string{
		"WITH a AS MATERIALIZED (INSERT INTO t SELECT 1) SELECT 1",
		"WITH a AS MATERIALIZED (CREATE TABLE t (k UInt8) ENGINE = Memory) SELECT 1",
		"WITH a AS MATERIALIZED (SELECT 1 INTO OUTFILE '/tmp/x') SELECT * FROM a",
		"WITH a AS MATERIALIZED (SELECT 1); DROP TABLE t",
		"WITH a AS NOT MATERIALIZED (SELECT 1) SELECT * FROM a",
	} {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, sql)
		assert.Equal(t, analysis.KindUnknown, analysis.ClassifyStatementKind(sql), sql)
		c := analysis.Classify(sql, "")
		assert.False(t, c.Parsed, sql)
		assert.Equal(t, analysis.QuerySecurityMutating, c.Security, sql)
	}
	assert.Equal(t, analysis.KindReadOnly,
		analysis.ClassifyStatementKind("WITH a AS MATERIALIZED (SELECT k FROM t) SELECT count() FROM a"))
	assert.Equal(t, analysis.KindUnknown,
		analysis.ClassifyStatementKind("WITH a AS MATERIALIZED (SELECT /* /* */ 1 FROM u */ 1) SELECT * FROM a"))
	assert.Equal(t, analysis.KindMutating,
		analysis.ClassifyStatementKind("INSERT INTO t WITH a AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM a"))
}

// A table read only in a marked body is a table the statement reads; the
// name the definition gives is a common table expression, not a table; and
// reading the definition whole is still a passthrough of its table.
func TestMaterializedCTEReadsAreReads(t *testing.T) {
	sql := "WITH a AS MATERIALIZED (SELECT k FROM db.u WHERE v > 1), b AS MATERIALIZED (SELECT k FROM a) SELECT count() FROM b"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	assert.Contains(t, analysis.ExtractTables(pr), analysis.TableRef{Database: "db", Table: "u"})

	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	var real []string
	for _, sc := range nanopass.FlattenScopes(scopes) {
		for _, ts := range sc.Tables {
			if !ts.IsCTE && !ts.IsSubquery && !ts.IsFunction {
				real = append(real, ts.Database+"."+ts.Table)
			}
		}
	}
	assert.Equal(t, []string{"db.u"}, real, "a and b are definitions, db.u is the one table")

	canon, err := passes.CanonicalizeFull(16).Run(sql)
	require.NoError(t, err)
	cpr, err := nanopass.Parse(canon)
	require.NoError(t, err)
	assert.Equal(t, analysis.ExtractTables(pr), analysis.ExtractTables(cpr), canon)

	for _, marker := range []string{"", "MATERIALIZED "} {
		whole := "WITH a AS " + marker + "(SELECT * FROM db.u) SELECT * FROM a"
		c := analysis.Classify(whole, "")
		plain := analysis.Classify("WITH a AS (SELECT * FROM db.u) SELECT * FROM a", "")
		assert.Equal(t, plain.Stratum, c.Stratum, whole)
		assert.Equal(t, plain.Passthrough, c.Passthrough, whole)
		assert.Equal(t, plain.Security, c.Security, whole)
	}
}
