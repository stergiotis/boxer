package analysis

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitStatements(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		want []string
	}{
		{"one statement", "SELECT 1", []string{"SELECT 1"}},
		{"a trailing separator", "SELECT 1;\n", []string{"SELECT 1"}},
		{"a table then a read", "CREATE TEMPORARY TABLE t AS SELECT 1 AS a;\nSELECT a FROM t", []string{"CREATE TEMPORARY TABLE t AS SELECT 1 AS a", "SELECT a FROM t"}},
		{"a separator in a string", "SELECT 'a;b'; SELECT 2", []string{"SELECT 'a;b'", "SELECT 2"}},
		{"a separator in a quoted name", "SELECT `a;b` FROM t; SELECT 2", []string{"SELECT `a;b` FROM t", "SELECT 2"}},
		{"a separator in a comment", "SELECT 1 -- no; cut\n; SELECT 2", []string{"SELECT 1 -- no; cut", "SELECT 2"}},
		{"runes before the cut", "SELECT 'é→'; SELECT 2", []string{"SELECT 'é→'", "SELECT 2"}},
		{"empty statements", ";; SELECT 1 ;;", []string{"SELECT 1"}},
		{"nothing", "  ", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, SplitStatements(tc.sql))
		})
	}
}
