//go:build llm_generated_opus46

package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

const (
	precOr         int32 = 1
	precAnd        int32 = 2
	precNot        int32 = 3
	precIsNull     int32 = 4
	precComparison int32 = 5
	precBetween    int32 = 5
	precAddSub     int32 = 6
	precMulDiv     int32 = 7
	precNegate     int32 = 8
	precTernary    int32 = 9
	precAtom       int32 = 99
)

func exprPrecedence(ctx antlr.ParserRuleContext) int32 {
	switch ctx.(type) {
	case *grammar.ColumnExprOrContext:
		return precOr
	case *grammar.ColumnExprAndContext:
		return precAnd
	case *grammar.ColumnExprNotContext:
		return precNot
	case *grammar.ColumnExprIsNullContext:
		return precIsNull
	case *grammar.ColumnExprPrecedence3Context:
		return precComparison
	case *grammar.ColumnExprBetweenContext:
		return precBetween
	case *grammar.ColumnExprPrecedence2Context:
		return precAddSub
	case *grammar.ColumnExprPrecedence1Context:
		return precMulDiv
	case *grammar.ColumnExprNegateContext:
		return precNegate
	case *grammar.ColumnExprTernaryOpContext:
		return precTernary
	case *grammar.ColumnExprLiteralContext,
		*grammar.ColumnExprIdentifierContext,
		*grammar.ColumnExprFunctionContext,
		*grammar.ColumnExprAsteriskContext,
		*grammar.ColumnExprSubqueryContext,
		*grammar.ColumnExprCaseContext,
		*grammar.ColumnExprCastContext,
		*grammar.ColumnExprDateContext,
		*grammar.ColumnExprTimestampContext,
		*grammar.ColumnExprExtractContext,
		*grammar.ColumnExprIntervalContext,
		*grammar.ColumnExprSubstringContext,
		*grammar.ColumnExprTrimContext,
		*grammar.ColumnExprArrayContext,
		*grammar.ColumnExprTupleContext,
		*grammar.ColumnExprArrayAccessContext,
		*grammar.ColumnExprTupleAccessContext,
		*grammar.ColumnExprParamSlotContext,
		*grammar.ColumnExprWinFunctionContext,
		*grammar.ColumnExprWinFunctionTargetContext,
		*grammar.ColumnExprDynamicContext,
		*grammar.ColumnExprAliasContext,
		*grammar.ColumnExprParensContext:
		return precAtom
	}
	return precAtom
}

func findColumnExprParent(node antlr.ParserRuleContext) antlr.ParserRuleContext {
	current := node.GetParent()
	for current != nil {
		ctx, ok := current.(antlr.ParserRuleContext)
		if !ok {
			break
		}
		switch ctx.GetRuleIndex() {
		case grammar.ClickHouseParserRULE_columnExpr:
			if _, isParens := ctx.(*grammar.ColumnExprParensContext); !isParens {
				return ctx
			}
		default:
			return nil
		}
		current = ctx.GetParent()
	}
	return nil
}

func isLeftOperand(parenNode antlr.ParserRuleContext, parent antlr.ParserRuleContext) bool {
	for i := 0; i < parent.GetChildCount(); i++ {
		child := parent.GetChild(i)
		if child == nil {
			continue
		}
		rctx, ok := child.(antlr.ParserRuleContext)
		if !ok {
			continue
		}
		if rctx.GetRuleIndex() == grammar.ClickHouseParserRULE_columnExpr {
			return rctx == parenNode
		}
	}
	return false
}

func isINRightOperand(parenNode antlr.ParserRuleContext, parent antlr.ParserRuleContext) bool {
	cmp, ok := parent.(*grammar.ColumnExprPrecedence3Context)
	if !ok {
		return false
	}

	{ // Check if this comparison involves IN
		hasIN := false
		for i := 0; i < cmp.GetChildCount(); i++ {
			child := cmp.GetChild(i)
			tn, ok := child.(antlr.TerminalNode)
			if !ok {
				continue
			}
			if tn.GetSymbol().GetTokenType() == grammar.ClickHouseParserIN {
				hasIN = true
				break
			}
		}
		if !hasIN {
			return false
		}
	}

	{ // The right operand is the last ColumnExpr child
		exprs := cmp.AllColumnExpr()
		if len(exprs) < 2 {
			return false
		}
		rightExpr := exprs[len(exprs)-1]
		return rightExpr == parenNode
	}
}

func canRemoveParens(inner antlr.ParserRuleContext, parent antlr.ParserRuleContext, parenNode antlr.ParserRuleContext) bool {
	if isINRightOperand(parenNode, parent) {
		return false
	}

	innerPrec := exprPrecedence(inner)
	parentPrec := exprPrecedence(parent)

	if innerPrec > parentPrec {
		return true
	}

	if innerPrec == parentPrec {
		if _, isTernary := parent.(*grammar.ColumnExprTernaryOpContext); isTernary {
			return false
		}
		return isLeftOperand(parenNode, parent)
	}

	return false
}

// RemoveRedundantParens removes parentheses that are unnecessary given operator precedence.
func RemoveRedundantParens(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("RemoveRedundantParens: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		paren, ok := ctx.(*grammar.ColumnExprParensContext)
		if !ok {
			return true
		}

		innerExpr := paren.ColumnExpr()
		if innerExpr == nil {
			return true
		}
		inner, ok := innerExpr.(antlr.ParserRuleContext)
		if !ok {
			return true
		}

		parent := findColumnExprParent(paren)

		if parent == nil {
			removeSurroundingParens(rw, paren)
			return true
		}

		if canRemoveParens(inner, parent, paren) {
			removeSurroundingParens(rw, paren)
		}

		return true
	})

	result = nanopass.GetText(rw)
	return
}

func removeSurroundingParens(rw *antlr.TokenStreamRewriter, paren *grammar.ColumnExprParensContext) {
	lparen := paren.LPAREN()
	rparen := paren.RPAREN()
	if lparen == nil || rparen == nil {
		return
	}
	nanopass.DeleteToken(rw, lparen.GetSymbol().GetTokenIndex())
	nanopass.DeleteToken(rw, rparen.GetSymbol().GetTokenIndex())
}
