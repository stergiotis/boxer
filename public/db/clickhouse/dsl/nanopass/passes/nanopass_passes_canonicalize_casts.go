//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeCasts returns a Pass that rewrites all cast syntaxes to the
// canonical function form CAST(expr, 'Type').
//
// Canonicalized forms:
//
//	expr::Type            → CAST(expr, 'Type')
//	CAST(expr AS Type)    → CAST(expr, 'Type')
//	CAST(expr, 'Type')    → CAST(expr, 'Type') (no change)
//
// Nested casts are handled by iterating until fixpoint — innermost casts
// are canonicalized first, then the pass re-parses and processes the next layer.
func CanonicalizeCasts() nanopass.Pass {
	return func(sql string) (result string, err error) {
		current := sql
		for {
			next, passErr := canonicalizeCastsOnce(current)
			if passErr != nil {
				err = passErr
				return
			}
			if next == current {
				break
			}
			current = next
		}
		result = current
		return
	}
}

// canonicalizeCastsOnce performs a single canonicalization pass. It skips cast nodes
// whose expression child is itself a non-canonical cast (to avoid overlapping rewrites).
// Repeated application handles nested casts layer by layer.
func canonicalizeCastsOnce(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeCasts: %w", err)
		return
	}

	rw := nanopass.NewRewriter(pr)
	changed := false

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if castCtx, ok := ctx.(*grammar1.ColumnExprCastContext); ok {
			if containsNonCanonicalCastChild(castCtx) {
				return true
			}
			if canonicalizeCastExpr(pr, rw, castCtx) {
				changed = true
			}
		}
		return true
	})

	if !changed {
		result = sql
		return
	}

	result = nanopass.GetText(rw)
	return
}

// containsNonCanonicalCastChild checks whether any direct child of a ColumnExprCastContext
// is itself a non-canonical cast node (expr::Type or CAST(expr AS Type)) that would also be
// rewritten, causing overlapping writes.
// The CAST(expr, 'Type') function form is already canonical and won't be rewritten by this pass,
// so it's safe to process the outer cast even if the child is a CAST function call.
func containsNonCanonicalCastChild(castCtx *grammar1.ColumnExprCastContext) bool {
	for i := 0; i < castCtx.GetChildCount(); i++ {
		child := castCtx.GetChild(i)
		// Any ColumnExprCastContext child is non-canonical (either :: or CAST AS form)
		// — both would be rewritten by this pass
		if _, isCast := child.(*grammar1.ColumnExprCastContext); isCast {
			return true
		}
	}
	return false
}

// canonicalizeCastExpr handles ColumnExprCastContext which covers:
//   - expr::Type           (children: expr, ::, ColumnTypeExpr)
//   - CAST(expr AS Type)   (children: CAST, (, expr, AS, ColumnTypeExpr, ))
//
// Both are rewritten to CAST(expr, 'Type').
// Returns true if a rewrite was performed.
func canonicalizeCastExpr(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, castCtx *grammar1.ColumnExprCastContext) bool {
	if castCtx.GetChildCount() == 0 {
		return false
	}

	var exprNode antlr.ParserRuleContext
	var typeText string

	firstChild := castCtx.GetChild(0)
	_, firstIsTerm := firstChild.(*antlr.TerminalNodeImpl)

	if firstIsTerm {
		// Could be CAST(expr AS Type) or :: form where first token is something else.
		// Check if it's CAST
		term := firstChild.(*antlr.TerminalNodeImpl)
		if strings.ToUpper(term.GetText()) == "CAST" {
			// CAST(expr AS Type) form
			// Children: CAST, (, expr, AS, TypeExpr, )
			for i := 0; i < castCtx.GetChildCount(); i++ {
				child := castCtx.GetChild(i)
				switch child.(type) {
				case *grammar1.ColumnTypeExprSimpleContext:
					typeText = child.(*grammar1.ColumnTypeExprSimpleContext).GetText()
				case *grammar1.ColumnTypeExprComplexContext:
					typeText = child.(*grammar1.ColumnTypeExprComplexContext).GetText()
				case antlr.ParserRuleContext:
					if exprNode == nil {
						_, isSimple := child.(*grammar1.ColumnTypeExprSimpleContext)
						_, isComplex := child.(*grammar1.ColumnTypeExprComplexContext)
						if !isSimple && !isComplex {
							exprNode = child.(antlr.ParserRuleContext)
						}
					}
				}
			}
		} else {
			// Not a CAST terminal — not a form we handle
			return false
		}
	} else {
		// First child is an expression → expr::Type form
		// Children: expr, ::, ColumnTypeExpr
		for i := 0; i < castCtx.GetChildCount(); i++ {
			child := castCtx.GetChild(i)
			switch child.(type) {
			case *grammar1.ColumnTypeExprSimpleContext:
				typeText = child.(*grammar1.ColumnTypeExprSimpleContext).GetText()
			case *grammar1.ColumnTypeExprComplexContext:
				typeText = child.(*grammar1.ColumnTypeExprComplexContext).GetText()
			case antlr.ParserRuleContext:
				if exprNode == nil {
					_, isSimple := child.(*grammar1.ColumnTypeExprSimpleContext)
					_, isComplex := child.(*grammar1.ColumnTypeExprComplexContext)
					if !isSimple && !isComplex {
						exprNode = child.(antlr.ParserRuleContext)
					}
				}
			}
		}
	}

	if exprNode == nil || typeText == "" {
		return false
	}

	exprText := nanopass.NodeText(pr, exprNode)
	replacement := "CAST(" + exprText + ", '" + typeText + "')"
	nanopass.ReplaceNode(rw, castCtx, replacement)
	return true
}
