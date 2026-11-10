package analysis

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/require"
)

func TestClassifyQueryStratum(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected QueryStratumE
	}{
		{"star", `SELECT * FROM t`, QueryStratumVerbatim},
		{"star_limit_offset", `SELECT * FROM t LIMIT 10 OFFSET 5`, QueryStratumVerbatim},
		{"qualified_star", `SELECT t.* FROM db.t`, QueryStratumVerbatim},
		{"table_function_source", `SELECT * FROM keelson('env') LIMIT 12`, QueryStratumVerbatim},
		{"union_all_of_stars", `SELECT * FROM a UNION ALL SELECT * FROM b`, QueryStratumVerbatim},
		{"subquery_of_star", `SELECT * FROM (SELECT * FROM t)`, QueryStratumVerbatim},
		{"cte_of_star", `WITH q AS (SELECT * FROM t) SELECT * FROM q`, QueryStratumVerbatim},

		{"column_subset", `SELECT a, b FROM t`, QueryStratumRestrict},
		{"star_except", `SELECT * EXCEPT c FROM t`, QueryStratumRestrict},
		{"where", `SELECT * FROM t WHERE a = 1`, QueryStratumRestrict},
		{"where_with_function", `SELECT * FROM t WHERE has(tags, 'x')`, QueryStratumRestrict},
		{"prewhere", `SELECT * FROM t PREWHERE a = 1`, QueryStratumRestrict},
		{"order_by", `SELECT * FROM t ORDER BY ts DESC LIMIT 3`, QueryStratumRestrict},
		{"restrict_inside_verbatim", `SELECT * FROM (SELECT a FROM t WHERE b > 2)`, QueryStratumRestrict},
		{"union_all_mixed", `SELECT * FROM a UNION ALL SELECT * FROM b WHERE x = 1`, QueryStratumRestrict},

		{"alias", `SELECT a AS x FROM t`, QueryStratumDerive},
		{"expression", `SELECT a + 1 FROM t`, QueryStratumDerive},
		{"aggregate", `SELECT count() FROM t`, QueryStratumDerive},
		{"group_by", `SELECT a FROM t GROUP BY a`, QueryStratumDerive},
		{"distinct", `SELECT DISTINCT a FROM t`, QueryStratumDerive},
		{"join", `SELECT * FROM a JOIN b ON a.k = b.k`, QueryStratumDerive},
		{"array_join", `SELECT * FROM t ARRAY JOIN xs`, QueryStratumDerive},
		{"limit_by", `SELECT * FROM t ORDER BY ts DESC LIMIT 1 BY k`, QueryStratumDerive},
		{"final", `SELECT * FROM t FINAL`, QueryStratumDerive},
		{"sample", `SELECT * FROM t SAMPLE 1/10`, QueryStratumDerive},
		{"no_source", `SELECT 1`, QueryStratumDerive},
		{"union_distinct", `SELECT * FROM a UNION DISTINCT SELECT * FROM b`, QueryStratumDerive},
		{"derive_inside_verbatim", `SELECT * FROM (SELECT count() AS n FROM t)`, QueryStratumDerive},
		{"ambiguous_cte", `WITH q AS (SELECT * FROM a), q AS (SELECT * FROM b) SELECT * FROM q`, QueryStratumDerive},

		{"insert_wrapper", `INSERT INTO t SELECT * FROM s`, QueryStratumUnplaced},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pr, err := nanopass.Parse(tc.sql)
			require.NoError(t, err, "parse: %s", tc.sql)
			got, err := ClassifyQueryStratum(pr, "")
			require.NoError(t, err)
			require.Equal(t, tc.expected.String(), got.String(), tc.sql)
		})
	}
}

func TestClassifyQueryStratumRefusesAForeignTree(t *testing.T) {
	got, err := ClassifyQueryStratum(nil, "")
	require.Error(t, err)
	require.Equal(t, QueryStratumUnplaced, got)
}

// The zero value must be the answer that grants nothing, on every axis.
func TestClassificationZeroValueIsClosed(t *testing.T) {
	var c Classification
	require.Equal(t, KindUnknown, c.Kind)
	require.Equal(t, QuerySecurityMutating, c.Security)
	require.Equal(t, QueryStratumUnplaced, c.Stratum)
}

func TestClassify(t *testing.T) {
	t.Run("restricting read of a stored table", func(t *testing.T) {
		c := Classify(`SELECT a, b FROM db.t WHERE has(tags, 'x') ORDER BY a`, "")
		require.True(t, c.Parsed)
		require.Equal(t, KindReadOnly, c.Kind)
		require.Equal(t, QuerySecurityRead, c.Security)
		require.Equal(t, QueryStratumRestrict, c.Stratum)
		require.Equal(t, []string{"db.t"}, formatRefs(c.Passthrough))
		names := make([]string, 0, len(c.Functions))
		for _, f := range c.Functions {
			names = append(names, f.Name)
		}
		require.Contains(t, names, "has")
	})
	t.Run("a table function keeps its stratum and loses its tables", func(t *testing.T) {
		c := Classify(`SELECT * FROM url('http://example.invalid/x', 'ArrowStream')`, "")
		require.Equal(t, QuerySecurityReadEgress, c.Security)
		require.Equal(t, QueryStratumVerbatim, c.Stratum)
		require.Empty(t, c.Passthrough)
		require.NotEmpty(t, c.Witnesses)
	})
	t.Run("an aggregate is a read with no passthrough", func(t *testing.T) {
		c := Classify(`SELECT count() FROM t`, "")
		require.Equal(t, QuerySecurityRead, c.Security)
		require.Equal(t, QueryStratumDerive, c.Stratum)
		require.Empty(t, c.Passthrough)
	})
	t.Run("a setting change ahead of a star read is unplaced", func(t *testing.T) {
		c := Classify(`SET max_threads = 1; SELECT * FROM t`, "")
		require.True(t, c.Parsed)
		require.Equal(t, QuerySecurityMutating, c.Security)
		require.Equal(t, QueryStratumUnplaced, c.Stratum)
	})
	t.Run("a parameter prelude does not unplace the read", func(t *testing.T) {
		c := Classify(`SET param_n = 3; SELECT * FROM t LIMIT {n:UInt32}`, "")
		require.Equal(t, QuerySecurityRead, c.Security)
		require.Equal(t, QueryStratumVerbatim, c.Stratum)
	})
	t.Run("the insert wrapper", func(t *testing.T) {
		c := Classify(`INSERT INTO t SELECT * FROM s`, "")
		require.True(t, c.Parsed)
		require.Equal(t, KindMutating, c.Kind)
		require.Equal(t, QuerySecurityMutating, c.Security)
		require.Equal(t, QueryStratumUnplaced, c.Stratum)
	})
	t.Run("what the grammar does not read", func(t *testing.T) {
		for sql, kind := range map[string]KindE{
			`CREATE TABLE t (a UInt8) ENGINE = Memory`: KindMutating,
			`SHOW TABLES`:     KindReadOnly,
			`this is not sql`: KindUnknown,
			``:                KindUnknown,
		} {
			c := Classify(sql, "")
			require.False(t, c.Parsed, sql)
			require.Equal(t, kind, c.Kind, sql)
			require.Equal(t, QuerySecurityMutating, c.Security, sql)
			require.Equal(t, QueryStratumUnplaced, c.Stratum, sql)
		}
	})
}
