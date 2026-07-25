package analysis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyStatementKind(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want KindE
	}{
		// Proven reads: Grammar1's SELECT surface.
		{"select", "SELECT 1", KindReadOnly},
		{"select from table", "SELECT a FROM db.t WHERE a > 1", KindReadOnly},
		{"with select", "WITH x AS (SELECT 1) SELECT * FROM x", KindReadOnly},
		{"union", "SELECT 1 UNION ALL SELECT 2", KindReadOnly},
		{"keelson macro", "SELECT * FROM keelson('env')", KindReadOnly},
		{"settings prelude configures the read", "SET max_threads=1; SELECT 1", KindReadOnly},
		{"param prelude", "SET param_a=1; SELECT {a:UInt64}", KindReadOnly},
		{"leading comment", "-- why\nSELECT 1", KindReadOnly},

		// Reads Grammar1 cannot parse, recognised by leading keyword.
		{"explain", "EXPLAIN SELECT 1", KindReadOnly},
		{"explain ast", "EXPLAIN AST SELECT 1", KindReadOnly},
		{"explain of a mutation still only plans it", "EXPLAIN AST INSERT INTO t VALUES (1)", KindReadOnly},
		{"show", "SHOW TABLES", KindReadOnly},
		{"describe", "DESCRIBE TABLE t", KindReadOnly},
		{"desc", "DESC t", KindReadOnly},
		{"exists", "EXISTS TABLE t", KindReadOnly},
		{"lowercase keyword", "show tables", KindReadOnly},
		{"block comment ahead of the keyword", "/* lead */ SHOW TABLES", KindReadOnly},

		// Mutations.
		{"insert values", "INSERT INTO t VALUES (1)", KindMutating},
		{"insert select", "INSERT INTO t SELECT 1", KindMutating},
		{"create table", "CREATE TABLE t (a Int64) ENGINE=Memory", KindMutating},
		{"create function", "CREATE FUNCTION f AS x -> x+1", KindMutating},
		{"drop", "DROP TABLE t", KindMutating},
		{"alter delete", "ALTER TABLE t DELETE WHERE 1", KindMutating},
		{"truncate", "TRUNCATE TABLE t", KindMutating},
		{"rename", "RENAME TABLE a TO b", KindMutating},
		{"attach", "ATTACH TABLE t", KindMutating},
		{"detach", "DETACH TABLE t", KindMutating},
		{"optimize", "OPTIMIZE TABLE t FINAL", KindMutating},
		{"kill", "KILL QUERY WHERE query_id='x'", KindMutating},
		{"set alone", "SET max_threads=1", KindMutating},
		{"use", "USE db", KindMutating},
		{"system", "SYSTEM RELOAD CONFIG", KindMutating},
		{"lowercase mutation", "insert into t values (1)", KindMutating},
		{"comment ahead of the mutation", "/* c */ DROP TABLE t", KindMutating},

		// Not provable either way.
		{"empty", "", KindUnknown},
		{"whitespace only", "   \n\t ", KindUnknown},
		{"comment only", "-- nothing here", KindUnknown},
		{"garbage", "NOT SQL ((", KindUnknown},
		{"bare word", "hello", KindUnknown},
		{"unlexed statement keyword", "GRANT SELECT ON db.* TO u", KindUnknown},
		{"truncated select", "SELECT", KindUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ClassifyStatementKind(tc.sql), "sql=%q", tc.sql)
		})
	}
}

// TestClassifyStatementKindZeroValueIsUnknown pins the default-deny property
// the doc comment promises: a KindE nobody assigned must not read as a read.
func TestClassifyStatementKindZeroValueIsUnknown(t *testing.T) {
	var zero KindE
	assert.Equal(t, KindUnknown, zero)
	assert.NotEqual(t, KindReadOnly, zero)
}

// TestClassifyStatementKindOversizedIsUnknown covers the input-guard path:
// past MaxInputBytes there is no proof to be had, so the answer is unknown
// even though the statement is plainly a SELECT.
func TestClassifyStatementKindOversizedIsUnknown(t *testing.T) {
	huge := "SELECT '" + strings.Repeat("x", 2*1024*1024) + "'"
	assert.Equal(t, KindUnknown, ClassifyStatementKind(huge))
}

func TestKindString(t *testing.T) {
	assert.Equal(t, "read-only", KindReadOnly.String())
	assert.Equal(t, "mutating", KindMutating.String())
	assert.Equal(t, "unknown", KindUnknown.String())
	assert.Equal(t, "unknown", KindE(200).String())
}
