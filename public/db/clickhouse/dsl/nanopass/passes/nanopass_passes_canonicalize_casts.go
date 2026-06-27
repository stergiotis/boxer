package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// CanonicalizeCasts rewrites all cast syntaxes to the canonical function form
// CAST(expr, 'Type'). Nested casts converge under fixpoint iteration: the outer
// cast is rewritten first (its expr span carried verbatim), then the re-parse
// canonicalises the inner one.
//
//	expr::Type            → CAST(expr, 'Type')
//	CAST(expr AS Type)    → CAST(expr, 'Type')
//	CAST(expr, 'Type')    → CAST(expr, 'Type') (unchanged: function form, no columnTypeExpr child)
var CanonicalizeCasts = nanopass.LiftBodyPass(
	"CanonicalizeCasts",
	func(sql string) (string, error) { return rewriteNodes(sql, "CanonicalizeCasts", castRule) },
	nanopass.PassProperties{
		NeedsFixedPoint: true,
		Reads:           nanopass.RegionBody,
		Writes:          nanopass.RegionBody,
	},
)

// castRule rewrites one ColumnExprCast — covering both `expr::Type` and
// `CAST(expr AS Type)` — to CAST(expr, 'Type'). The already-canonical
// `CAST(expr, 'Type')` parses as a plain function call (not a ColumnExprCast),
// so it never reaches this rule; the typeText guard is a belt-and-suspenders for
// the same. Overlap with casts nested inside expr is avoided structurally:
// rewriteNodes skips the replaced node's subtree and the fixpoint re-parse picks
// the inner cast up on the next iteration.
func castRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprCastContext)
	if !ok {
		return "", false
	}
	// Both forms carry exactly one columnTypeExpr child and one expression
	// child; the rest are terminals (CAST, parens, AS, ::).
	var exprNode antlr.ParserRuleContext
	var typeText string
	for i := 0; i < c.GetChildCount(); i++ {
		child, isCtx := c.GetChild(i).(antlr.ParserRuleContext)
		if !isCtx {
			continue
		}
		if isColumnTypeExprNode(child) {
			if typeText == "" {
				typeText = child.GetText()
			}
			continue
		}
		if exprNode == nil {
			exprNode = child
		}
	}
	if exprNode == nil || typeText == "" {
		return "", false
	}
	// Enum types carry single-quoted member names (Enum8('a' = 1)) — escape them
	// for splicing into the single-quoted type string.
	escapedType := strings.ReplaceAll(typeText, `'`, `\'`)
	return callForm("CAST", spanOf(pr, exprNode), "'"+escapedType+"'"), true
}

// isColumnTypeExprNode reports whether the node is any alternative of the
// columnTypeExpr rule — simple (UInt64), complex (Nullable(String)), parametric
// (FixedString(16), DateTime64(3)), nested, or enum.
func isColumnTypeExprNode(ctx antlr.ParserRuleContext) bool {
	switch ctx.(type) {
	case *grammar1.ColumnTypeExprSimpleContext,
		*grammar1.ColumnTypeExprComplexContext,
		*grammar1.ColumnTypeExprParamContext,
		*grammar1.ColumnTypeExprNestedContext,
		*grammar1.ColumnTypeExprEnumContext:
		return true
	}
	return false
}
