//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeCaseConditionals converts CASE expressions to ClickHouse function calls and
// normalizes multiIf with a single condition to if.
//
// CASE transformations (leaf-level only per invocation — use FixedPoint for nesting):
//
//	Searched CASE (no operand, multiple branches):
//	  CASE WHEN c1 THEN r1 WHEN c2 THEN r2 ELSE d END
//	  → multiIf(c1, r1, c2, r2, d)
//
//	Searched CASE (no operand, single branch):
//	  CASE WHEN c THEN r ELSE d END → if(c, r, d)
//	  CASE WHEN c THEN r END        → if(c, r, NULL)
//
//	Simple CASE (with operand):
//	  CASE x WHEN v1 THEN r1 WHEN v2 THEN r2 ELSE d END
//	  → caseWithExpression(x, v1, r1, v2, r2, d)
//
//	  CASE x WHEN v1 THEN r1 END
//	  → caseWithExpression(x, v1, r1, NULL)
//
// multiIf normalization (token-level):
//
//	multiIf(c, r, d) with exactly 3 arguments → if(c, r, d)
//
// All three function names (if, multiIf, caseWithExpression) are real ClickHouse
// functions, so the output is valid ClickHouse SQL verifiable via EXPLAIN SYNTAX.
func CanonicalizeCaseConditionals(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeCaseConditionals: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	// Phase 1: Rewrite leaf-level CASE expressions
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		c, ok := ctx.(*grammar1.ColumnExprCaseContext)
		if !ok {
			return true
		}
		if containsInnerCase(c) {
			return true // descend to find leaf CASEs
		}
		rewriteCase(rw, pr, c)
		return false
	})

	// Phase 2: Normalize multiIf(c, r, d) → if(c, r, d)
	// This catches both pre-existing multiIf calls and those just created by Phase 1.
	// We do this on a second parse to avoid interfering with Phase 1 rewrites.
	// However, since Phase 1 emits text and Phase 2 needs to count args,
	// Phase 2 runs on the next pipeline invocation (or a separate pass).
	// For simplicity, we include it here by emitting "if" directly for single-branch
	// searched CASEs in rewriteCase, and handle pre-existing multiIf in a separate function.

	result = nanopass.GetText(rw)
	return
}

// NormalizeMultiIf normalizes multiIf(c, r, d) with exactly 3 arguments to if(c, r, d).
// This is a separate pass because it needs to count function arguments, which requires
// the CST to reflect the current state (after CanonicalizeCaseConditionals has been applied).
//
// Run this after CanonicalizeCaseConditionals in the pipeline:
//
//	result, err = nanopass.FixedPoint(passes.CanonicalizeCaseConditionals, 10)(sql)
//	result, err = passes.NormalizeMultiIf(result)
func NormalizeMultiIf(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("NormalizeMultiIf: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		c, ok := ctx.(*grammar1.ColumnExprFunctionContext)
		if !ok {
			return true
		}
		rewriteMultiIfToIf(rw, pr, c)
		return true
	})

	result = nanopass.GetText(rw)
	return
}

// rewriteMultiIfToIf checks if a function call is multiIf with exactly 3 arguments
// and rewrites it to if.
func rewriteMultiIfToIf(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprFunctionContext) {
	// Find function name
	var nameIdent *grammar1.IdentifierContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext); ok {
			nameIdent = ident
			break
		}
	}
	if nameIdent == nil {
		return
	}

	name := strings.ToLower(nameIdent.GetText())
	if name != "multiif" {
		return
	}

	// Count arguments by finding the ColumnArgList or ColumnExprList and counting columnExpr children
	argCount := 0
	nanopass.WalkCST(ctx, func(inner antlr.ParserRuleContext) bool {
		switch inner.(type) {
		case *grammar1.ColumnArgListContext:
			// Count ColumnArgExpr children
			for j := 0; j < inner.GetChildCount(); j++ {
				if _, ok := inner.GetChild(j).(*grammar1.ColumnArgExprContext); ok {
					argCount++
				}
			}
			return false
		case *grammar1.ColumnExprListContext:
			// ColumnExprList inside function parens — count columnsExpr children
			for j := 0; j < inner.GetChildCount(); j++ {
				if _, ok := inner.GetChild(j).(*grammar1.ColumnsExprColumnContext); ok {
					argCount++
				}
			}
			return false
		}
		return true
	})

	if argCount == 3 {
		// Replace "multiIf" with "if" — just replace the identifier token
		nanopass.ReplaceToken(rw, nameIdent.GetStart().GetTokenIndex(), "if")
	}
}

// containsInnerCase returns true if ctx contains a nested ColumnExprCaseContext.
func containsInnerCase(ctx *grammar1.ColumnExprCaseContext) bool {
	found := false
	var walk func(node antlr.Tree)
	walk = func(node antlr.Tree) {
		if found {
			return
		}
		for i := 0; i < node.GetChildCount(); i++ {
			child := node.GetChild(i)
			if _, ok := child.(*grammar1.ColumnExprCaseContext); ok {
				found = true
				return
			}
			walk(child)
		}
	}
	walk(ctx)
	return found
}

func rewriteCase(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprCaseContext) {
	type stateE int
	const (
		stateStart stateE = iota
		stateOperand
		stateWhen
		stateThen
		stateElse
	)
	state := stateStart

	var operandText string
	hasOperand := false
	hasElse := false

	type whenThen struct {
		when string
		then string
	}
	pairs := make([]whenThen, 0, 4)
	var currentWhen string
	var elseText string

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)

		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			switch tt {
			case grammar1.ClickHouseLexerCASE:
				state = stateOperand
			case grammar1.ClickHouseLexerWHEN:
				state = stateWhen
			case grammar1.ClickHouseLexerTHEN:
				state = stateThen
			case grammar1.ClickHouseLexerELSE:
				state = stateElse
			case grammar1.ClickHouseLexerEND:
				// done
			}
			continue
		}

		if ce, ok := child.(grammar1.IColumnExprContext); ok {
			exprText := nanopass.NodeText(pr, ce.(antlr.ParserRuleContext))
			switch state {
			case stateOperand:
				hasOperand = true
				operandText = exprText
			case stateWhen:
				currentWhen = exprText
			case stateThen:
				pairs = append(pairs, whenThen{when: currentWhen, then: exprText})
			case stateElse:
				hasElse = true
				elseText = exprText
			}
		}
	}

	// Compute default value
	defaultVal := "NULL"
	if hasElse {
		defaultVal = elseText
	}

	var b strings.Builder

	if hasOperand {
		// Simple CASE → caseWithExpression(operand, v1, r1, v2, r2, ..., default)
		b.WriteString("caseWithExpression(")
		b.WriteString(operandText)
		for _, pair := range pairs {
			b.WriteString(", ")
			b.WriteString(pair.when)
			b.WriteString(", ")
			b.WriteString(pair.then)
		}
		b.WriteString(", ")
		b.WriteString(defaultVal)
		b.WriteByte(')')
	} else if len(pairs) == 1 {
		// Searched CASE with single branch → if(condition, result, default)
		b.WriteString("if(")
		b.WriteString(pairs[0].when)
		b.WriteString(", ")
		b.WriteString(pairs[0].then)
		b.WriteString(", ")
		b.WriteString(defaultVal)
		b.WriteByte(')')
	} else {
		// Searched CASE with multiple branches → multiIf(c1, r1, c2, r2, ..., default)
		b.WriteString("multiIf(")
		for i, pair := range pairs {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pair.when)
			b.WriteString(", ")
			b.WriteString(pair.then)
		}
		b.WriteString(", ")
		b.WriteString(defaultVal)
		b.WriteByte(')')
	}

	nanopass.ReplaceNode(rw, ctx, b.String())
}
