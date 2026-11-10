package analysis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatementShape(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{"literals", `SELECT a FROM t WHERE b = 'x' AND c > 3`, `SELECT a FROM t WHERE b = ? AND c > ?`},
		{"layout and comments", "SELECT a -- pick\n  FROM   t /* here */ WHERE b = 1.5", `SELECT a FROM t WHERE b = ?`},
		{"in list", `SELECT a FROM t WHERE b IN (1, 2, 3)`, `SELECT a FROM t WHERE b IN ( ?,… )`},
		{"in list of one", `SELECT a FROM t WHERE b IN (1)`, `SELECT a FROM t WHERE b IN ( ? )`},
		{"array literal", `SELECT has(['a','b'], x) FROM t`, `SELECT has ( [ ?,… ] , x ) FROM t`},
		{"literal then column", `SELECT 1, a FROM t`, `SELECT ? , a FROM t`},
		{"function arguments", `SELECT f(a, 1, 2) FROM t`, `SELECT f ( a , ?,… ) FROM t`},
		{"limit offset", `SELECT * FROM t LIMIT 10 OFFSET 5`, `SELECT * FROM t LIMIT ? OFFSET ?`},
		{"ddl the grammar does not read", `CREATE TABLE t (a UInt8) ENGINE = MergeTree ORDER BY a`, `CREATE TABLE t ( a UInt8 ) ENGINE = MergeTree ORDER BY a`},
		{"kill by id", `KILL QUERY WHERE query_id='abc' ASYNC`, `KILL QUERY WHERE query_id = ? ASYNC`},
		{"empty", ``, ``},
		{"only a comment", `-- nothing`, ``},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, StatementShape(tc.sql))
		})
	}
}

func TestStatementShapeFoldsWhatOneTemplateSends(t *testing.T) {
	a := StatementShape(`SELECT x FROM db.t WHERE id IN (1, 2) AND name = 'alpha' LIMIT 10`)
	b := StatementShape("SELECT x\nFROM db.t\nWHERE id IN (7, 8, 9, 10) AND name = 'it''s' LIMIT 500")
	require.Equal(t, a, b)
	require.NotEqual(t, a, StatementShape(`SELECT x FROM db.u WHERE id IN (1, 2) AND name = 'alpha' LIMIT 10`))
}

func TestStatementShapeOfOversizedInputIsEmpty(t *testing.T) {
	require.Empty(t, StatementShape("SELECT "+strings.Repeat("1,", 8<<20)+"1"))
}
