package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// CanonicalizeTernary converts ternary conditional expressions to if() function
// calls. Nested ternaries (e.g. `a ? b ? c : d : e`) require fixpoint
// convergence — declares NeedsFixedPoint.
//
//	cond ? then_expr : else_expr → if(cond, then_expr, else_expr)
var CanonicalizeTernary = nanopass.LiftBodyPass(
	"CanonicalizeTernary",
	func(sql string) (string, error) { return rewriteNodes(sql, "CanonicalizeTernary", ternaryRule) },
	nanopass.PassProperties{
		NeedsFixedPoint: true,
		Reads:           nanopass.RegionBody,
		Writes:          nanopass.RegionBody,
	},
)

// ternaryRule rewrites one ColumnExprTernaryOp to if(cond, then, else).
// Grammar: <assoc=right> columnExpr QUERY columnExpr COLON columnExpr.
func ternaryRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprTernaryOpContext)
	if !ok {
		return "", false
	}
	ops := columnExprOperands(pr, c)
	if len(ops) != 3 {
		return "", false // malformed, leave unchanged
	}
	return callForm("if", ops[0], ops[1], ops[2]), true
}
