package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// TestColumnsIsAName checks that `columns` parses wherever a name can stand,
// as ClickHouse accepts it, and that SLL and LL agree on every such tree.
func TestColumnsIsAName(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"alias_and_order", "SELECT database, count() AS columns FROM system.columns GROUP BY database ORDER BY columns DESC"},
		{"bare_column", "SELECT columns FROM t"},
		{"upper_alias", "SELECT 1 AS COLUMNS"},
		{"with_alias", "WITH 1 AS columns SELECT columns + 1"},
		{"table_alias", "SELECT columns.name FROM system.columns AS columns"},
		{"qualifier", "SELECT columns.name, system.columns.type FROM system.columns"},
		{"lambda_arg", "SELECT arrayMap(columns -> columns + 1, [1, 2])"},
		{"except_list", "SELECT * EXCEPT columns FROM t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := nanopass.Parse(tc.sql)
			require.NoError(t, err)
			llTree, llClean := parseAtMode(tc.sql, antlr.PredictionModeLL)
			require.True(t, llClean)
			sllTree, sllClean := parseAtMode(tc.sql, antlr.PredictionModeSLL)
			require.True(t, sllClean)
			assert.Equal(t, llTree, sllTree)
		})
	}
}

// TestColumnsCallIsDynamicSelection checks that `COLUMNS('re')` stays a
// dynamic column selection and is never read as a call of a function named
// columns, now that COLUMNS is also a name.
func TestColumnsCallIsDynamicSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		rule string
	}{
		{"projection", "SELECT COLUMNS('^a') FROM t", "(columnExpr (dynamicColumnSelection COLUMNS ( '^a' )))"},
		{"lower_case", "SELECT columns('^a') FROM t", "(columnExpr (dynamicColumnSelection columns ( '^a' )))"},
		{"except", "SELECT * EXCEPT COLUMNS('^b') FROM t", "(staticOrDynamicColumnSelection (dynamicColumnSelection COLUMNS ( '^b' )))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []int{antlr.PredictionModeSLL, antlr.PredictionModeLL} {
				tree, clean := parseAtMode(tc.sql, mode)
				require.True(t, clean)
				assert.Contains(t, tree, tc.rule)
				assert.NotContains(t, tree, "columnArgList")
			}
			pr, err := nanopass.Parse(tc.sql)
			require.NoError(t, err)
			found := false
			nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
				switch ctx.(type) {
				case *grammar1.ColumnExprDynamicContext, *grammar1.DynamicColumnListContext:
					found = true
				case *grammar1.ColumnExprFunctionContext:
					t.Errorf("COLUMNS(...) parsed as a function call")
				}
				return true
			})
			assert.True(t, found)
		})
	}
}
