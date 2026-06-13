package passes

import (
	"regexp"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
	return nanopass.LiftBodyPass(
		"WrapColumnsWithDynamic",
		func(sql string) (result string, err error) {
			re, compileErr := regexp.Compile(pattern)
			if compileErr != nil {
				err = eb.Build().Str("pattern", pattern).Errorf("invalid column regex: %w", compileErr)
				return
			}

			pr, err := nanopass.Parse(sql)
			if err != nil {
				err = eh.Errorf("WrapColumnsWithDynamic: %w", err)
				return
			}
			rw := nanopass.NewRewriter(pr)

			scopes, err := nanopass.BuildScopes(pr, "")
			if err != nil {
				err = eh.Errorf("WrapColumnsWithDynamic: %w", err)
				return
			}
			for _, scope := range nanopass.FlattenScopes(scopes) {
				wrapColumnsInScope(rw, scope, re)
			}

			result = nanopass.GetText(rw)
			return
		},
		nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody,
			Writes:     nanopass.RegionBody,
		},
	)
}

func wrapColumnsInScope(rw nanopass.RewriterI, scope *nanopass.SelectScope, re *regexp.Regexp) {
	stmt := scope.Node
	projClause := stmt.ProjectionClause()
	if projClause == nil {
		return
	}

	// Walk the projection clause looking for ColumnsExprColumn nodes.
	// Each ColumnsExprColumn wraps a single column expression in the SELECT list.
	// Nested SELECTs are pruned — they are processed via their own scope.
	nanopass.WalkCST(projClause.(antlr.ParserRuleContext), func(ctx antlr.ParserRuleContext) bool {
		if isScopeBoundary(ctx) {
			return false
		}
		colsExpr, ok := ctx.(*grammar1.ColumnsExprColumnContext)
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
		escaped := regexp.QuoteMeta(colName)
		nanopass.ReplaceNode(rw, colsExpr, "COLUMNS('^"+escaped+"')")

		return false
	})
}

// extractBareColumnName checks if a ColumnsExprColumn contains a bare (unqualified,
// unaliased) column identifier and returns its name.
// Returns ("", false) for qualified columns, aliased expressions, function calls, etc.
func extractBareColumnName(colsExpr *grammar1.ColumnsExprColumnContext) (name string, ok bool) {
	// ColumnsExprColumn has exactly one child: a columnExpr
	if colsExpr.GetChildCount() == 0 {
		return
	}

	// The child must be a ColumnExprIdentifier (bare column reference)
	child := colsExpr.GetChild(0)
	identExpr, isIdent := child.(*grammar1.ColumnExprIdentifierContext)
	if !isIdent {
		return
	}

	// ColumnExprIdentifier → ColumnIdentifier
	// ColumnIdentifier may have a TableIdentifier (qualified) or just a NestedIdentifier
	for i := 0; i < identExpr.GetChildCount(); i++ {
		colId, isColId := identExpr.GetChild(i).(*grammar1.ColumnIdentifierContext)
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
