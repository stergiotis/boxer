package aliases

import (
	"iter"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

func ExtractColumnExprAlias(node *grammar1.ColumnExprAliasContext) (funcName string, aliasName string, hasExplicitAlias bool) {
	if node.Identifier() != nil {
		rawAlias := node.Identifier().GetText()
		aliasName = unquoteIdentifier(rawAlias)
		hasExplicitAlias = true
	} else if node.Alias() != nil {
		aliasName = unquoteIdentifier(node.Alias().GetText())
		hasExplicitAlias = true
	} else {
		colExpr := node.ColumnExpr()
		id, ok := colExpr.(*grammar1.ColumnExprIdentifierContext)
		if ok {
			aliasName = unquoteIdentifier(id.GetText())
			hasExplicitAlias = true
		}
	}

	if !hasExplicitAlias {
		return
	}

	colExpr := node.ColumnExpr()
	fun, ok := colExpr.(*grammar1.ColumnExprFunctionContext)
	if ok {
		if fun.Identifier() != nil {
			funcName = unquoteIdentifier(fun.Identifier().GetText())
		}
	}
	return
}
func IterateAllAliases(tree *grammar1.QueryStmtContext) iter.Seq2[ /*function*/ string /*alias*/, string] {
	return func(yield func(string, string) bool) {
		for node := range antlr4utils.IterateAllByType[*grammar1.ColumnExprAliasContext](tree) {
			funcName, aliasName, _ := ExtractColumnExprAlias(node)
			if !yield(funcName, aliasName) {
				return
			}
		}
	}

}

// FIXME merge with ast
func unquoteIdentifier(s string) (unquoted string) {
	if len(s) < 2 {
		return s
	}

	first := s[0]
	last := s[len(s)-1]
	quoteChar := byte(0)

	if first == '`' && last == '`' {
		quoteChar = '`'
	} else if first == '"' && last == '"' {
		quoteChar = '"'
	} else if first == '\'' && last == '\'' {
		quoteChar = '\''
	}

	if quoteChar != 0 {
		unquoted = s[1 : len(s)-1]
		// Handle Escapes: ClickHouse escapes quotes by doubling them or using backslash
		// simplistic approach: replace doubled quotes
		if quoteChar == '`' {
			unquoted = strings.ReplaceAll(unquoted, "``", "`")
		} else if quoteChar == '"' {
			unquoted = strings.ReplaceAll(unquoted, `""`, `"`)
			unquoted = strings.ReplaceAll(unquoted, `\"`, `"`)
		}
		return
	}

	return s
}
