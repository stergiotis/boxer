//go:build llm_generated_opus47

package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Precedence mirrors the columnExpr alternative order in Grammar1: an
// EARLIER alternative binds TIGHTER. BETWEEN and the ternary operator are
// the last operator alternatives — they bind looser than OR (verified by
// parse trees: `a BETWEEN b AND c OR d` ≡ `a BETWEEN b AND (c OR d)`,
// `a ? b : c OR d` ≡ `a ? b : (c OR d)`).
const (
	precTernary    int32 = 0
	precBetween    int32 = 1
	precOr         int32 = 2
	precAnd        int32 = 3
	precNot        int32 = 4
	precIsNull     int32 = 5
	precComparison int32 = 6
	precAddSub     int32 = 7
	precMulDiv     int32 = 8
	precNegate     int32 = 9
	precAtom       int32 = 99
)

func exprPrecedence(ctx antlr.ParserRuleContext) int32 {
	switch ctx.(type) {
	case *grammar1.ColumnExprOrContext:
		return precOr
	case *grammar1.ColumnExprAndContext:
		return precAnd
	case *grammar1.ColumnExprNotContext:
		return precNot
	case *grammar1.ColumnExprIsNullContext:
		return precIsNull
	case *grammar1.ColumnExprPrecedence3Context:
		return precComparison
	case *grammar1.ColumnExprBetweenContext:
		return precBetween
	case *grammar1.ColumnExprPrecedence2Context:
		return precAddSub
	case *grammar1.ColumnExprPrecedence1Context:
		return precMulDiv
	case *grammar1.ColumnExprNegateContext:
		return precNegate
	case *grammar1.ColumnExprTernaryOpContext:
		return precTernary
	case *grammar1.ColumnExprLiteralContext,
		*grammar1.ColumnExprIdentifierContext,
		*grammar1.ColumnExprFunctionContext,
		*grammar1.ColumnExprAsteriskContext,
		*grammar1.ColumnExprSubqueryContext,
		*grammar1.ColumnExprCaseContext,
		*grammar1.ColumnExprCastContext,
		*grammar1.ColumnExprDateContext,
		*grammar1.ColumnExprTimestampContext,
		*grammar1.ColumnExprExtractContext,
		*grammar1.ColumnExprIntervalContext,
		*grammar1.ColumnExprSubstringContext,
		*grammar1.ColumnExprTrimContext,
		*grammar1.ColumnExprArrayContext,
		*grammar1.ColumnExprTupleContext,
		*grammar1.ColumnExprArrayAccessContext,
		*grammar1.ColumnExprTupleAccessContext,
		*grammar1.ColumnExprParamSlotContext,
		*grammar1.ColumnExprWinFunctionContext,
		*grammar1.ColumnExprWinFunctionTargetContext,
		*grammar1.ColumnExprDynamicContext,
		*grammar1.ColumnExprAliasContext,
		*grammar1.ColumnExprParensContext:
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
		case grammar1.ClickHouseParserGrammar1RULE_columnExpr:
			if _, isParens := ctx.(*grammar1.ColumnExprParensContext); !isParens {
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
		if rctx.GetRuleIndex() == grammar1.ClickHouseParserGrammar1RULE_columnExpr {
			return rctx == parenNode
		}
	}
	return false
}

func isINRightOperand(parenNode antlr.ParserRuleContext, parent antlr.ParserRuleContext) bool {
	cmp, ok := parent.(*grammar1.ColumnExprPrecedence3Context)
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
			if tn.GetSymbol().GetTokenType() == grammar1.ClickHouseParserGrammar1IN {
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

func canRemoveParens(pr *nanopass.ParseResult, inner antlr.ParserRuleContext, parent antlr.ParserRuleContext, parenNode antlr.ParserRuleContext) bool {
	if isINRightOperand(parenNode, parent) {
		return false
	}

	// Guard against creating "--" comment syntax or ambiguous operator sequences.
	// If the parent is unary minus and the inner starts with minus, keep parens: -(-x), -(-1)
	if _, isParentNegate := parent.(*grammar1.ColumnExprNegateContext); isParentNegate {
		if innerStartsWithMinus(inner) {
			return false
		}
	}

	// Same hazard with binary minus when the source has no whitespace:
	// a-(-5) → a--5 starts a line comment. Keep the parens whenever the
	// token immediately before '(' is a dash and the inner starts with one.
	if innerStartsWithMinus(inner) && dashPrecedesParen(pr, parenNode) {
		return false
	}

	// BETWEEN owns an AND inside its own syntax; removing parens around its
	// operands can hand that AND to the wrong owner (`a BETWEEN (b AND c)
	// AND d` reparses with a different middle operand). Only atoms are safe.
	if _, isParentBetween := parent.(*grammar1.ColumnExprBetweenContext); isParentBetween {
		return exprPrecedence(inner) == precAtom
	}

	// If the inner is unary minus and it's NOT the left operand of a binary operator,
	// keep parens to avoid ambiguous sequences like "a + -b" or "a * -b"
	if _, isInnerNegate := inner.(*grammar1.ColumnExprNegateContext); isInnerNegate {
		if !isLeftOperand(parenNode, parent) {
			return false
		}
	}

	innerPrec := exprPrecedence(inner)
	parentPrec := exprPrecedence(parent)

	if innerPrec > parentPrec {
		return true
	}

	if innerPrec == parentPrec {
		if _, isTernary := parent.(*grammar1.ColumnExprTernaryOpContext); isTernary {
			return false
		}
		return isLeftOperand(parenNode, parent)
	}

	return false
}

// innerStartsWithMinus returns true if the first token of the expression is a minus sign.
// This covers both ColumnExprNegate (-expr) and negative literals (-1).
func innerStartsWithMinus(ctx antlr.ParserRuleContext) bool {
	startTok := ctx.GetStart()
	return startTok != nil && startTok.GetText() == "-"
}

// dashPrecedesParen reports whether the token immediately before the
// paren expression's '(' — with no intervening hidden tokens — is a dash.
func dashPrecedesParen(pr *nanopass.ParseResult, parenNode antlr.ParserRuleContext) bool {
	lparen := parenNode.GetStart()
	if lparen == nil {
		return false
	}
	idx := lparen.GetTokenIndex()
	if idx == 0 {
		return false
	}
	prev := pr.TokenStream.Get(idx - 1)
	return prev.GetTokenType() == grammar1.ClickHouseLexerDASH
}

// RemoveRedundantParens removes parentheses that are unnecessary given
// operator precedence.
var RemoveRedundantParens = nanopass.LiftBodyPass(
	"RemoveRedundantParens",
	removeRedundantParensImpl,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

func removeRedundantParensImpl(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("RemoveRedundantParens: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		paren, ok := ctx.(*grammar1.ColumnExprParensContext)
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

		if canRemoveParens(pr, inner, parent, paren) {
			removeSurroundingParens(rw, paren)
		}

		return true
	})

	result = nanopass.GetText(rw)
	return
}

func removeSurroundingParens(rw *antlr.TokenStreamRewriter, paren *grammar1.ColumnExprParensContext) {
	lparen := paren.LPAREN()
	rparen := paren.RPAREN()
	if lparen == nil || rparen == nil {
		return
	}
	nanopass.DeleteToken(rw, lparen.GetSymbol().GetTokenIndex())
	nanopass.DeleteToken(rw, rparen.GetSymbol().GetTokenIndex())
}
