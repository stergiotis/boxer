//go:build llm_generated_opus46

package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeTernary converts ternary conditional expressions to if() function calls:
//
//	cond ? then_expr : else_expr → if(cond, then_expr, else_expr)
//
// Nested ternaries are handled correctly because the walk processes outer nodes
// first and captures inner expression text verbatim (which may contain already-
// rewritten if() calls from inner ternaries on a subsequent pipeline re-parse).
//
// For correct nesting, this pass should be applied with FixedPoint or re-parsed
// between iterations when the SQL contains nested ternaries like a ? b ? c : d : e.
func CanonicalizeTernary(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeTernary: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if c, ok := ctx.(*grammar1.ColumnExprTernaryOpContext); ok {
			rewriteTernary(rw, pr, c)
			return false // don't descend into replaced node
		}
		return true
	})

	result = nanopass.GetText(rw)
	return
}

var _ nanopass.Pass = CanonicalizeTernary

// rewriteTernary converts a single ColumnExprTernaryOp to if(cond, then, else).
// Grammar: <assoc=right> columnExpr QUERY columnExpr COLON columnExpr
// Children: columnExpr[0] QUERY columnExpr[1] COLON columnExpr[2]
func rewriteTernary(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprTernaryOpContext) {
	exprs := make([]string, 0, 3)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar1.IColumnExprContext); ok {
			exprs = append(exprs, nanopass.NodeText(pr, ce.(antlr.ParserRuleContext)))
		}
	}

	if len(exprs) != 3 {
		return // malformed, leave unchanged
	}

	nanopass.ReplaceNode(rw, ctx, "if("+exprs[0]+", "+exprs[1]+", "+exprs[2]+")")
}
