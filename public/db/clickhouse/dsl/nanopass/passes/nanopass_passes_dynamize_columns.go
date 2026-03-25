//go:build llm_generated_opus46

package passes

import (
	"regexp"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// WrapColumnsWithDynamic returns a Pass that wraps individual column references
// matching the given regex pattern with COLUMNS('^column_name$') syntax.
//
// For example, with pattern `.*_id$`:
//
//	SELECT id, tenant_id, customer_id, amount FROM orders
//	→ SELECT id, COLUMNS('^tenant_id$'), COLUMNS('^customer_id$'), amount FROM orders
//
// Only bare column identifiers in the SELECT list are wrapped — qualified columns
// (table.column), expressions, function calls, aliases, and star expressions are
// left untouched.
//
// The pass is scope-aware: it processes all UNION ALL branches, CTE bodies,
// and subqueries.
func WrapColumnsWithDynamic(pattern string) nanopass.Pass {
	return func(sql string) (result string, err error) {
		re, compileErr := regexp.Compile(pattern)
		if compileErr != nil {
			err = eh.Errorf("WrapColumnsWithDynamic: invalid regex %q: %w", pattern, compileErr)
			return
		}

		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("WrapColumnsWithDynamic: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		scopes := nanopass.BuildScopes(pr)
		for _, scope := range scopes {
			wrapColumnsInScope(rw, scope, re)
		}

		result = nanopass.GetText(rw)
		return
	}
}

func wrapColumnsInScope(rw *antlr.TokenStreamRewriter, scope *nanopass.SelectScope, re *regexp.Regexp) {
	stmt := scope.Node
	projClause := stmt.ProjectionClause()
	if projClause == nil {
		return
	}

	// Walk the projection clause looking for ColumnsExprColumn nodes.
	// Each ColumnsExprColumn wraps a single column expression in the SELECT list.
	nanopass.WalkCST(projClause.(antlr.ParserRuleContext), func(ctx antlr.ParserRuleContext) bool {
		colsExpr, ok := ctx.(*grammar.ColumnsExprColumnContext)
		if !ok {
			return true
		}

		// Check if this ColumnsExprColumn contains a bare column identifier.
		// The structure is: ColumnsExprColumn → ColumnExprIdentifier → ColumnIdentifier
		// We only wrap bare identifiers, not qualified (table.col), aliased (col AS x),
		// function calls, or any other expression type.
		colName, isWrappable := extractBareColumnName(colsExpr)
		if !isWrappable {
			return false
		}

		if !re.MatchString(colName) {
			return false
		}

		// Replace the entire ColumnsExprColumn with COLUMNS('^colName$')
		escaped := escapeRegexLiteral(colName)
		nanopass.ReplaceNode(rw, colsExpr, "COLUMNS('^"+escaped+"$')")

		return false
	})

	// Recurse into CTE body scopes
	for _, cte := range scope.CTEDefs {
		if cte.Scope != nil {
			wrapColumnsInScope(rw, cte.Scope, re)
		}
	}

	// Recurse into FROM subquery scopes
	for _, ts := range scope.Tables {
		if ts.IsSubquery && ts.Scope != nil {
			wrapColumnsInScope(rw, ts.Scope, re)
		}
	}

	// Recurse into expression subqueries
	for _, sub := range scope.Subqueries {
		wrapColumnsInScope(rw, sub, re)
	}
}

// extractBareColumnName checks if a ColumnsExprColumn contains a bare (unqualified,
// unaliased) column identifier and returns its name.
// Returns ("", false) for qualified columns, aliased expressions, function calls, etc.
func extractBareColumnName(colsExpr *grammar.ColumnsExprColumnContext) (name string, ok bool) {
	// ColumnsExprColumn has exactly one child: a columnExpr
	if colsExpr.GetChildCount() == 0 {
		return
	}

	// The child must be a ColumnExprIdentifier (bare column reference)
	child := colsExpr.GetChild(0)
	identExpr, isIdent := child.(*grammar.ColumnExprIdentifierContext)
	if !isIdent {
		return
	}

	// ColumnExprIdentifier → ColumnIdentifier
	// ColumnIdentifier may have a TableIdentifier (qualified) or just a NestedIdentifier
	for i := 0; i < identExpr.GetChildCount(); i++ {
		colId, isColId := identExpr.GetChild(i).(*grammar.ColumnIdentifierContext)
		if !isColId {
			continue
		}

		// If it has a TableIdentifier, it's qualified (table.col) — skip
		if colId.TableIdentifier() != nil {
			return
		}

		// Get the bare column name from NestedIdentifier
		if colId.NestedIdentifier() != nil {
			name = colId.NestedIdentifier().GetText()
			ok = true
		}
		return
	}
	return
}

// escapeRegexLiteral escapes regex metacharacters in a column name so that
// COLUMNS('^name$') matches the literal name.
func escapeRegexLiteral(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 4)
	for _, c := range s {
		switch c {
		case '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '\\', '^', '$', '|':
			sb.WriteByte('\\')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}
